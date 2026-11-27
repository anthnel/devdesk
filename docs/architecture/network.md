# Container engine, OCI registries, network diagnostics, sockets and interfaces

> DevDesk architecture notes. Referenced from `.claude/CLAUDE.md`;
> read this file when working on the code it describes.

## The container engine — `internal/engine` and `internal/docker`

**`internal/engine`** names the engine in use — docker or podman — and carries
everything that is not the same between the two: the binary, the credential
helper prefix, the auth file, the host socket, and the `--format` templates. It
is its own package rather than a field of `internal/docker` because
`internal/scan` needs the same answer and does not import `internal/docker` —
`internal/forge`'s arrangement read one level down (§3.67).

`engine.Resolve(preference)` turns `app.container_engine` into a `Shape`:
`auto` takes docker if it is on PATH and podman otherwise; a pinned engine that
is absent is an **error**, never the other one, because the two do not hold the
same containers. Anything else is taken as a path to a binary. The router
resolves once at startup and once per context switch, into `engine.SetCurrent`.

A `Shape` carries **no resolved filesystem path**: `AuthPaths()`, `AuthPath()`
and `HostSocket()` are methods. The shape is built at package initialisation, so
a `$HOME` read there would be frozen before anything else runs — and whether the
socket exists is the whole question, since `podman system service` can start or
stop while the application is up.

**`internal/docker`** drives that engine. Every invocation goes through one
seam — `dockerRunner`, implemented by `cliRunner` (`exec.go`) — which is what
made podman cheap: the binary name lived in exactly one place. The package is
split by resource: `containers.go`, `images.go`, `networks.go`, `volumes.go`,
`registry.go`, `system.go`, `launch.go`, plus `exec.go` (the seam), `parse.go`,
`container_ports.go`, `identifier.go` and `names.go`. It is **not** called
`internal/container`: renaming it would touch 40 files for a vocabulary gain.

Two things about the engine are worth knowing before changing this code:

- **The `--format` templates are per engine and identical today.** Podman
  implements `--format`, but equality of the *fields* is guaranteed nowhere, and
  a divergence raises no error — it produces wrong rows. No measurement has been
  taken against a real podman; `TestTheTwoTemplateSetsAreStillUnmeasured` fails
  the day the two sets stop matching, so a divergence is recorded deliberately.
- **Supported engine versions (OC-01, DEVDESK-796).** The matrix DevDesk is
  measured and tested against is Docker 29.10.1 (N) and 28.5.2 (N-1), and
  Podman 6.2.1 (N) and 5.8.11 (N-1), rootless included. The dated measurements
  are in `docs/measurements/2026-11-27-engines.md`.
- **`system df -v` is scraped, not templated**, and is therefore disabled under
  podman (`Shape.ParsesSystemDFVerbose`). It is a fixed-width table read at
  offsets taken from its header line, and there is no `--format` that exposes
  per-image unique size.
- **Tests run once per engine, with a strict stub (OC-07, DEVDESK-804).** A test
  of `internal/docker` whose result depends on the engine goes through
  `forEachEngine(t, fn)` (`exec_test.go`): one subtest per `engine.Names()`,
  `useEngine` applied, the original engine restored by a `Cleanup` registered
  before `fn` runs. It pairs with `stubStrict(t, reply{argv, out, err}...)`: a
  `stubRunner` with a `report` is strict, matches the whole argv (so a wrong
  template fails, where the old stub only read the subcommand) and fails the test
  with the argv quoted through `%q` for `Run` and `Build` alike; a reply the
  code never asked for fails it too, at cleanup. Strictness is
  opt-in: a `stubRunner` without `report` keeps its per-subcommand behaviour, so
  tests migrate one by one. `TestForEachEngineCoversEveryConfiguredEngine`
  (`foreach_test.go`) fails when `config.ContainerEngines()` and the engines the
  helper runs diverge; it stands in for the guard of `TestEveryConfiguredEngineHasAShape`
  (`internal/engine`), which cannot see the helper.
- **Published ports are on 127.0.0.1 unless the user wrote an address (OC-19,
  DEVDESK-817).** `buildLaunchArgs` passes every `-p` value through
  `docker.LoopbackPort` (`launch.go`), so the same code serves Docker and Podman
  (rootless included). A spec with no address gets `127.0.0.1:` (`8080:80` →
  `127.0.0.1:8080:80`, `80` → `127.0.0.1::80`, `53/udp` → `127.0.0.1::53/udp`,
  a range likewise); an address field is recognised by a leading `[` (IPv6) or two
  colons, and is never rewritten, so `127.0.0.1:…`, `[::1]:…`, `192.168.1.10:…`
  and `0.0.0.0:…` reach the engine as typed; an empty address (`:8080:80`, `::80`)
  binds every interface and is treated as no address. It is idempotent, which lets
  the form preview its result and the launcher apply it again. A malformed spec is
  left to the engine to refuse. `docker.PortExposedOnAllInterfaces` is true when
  the spec itself names `0.0.0.0` or `[::]`: the launch form
  (`internal/ui/oci_resources`) then shows a Warning footer ("Port … is published
  on all interfaces"), derived on every render in `LaunchForm.ValidationStatus`
  with no timer and below any validation error; the launch is still allowed. The
  form hands the specs over as typed, previews and `ctrl+y` copy the `LoopbackPort`
  form, and the cache keeps the host port only, so an entry written before this
  rule gets 127.0.0.1 when it is read. A non-loopback specific address is neither
  rewritten nor warned about.

- **The inspect hides the values of sensitive variables (OC-13, DEVDESK-811,
  requirement S9).** `inspectSource` is `viewer.Maskable`: the viewer hands it the
  bytes of `docker.InspectContainer` and the `secretenv.Matcher` built from
  `containers.sensitive_env` (OC-12), and `maskInspectEnv`
  (`internal/ui/containers/inspect_mask.go`) replaces the value of each sensitive
  `Config.Env` entry with `********` in place, leaving the rest of the document
  byte for byte (a rewritten entry written with JSON escapes comes back as
  literal UTF-8). Under Podman, `Config.CreateCommand` (the argv of the
  `podman run`) repeats the `-e` values: the element after `-e`/`--env` and the
  glued `--env=NAME=VALUE` and `-eNAME=VALUE` are masked with the same matcher,
  and `--env-file` paths are left alone. Output that is not JSON is refused, never shown raw. The raw bytes stay in the viewer's memory so
  `m` can reveal them; the display, the search and `Y` only ever see the masked
  text. `Cmd` and the labels are not masked. See `docs/architecture/viewer.md`.

- **A launch passes environment values to the child, never in argv (OC-14,
  DEVDESK-812, requirement S9).** `ContainerLaunchOptions.Env` stays a list of
  `NAME=VALUE`. `buildLaunchArgs` emits `-e NAME` only (an entry that is not
  launchable is left out), and the value travels in the environment of the child:
  `launchEnviron` returns `os.Environ()` followed by exactly the typed entries
  (nil when nothing was typed, so the child keeps DevDesk's environment as before;
  `os/exec` keeps the last entry of a name). `BuildLaunchCmd` sets it as
  `cmd.Env`; `LaunchContainer` passes it as the new `dockerCmd.Env` field, which
  `cliRunner.Run` applies as `cmd.Env` when non-nil. The model is
  `containerRun.cmd` (`internal/scan/container_run.go`, same `-e NAME` form for
  the proxy). Both engines behave the same, with the engine's own documented
  reading of `-e NAME` from the client environment.
  - **Refusal.** `ErrInvalidLaunchEnv` is returned by `LaunchContainer` and
    `BuildLaunchCmd` for the whole launch (nothing is started or built) when an
    entry has no `=` or a name `ValidEnvName` refuses: a bare `-e NAME` would copy
    DevDesk's own variable into the container. Blank entries are skipped. The text
    never holds the entry, nor even an equals sign.
  - **One name rule.** `docker.ValidEnvName`: ASCII only,
    `^[A-Za-z0-9_][A-Za-z0-9_.-]*$`. A dot or a dash inside the name and a leading
    digit pass. Refused: a wildcard (`*`, `?`: Podman expands `-e PREFIX*` to every
    variable of DevDesk's environment starting with `PREFIX`, so `*=x` would copy
    `GITHUB_TOKEN`, the proxies or `SSH_AUTH_SOCK` into the container), `=`, white
    space, a leading dash (it would read as an engine flag), and any non-ASCII
    character (invisible and bidirectional ones such as U+200B and U+202E, accents,
    emoji). The launch form calls the same function, so the two cannot disagree.
  - **`NAME=` (empty value).** The launcher accepts it (`-e EMPTY`, `EMPTY=` in the
    child's environment); the form refuses it (a variable left empty is almost
    always one that was not filled in).
  - **The form** (`internal/ui/oci_resources`): `validate()` returns one
    `FieldError` of key `env` when an entry has no value or a refused name;
    `applyInvalid()` underlines the field (`marks.Sync`, Rule 140), the footer
    carries a fixed sentence at level Error derived on every render, and
    `updateActiveInput` revalidates while the field is edited. Opening the form and
    `ApplyCached` validate nothing: a name restored from the cache with no value
    is refused at the submit only (see "The launch cache" below).
  - **Limits.** A value cannot hold a comma (the field is split on commas). With
    duplicates (`A=1, A=2`) the argv holds `-e A` twice and the last value wins in
    the child.
  - **The log.** A refused `run` returns a `*docker.LaunchError`: its `Error()`
    keeps the engine's own output (`TestALaunchThatDockerRefusesCarriesTheReason`
    requires it in the error, and the view shows a fixed sentence), but
    `handleContainerLaunchComplete` logs `docker.LaunchLogText(err)`, which is
    `<engine> run failed (exit code N)` for it, `<engine> run failed (could not
    start the process)` when the process never started (a fixed phrase, never the
    error text), and the error as it is for any other. `Summary()` is the safe
    form. `TestTheLaunchCompletionNeverLogsTheRawError` reads the source and fails
    when that handler passes `msg.Err` to a log call. Limit: any new log site that
    prints a `ContainerLaunchCompleteMsg.Err` is not seen by that guard.
  - **Reserved names (OC-15, DEVDESK-813).** The typed variables are in the
    environment of the engine CLI process itself, so a name also drives the CLI,
    not only the container (`DOCKER_HOST=tcp://other:2375` would send `docker run`
    to another daemon). `docker.ReservedEnvName(name)`
    (`internal/docker/launch_reserved.go`) refuses such a name, ignoring case, and
    the form and the launcher share it with `ValidEnvName`. `LaunchContainer` and
    `BuildLaunchCmd` return `docker.ErrReservedLaunchEnv` (an
    `ErrInvalidLaunchEnv` too, `errors.Is`), with a fixed text that holds neither
    the name nor the value (a name field can hold a pasted secret), before
    anything is started or built; `launchEnviron` and `buildLaunchArgs` also drop
    a reserved entry, so `cmd.Env` never holds one the form typed, whatever the
    caller did. The form (`envFault` in `launch_form.go`) underlines the field
    (Rule 140) and the footer says, at level Error, `A reserved environment
    variable name cannot be set from this form` for the first faulty entry, with
    the same timing as the OC-14 errors (nothing before the first refused submit,
    then live). The match is exact on the name, plus two prefixes and one suffix:
    - **docker client** (read by the CLI, documented environment variables of
      `docker`): `DOCKER_HOST` (the daemon), `DOCKER_CONFIG` (client
      configuration and credentials directory), `DOCKER_CONTEXT` (selected
      context, overrides `DOCKER_HOST`), `DOCKER_TLS_VERIFY` and
      `DOCKER_CERT_PATH` (TLS to the daemon), `DOCKER_API_VERSION` (pinned API),
      `DOCKER_TLS` (TLS switched on by any non-empty value) and
      `DOCKER_CUSTOM_HEADERS` (HTTP headers added to every request; experimental).
    - **podman client** (podman's manual): `CONTAINER_HOST` and
      `CONTAINER_CONNECTION` (the remote service, and they turn remote mode on),
      `CONTAINERS_CONF`, `CONTAINERS_STORAGE_CONF` and
      `CONTAINERS_REGISTRIES_CONF` (the files it loads), `CONTAINERS_CONF_OVERRIDE`
      (a config file loaded last, containers.conf(5)), `CONTAINER_SSHKEY` (the SSH
      identity for a remote service), `PODMAN_CONNECTIONS_CONF` (the file holding
      the system connections), `REGISTRY_AUTH_FILE` (registry credentials),
      `XDG_RUNTIME_DIR`, `XDG_CONFIG_HOME` and `XDG_DATA_HOME` (rootless sockets,
      configuration and image storage; `XDG_RUNTIME_DIR` is also how the docker CLI
      finds a rootless socket). Confirmed in the docker CLI reference, the podman
      manual and containers.conf(5), except `REGISTRY_AUTH_FILE`, `HOME`, `PATH`
      and `SSH_AUTH_SOCK`, kept by reasoning (they are not in the pages read).
    - **what both resolve things through**: `HOME` (`~/.docker`,
      `~/.config/containers`, `~/.ssh`), `PATH` (which credential helper,
      `ssh` or compose plugin is started), `SSH_AUTH_SOCK` (the agent an
      `ssh://` host uses).
    - **Windows** (reserved on every platform: a name is refused by what it is,
      not by where DevDesk runs; all by reasoning, not read in the docker or
      podman pages): `USERPROFILE` (the Windows `HOME`, `%USERPROFILE%\.docker`),
      `APPDATA` and `LOCALAPPDATA` (configuration and data), `PATHEXT` (which
      extensions a helper may have), `SYSTEMROOT` and `WINDIR` (system DLLs and
      tools), `COMSPEC` (the interpreter `cmd.exe` starts).
    - **storage and podman**: `STORAGE_DRIVER` and `STORAGE_OPTS` (the storage
      driver and its options, in the podman manual), `CONTAINERS_HELPER_BINARY_DIR`
      (where podman finds its helper binaries, by reasoning), `CONTAINER_PASSPHRASE`
      (the passphrase of the SSH identity; by reasoning, absent from the manual
      page read).
    - **ssh, TLS, Go** (by reasoning): `SSH_ASKPASS` and `SSH_ASKPASS_REQUIRE`
      (a program ssh runs to ask for a passphrase, for `ssh://` hosts), `GODEBUG`
      (Go runtime settings of the CLI, including TLS ones), `SSL_CERT_FILE` and
      `SSL_CERT_DIR` (the trust roots of every TLS connection the CLI makes).
    - **telemetry, trust, platform** (by reasoning, except
      `DOCKER_DEFAULT_PLATFORM`, in the docker CLI reference):
      `DOCKER_CLI_OTEL_EXPORTER_OTLP_ENDPOINT` (where the CLI sends its
      telemetry), `DOCKER_CONTENT_TRUST` and `DOCKER_CONTENT_TRUST_SERVER` (content
      trust and its notary server), `NOTIFY_SOCKET` (the systemd notification
      socket), `DOCKER_DEFAULT_PLATFORM` (the platform `run` asks for),
      `DOCKER_AUTH_CONFIG` (registry credentials given inline).
    - **directories**: `TMPDIR` (in the podman manual), `XDG_CACHE_HOME` and
      `XDG_STATE_HOME` (by reasoning, like `XDG_DATA_HOME`: the XDG base
      directories of the client).
    - **prefixes** `LD_` and `DYLD_` (`LD_PRELOAD`, `LD_LIBRARY_PATH`,
      `DYLD_INSERT_LIBRARIES`): the dynamic loader runs the typed library inside
      the CLI. `OTEL_` (the OpenTelemetry SDK family: endpoint, headers,
      exporter; by reasoning) and `_CONTAINERS_` (podman's internal variables
      between its own processes; by reasoning). No `DOCKER_`, `CONTAINER_`,
      `CONTAINERS_` or `PODMAN_` prefix: `DOCKER_HOSTNAME` must stay accepted
      (criterion 2), so those names are listed one by one.
    - **suffix** `_PROXY` (`HTTP_PROXY`, `NO_PROXY`, `MY_PROXY`, any case): the
      CLI's own connections (daemon, registry) would go through the proxy typed.
      Not `PROXY` alone, not `MY_PROXYX`.
    - Not reserved, on purpose: `DOCKER_HOSTNAME`, `MY_PATH`, `PATHS`,
      `HOMEPAGE`, `CONTAINERS_X_Y`, `LDAP_URL`, `XDG_DATA_DIRS`, `TMPDIRS`,
      `SSH_ASKPASS_X`, `OTELX`, `MY_OTEL_X`, `STORAGE_X`, `GODEBUGX`; and, with no
      effect on `run`, `BUILDKIT_*`, `DOCKER_BUILDKIT`, `COMPOSE_*`, `GIT_SSH*`,
      `LANG`, `LC_*`. A name that merely looks like a reserved one is an ordinary
      variable of the container. Tests: `internal/docker/launch_reserved_test.go`
      (the table, both engines, a strict stub that fails on any engine call for a
      refusal, a fuzz target whose seeds run in `go test`; it checks the refusal
      itself and also `cmd.Args` and `cmd.Env`, so it does not rest on the
      filtering of `launchEnviron`) and
      `internal/ui/oci_resources/launch_reserved_test.go`. The form says
      `reserved` only for a well-formed entry (a `=` and a valid name): `PATH`
      with no `=`, or a non-ASCII name that case-folds to a reserved one, gets the
      generic sentence.
    - **Limits.** It is a deny list: a name outside it that drives an exotic
      client setting is not seen. Not covered: `BUILDKIT_*`, `COMPOSE_*`,
      `GIT_SSH*` and any other `DOCKER_`, `CONTAINER_`, `CONTAINERS_` or
      `PODMAN_` name that is not listed above (a client that grows a variable
      reads it before this list does). Names are compared case-insensitively, with
      no other normalization; non-ASCII names are already refused by
      `ValidEnvName`. A name cached by an older build and restored into the form is
      refused at the submit like any other. Passing the values through
      `--env-file /dev/fd/3` instead, which would keep a name from reaching the
      CLI's own environment, was set aside by decision D13 of the OC plan (not the backlog's D13): DevDesk refuses, it
      does not use an `--env-file`.
  - **The preview and `ctrl+y` (OC-16, DEVDESK-814).** `buildCommandLines` (the
    two-column preview) and `buildCommandString` (what `ctrl+y` copies) print the
    launcher's own words: `-e NAME`, never a value, for every variable (no
    "sensitive" class). `previewEnvName` names an entry only when `envEntryName`
    accepts it, the same function `envEntryValid` (the form's validation) uses:
    `NAME=VALUE`, a name `docker.ValidEnvName` accepts (not trimmed, so
    `API_TOKEN =x` is not named), a value that is not empty and not only `=`
    characters. The prefix before the first `=` of such an entry is the name and
    stays visible. Every other entry is shown as the fixed marker `-e <invalid>`,
    never repeated: no `=` at all (`ghp_...`, `glpat-...`, a bare `API_TOKEN`), an
    empty value (`NAME=`), a base64 token (`abcdef==`), a refused name. The form
    refuses all of these at the submit anyway. The marker is a redirection in a
    POSIX shell and PowerShell refuses `<`, so a pasted command fails instead of
    running without the variable. Reserved names
    (OC-15) are not filtered here: the form refuses them at the submit. The
    clipboard goes through the seam `writeClipboard` (`commands.go`, tests replace
    it) and the footer after a copy is the Info `commandCopiedText` ("Command
    copied to clipboard, environment values are not copied"). The form help says
    the same (Launch Form section). The only place the value is still drawn is the
    Environment input the user types in. Tests:
    `internal/ui/oci_resources/launch_preview_names_test.go`.
  - **The launch cache (OC-17, DEVDESK-815, S9).**
    `~/.devdesk/cache/launch-options.json` keeps the names of the Environment
    variables and never a value. `LaunchOptionsEntry.EnvNames` (`env_names`)
    replaced the `Env` string; `ToCacheEntry` fills it through `cache.EnvNames`
    (the part before the first `=` of each entry; an entry with no `=`, no name or a
    value made only of `=` is dropped, since what sits there is the secret itself,
    a token pasted into the field), and `Set` cuts anything after a `=` again, so no
    caller can write a value. The file is written through `writeFileAtomic` (a
    temporary file, `chmod 0600`, rename), so an existing file with a wider mode
    comes back 0600; a cache directory DevDesk creates is 0700 (an existing one is
    not touched). On Windows `chmod 0600` only drops the read-only attribute: the
    protection there comes from the ACL the file inherits from the user profile.
    **Migration:** the file has no version field (a bare map); an
    entry still carrying the old `env` key is read as the legacy shape
    (`legacyLaunchEntry`), its names are kept, the values are dropped, and
    `NewLaunchOptionsCache` rewrites the file at once (also when `env_names` itself
    held a `NAME=VALUE` written by hand). The names kept from a legacy field are the
    ones the form accepts (`docker.ValidEnvName`: ASCII, no space, no `*`, no
    invisible character), at most 128 bytes, and not starting with a known token
    prefix (`ghp_`, `gho_`, `ghu_`, `ghs_`, `github_pat_`, `glpat-`, `xoxb-`,
    `xoxp-`, `AKIA`); a reserved name (OC-15) is kept and refused at the submit. New
    writes are not filtered again (the submit validated them). The migration runs
    when the OCI view starts (`Init` batches `migrateLaunchOptionsCmd`, which only
    opens the cache and returns no message) and again whenever the cache is
    opened. An old entry with `"env": ""` holds no value and is left as it is until
    the next save. A file that is **corrupt** (not valid JSON, a mistyped field) or
    **larger than 1 MiB** (`maxLaunchCacheBytes`, the read is bounded) is
    **removed** on the open, since the cache is regenerable and the file may hold
    values an older build wrote (the old `os.WriteFile` was not atomic); the form
    opens empty and the log is the fixed line `cache file is not valid JSON or too
    large, removed`: never a byte of the file, its path or the parser's error, which
    quotes the byte it stopped on. **The form:** `ApplyCached` writes each name as `NAME=`
    (joined with `, `) and sets `envRestored`; while one of those entries still has no
    value the Environment hint reads `Values are not saved: type them again`
    (`envRestoredHint`, a hint, not an error: nothing is underlined and the footer
    stays empty at opening, Rule 140). An edit that leaves no entry without a value
    ends the restored state (`releaseRestoredEnv`): the hint does not come back for
    a value the user emptied. A reserved or invalid name (from a current-format file)
    stays in the field and is refused at the submit like any other. Tests:
    `internal/cache/launch_options_names_test.go`,
    `internal/cache/launch_options_hardening_test.go`,
    `internal/ui/oci_resources/launch_cache_names_test.go`,
    `internal/ui/oci_resources/launch_cache_hardening_test.go`. Limits: a legacy
    `"env"` such as `TOKEN=a,b=c` keeps `b` as a name (the field is split on commas,
    so the piece after a comma of a value cannot be told from an entry), and
    `ghp_x=y` style secrets that pass the checks above keep their prefix as a
    "name"; a name accepted by the form that merely looks like a token cannot be
    told from a variable.
  - **Elsewhere.** The scanner
    `internal/scan/plumber.go` put `-e GH_TOKEN=<token>` in an argv: fixed by
    DEVDESK-945 (`-e GH_TOKEN`, the value in `containerRun.Env`), with the static guard S9 below.

- **A launch asks before it mounts something sensitive of the host (OC-20,
  DEVDESK-818, requirement S4).** `docker.SensitiveMounts(opts)`
  (`internal/docker/launch_mounts.go`) is a reader: it classifies the bind mounts
  of `ContainerLaunchOptions.Volumes` (`-v`) and the new `Mounts` (`--mount`, the
  value of each, emitted by `buildLaunchArgs` as one `--mount VALUE` pair right
  after the `-v` pairs), volumes first, in the order given, and returns one
  `SensitiveMount{Spec, Source, Path, Category, Reason, EngineSocket}` per
  sensitive one (nil when none). It starts nothing, creates nothing and never fails;
  `LaunchContainer` and `BuildLaunchCmd` do not call it and launch what they are
  given (`TestLaunchContainerDoesNotSecondGuessASensitiveMount`): the confirmation
  is the form's.
  - **What is sensitive** (`MountCategory`), the source being equal to, under, or
    an ancestor of it: `root` (`/`, and `/proc` with everything under it: the root
    and the working directory of any process, `/proc/<pid>/root` of PID 1 being the
    host's, are reached through it). **`root` also sets `EngineSocket`**: it holds
    every engine socket, so mounting `/` (or `/proc`) gets the named choice, the
    Error footer and the engine-socket text, like `/var`; the other categories keep
    their own, a socket under `/etc` or `~/.ssh` included,
    `system-config` (`/etc`), `home` (`$HOME` itself, not what is under it),
    `ssh` (`~/.ssh`), `devdesk` (`~/.devdesk`), and `engine-socket`: the Docker,
    Podman and containerd sockets at their usual places (`/var/run/docker.sock`,
    `/run/docker.sock`, `/run/podman/podman.sock`, `/var/run/podman/podman.sock`,
    `/run/containerd/containerd.sock`), `$XDG_RUNTIME_DIR/docker.sock` and
    `$XDG_RUNTIME_DIR/podman/podman.sock` (the rootless ones, under
    `/run/user/<uid>` when `XDG_RUNTIME_DIR` is unset), the `unix://` path of
    `DOCKER_HOST` or `CONTAINER_HOST` (a `tcp://` or `ssh://` host names no file),
    and **any source that is a socket file** (`os.Stat` says `ModeSocket`: a link to
    a socket, a copy of one, whatever its name; a socket under `/etc`, `~/.ssh` or
    `~/.devdesk` keeps that category). An **ancestor** is flagged with the category
    of the sensitive path it holds (review of OC-20, a decision of the orchestrator
    on the security review: the criterion "equal or under" was extended): `/run`,
    `/var/run` and `/var` are `engine-socket`, the parent of `$HOME` is `home`; a
    path that is neither equal, under nor an ancestor (`./data`,
    `$HOME/projects/app`) is not flagged. The comparison is by whole components
    (`/etcetera` is not `/etc`). Each category has one fixed `Reason` sentence,
    never built from what was typed (the modal and the log print it). The home
    categories use `HOME`, or the user database (`os.UserHomeDir`, then
    `user.Current`) when it is unset or not absolute; `~` is expanded with `HOME`
    only. `~/.ssh` and `~/.devdesk` are resolved too (they are often links: stow,
    a dotfiles manager), and the target of the link is as sensitive as the link.
  - **`unverified`.** A path DevDesk cannot read for a reason other than "does not
    exist" (permission denied on a parent, an unreadable link, as `/root/...` seen
    by a user) cannot be proven harmless: it is flagged with the category
    `unverified` and the reason `DevDesk could not check where this path leads, so
    it may expose this host.`, unless it is sensitive anyway. A missing path, a file
    read as a directory and a loop of links are not flagged for that.
  - **What is a bind source.** `-v`: the part before the first `:`; `--mount`: the
    `source`/`src` of a value whose `type` is `bind` or Podman's `glob` (any order
    of keys; `target`, `dst`, `destination` are the container's and never read).
    The keys and the value of `type` are read in any case (`TYPE=BIND`, `Src=`):
    the engine lowercases them, and Podman is read the same way. A `type=volume`
    value (or one with no type) is also a bind when it has
    `volume-opt=device=<path>` (the local driver's bind in disguise). A source is a
    path when it starts with `/`, `.` or `~` (the Docker CLI makes any
    `.`-prefixed source absolute, so `.ssh` is `./.ssh`); a named volume, an
    anonymous volume (`/etc` alone), `type=tmpfs`, a device that is not a path and a
    value with no bind type are not checked. **A glob** (`type=glob`, Podman) is
    expanded with `filepath.Glob` and each match classified, the first sensitive
    one reported (`/e?c` is `/etc`, `/home/*/.ssh` is the `.ssh` of each home). When
    it matches nothing, is not a valid pattern or matches more than 1000 paths, the
    directory of its literal prefix is classified instead (`/etc/zz*` is `/etc`;
    `/` is never classified for a wildcard in the first component), and a pattern
    that could not be expanded and says nothing sensitive is `unverified`. A
    `type=bind` source is never a pattern. A leading `~` (alone or `~/`) is expanded
    with `HOME` although the engine would not do it (the hint is read the way it
    was meant); with `HOME` unset or not absolute it is not expanded and flags
    nothing. A relative source is taken from DevDesk's working directory.
  - **Resolution.** The path is resolved before the comparison, component by
    component (`resolvePath`): a link is followed where it stands, so a `..` after
    a link applies to the link's target, as the kernel does
    (`TestSensitiveMountsResolvesLinksBeforeNormalising`). The Docker CLI cleans the
    path first (`..` applied to the link's name), so **both readings are evaluated**
    and the source is flagged when either is sensitive
    (`TestSensitiveMountsReadsABothWaysAfterALink`).
    At most 40 links are followed (a loop ends and the path is kept as written).
    `HOME`, `/etc` and the engine sockets are resolved too, so `/var/run/docker.sock`
    is the socket when `/var/run` is a link to `/run`. A path that does not resolve
    in full (a missing component, a dangling link) is compared as written after
    normalization and then as resolved so far, and `Path` is the first of the two
    that is sensitive: a missing `/var/run/docker.sock` is reported as typed. A
    reading that holds a sensitive path (an ancestor) is only looked at after every
    candidate has been compared for equal or under.
  - **The form** (`internal/ui/oci_resources/launch_mounts.go`). The check runs when
    the launch is submitted (`handleLaunchFormSubmitWithCache`), before anything is
    started or handed to the terminal, `-it` launches included. The Volumes field
    takes `--mount` values too: it is split on commas (`splitVolumeField`), except
    that a value starting with `type=` runs on over its own `key=value` tokens and
    the bare `readonly` and `ro`, until the next `type=` token or the first other
    token (a `-v` entry). A `--mount` value that does not start with `type=` is not
    recognized; any other bare flag (`volume-nocopy`) ends the value; a `-v` entry
    that looks like `key=value` right after a mount is read as part of the mount.
    The preview and `ctrl+y` show `--mount VALUE`. When the classification is not
    empty a `components.ChoiceModal` opens over the form (`Model.mountModal`, the
    held launch in `mountPending`); `launchForm` stays open, so Cancel gives the
    form back with its values and its focus on Launch. The modal has the keyboard
    (`handleKeyMsg` first, pastes dropped) and `Render()` draws it instead of the
    form. It names the resolved `Path` (control and format characters replaced
    by `?`) and the `Reason` of every sensitive mount, one action and Cancel,
    **Cancel being the default**: `enter`, `esc` and a second `right` cancel; the
    way to the action is `left` then `enter`, and no letter (`y` included) answers.
    The text is wrapped to the terminal (`ansi.Wrap` for the sentences, `ansi.Hardwrap`
    for a path, so nothing of it is cut off) and wrapped again, with the focus
    back on Cancel, when the terminal is resized while the modal is open. The modal
    opens whatever the state of the form: a `LaunchFormSubmitMsg` that arrives after
    `esc` closed the form (the message was in flight) is held like any other,
    and Cancel copes with a form that is gone, so nothing sensitive starts unasked.
    The action is `Mount anyway`, or `Mount the engine socket` when any mount is an
    engine socket, even among others; in that case the modal says the container
    gets control of the engine, and the footer says it at level Error
    (`mountStatus`, derived in `formStatus` on every render, no timer, Rule 128).
    Confirming runs the launch exactly as before (`startLaunch`: cache snapshot,
    detached or handed to the terminal). The log line names the categories, never
    a path. Typing a sensitive path is not an error of the form (Rule 140).
  - **Limits (not done, known).** **Time of check, time of use**: DevDesk resolves
    the path when the form is submitted and the engine resolves it again when it
    starts the container, so a link changed in between is not seen. Windows has no
    guard: `SensitiveMounts` returns nil there (the paths and the engine's named
    pipe differ). macOS reads the Linux layout, so Docker Desktop's VM-side paths
    are not known, and its file system is case-insensitive by default, so `~/.SSH`
    is not seen as `~/.ssh`. Not seen: a `--mount` value in CSV quotes, a mount
    made by `--volumes-from`, a device (`--device`), a compose file, and a path the
    engine resolves differently from the file system DevDesk sees (a remote
    engine: `DOCKER_HOST` over `tcp://` or `ssh://`: the paths are the remote
    host's, and the check reads this machine's). A bind-mounted parent that holds
    no known sensitive path by name is not flagged. A glob only sees what
    `filepath.Glob` lists: a directory it cannot read is skipped, not reported.
    **DEVDESK-948** (not in OC-20): the classification runs in `Update`, and
    reads the file system synchronously (a hung NFS or autofs mount, or
    `user.Current()`, would freeze the view): to move into a `Cmd` with a timeout;
    an unknown socket (an ssh-agent outside `~/.ssh`) gets the engine reason, a
    distinct one is wanted; and the paths outside the perimeter, `~/.docker`,
    `~/.kube`, `~/.gnupg`, `/var/lib/docker`, `/dev` and `/sys`, are not flagged.
    A typed path is drawn through `printable` (control, format, and the line
    separators U+2028 and U+2029 become `?`), and wrapped without any indentation
    (`wrapPath`), so that a space of the path at the start of a wrapped line is not
    taken for the layout of the modal; the reason of each mount is the indented line.
    Tests: `internal/docker/launch_mounts_test.go`,
    `internal/docker/launch_mounts_argv_test.go`,
    `internal/ui/oci_resources/launch_mounts_test.go`.

- **A launch refuses what reads as a flag and asks before the host network (OC-21,
  DEVDESK-819, requirement S4).** Nothing the form or the launcher is given can
  produce `--privileged`, `--pid=host` or `--cap-add=SYS_ADMIN`: `buildLaunchArgs`
  emits only `--pull=never`, `--rm`, `-d`, `-i`, `-t`, `--name`, `--entrypoint`,
  `-p`, `-e`, `-v`, `--mount`, `--network` and `--user`, each typed value one argv
  element (`launch_privileged_test.go`, a property over 700 draws on both engines).
  - **The launcher.** `LaunchContainer` and `BuildLaunchCmd` return
    `docker.ErrInvalidLaunchOption` (`validateLaunchOptions`, before the
    environment check and before the engine is looked up: nothing is built or
    started) when `Image`, `Name`, `Entrypoint`, `User`, `Network` or an entry of
    `Ports`, `Volumes` or `Mounts` starts with `-`, untrimmed (the form trims).
    The error and `LaunchLogText` never hold the value, nor the field. `Ports` is in
    the list although `LoopbackPort` rewrites a spec: it only prefixes a spec that
    names no address, and `--privileged;x:80` holds two colons, so it read
    `--privileged;x` as an address and left the element as it was. `Env` is left
    alone: `NAME=-x` is a value, it travels in the child's environment (OC-14), and
    a name starting with a dash is refused by `ValidEnvName`. A dash anywhere but
    first, a path (`/`, `.`, `~`), a volume name and a `--mount` value pass.
  - **The host network is the form's to ask about**, as the mounts are (OC-20):
    `docker.SharesHostNetwork(network)` is `host` once trimmed, without regard to
    case, and the launcher does not call it (`TestLaunchContainerDoesNotSecondGuessTheNetwork`:
    `--network host` stays two argv elements). The submit holds the launch behind the
    modal of OC-20 (`holdForConfirmation`, `mountCheck.hostNetwork`): **one modal
    however many reasons**, Cancel by default, `left` then `enter` the only way to
    the action, no letter answers. Host network alone: title `Host network`, the
    sentence `The container shares the network of this host, loopback included` and
    the action `Use the host network`. With sensitive mounts it names every mount
    and that sentence, and the action reads `Mount and use the host network`, or
    `Mount the engine socket and use the host network` (the footer is then the
    socket's Error). A network restored from the cache is asked about like a chosen
    one, interactive launches included. While the Network field holds the host
    network the footer says `This container will share the network of this host` at
    level Warning, derived on every render (in the form and with the modal open; an
    Error of the form wins), and the preview shows the `--network host` line in
    `theme.ColorFooterWarn`; the text `ctrl+y` copies stays plain (`theme.PreviewWarnStyle` is the screen's
    colour only). That copied text is pasted into a shell, so `shellQuote` quotes
    every value that holds a character outside `[A-Za-z0-9_./:@%+=,-]` (`*`, `?`,
    `[`, `~`, `#`, `;`, `$`, `(`, `&`, `|`, a space...), the `-p` entries included.
    A submit that arrives while the modal holds another one, or after the launch of
    the same form started (every `enter` builds one message, several can be in
    flight), is dropped (`isDoubleSubmit`): the held launch is never replaced and no
    modal opens over a form that is gone.
  - **A dash in the form** (`LaunchForm.dashFaults`, part of `validate`). Name,
    Entrypoint, User (trimmed) and each entry of Volumes and Additional ports (the
    entries the submit splits the field into) that start with `-` refuse the submit:
    the input is underlined (`theme.StyleInvalidField`, `marks.Sync`), revalidated on
    each change after the first refused submit, and the footer says
    `<Label> cannot start with a dash` at level Error (Rule 140), the first field on
    screen only (the others are underlined with no sentence: a `FieldError` with an
    empty `Text` is not counted in `(+N more)`). Network (a cycle field) and Image
    (not a field) have nothing to underline: the footer alone says `Network cannot
    start with a dash` or `Image cannot start with a dash`. The form accepts only what
    `BuildLaunchCmd` accepts (`TestAnyCombinationOfFormFieldsIsSafe`, 300 draws).
  - **Limits.** `container:<id>` (joining another container's network namespace) and
    `none` are not classified: no modal, `--network` as typed, and never a `--pid` or
    `--ipc` with it (follow-up). The entrypoint is one argv element, which both
    engines read as the path of one executable (`sh -c "x"` is stored safely, then
    fails in the engine); `VerifyEntrypoint` still splices it into `sh -c "command -v
    ..."` (the temporary exception of guard I1, DEVDESK-947). The check is on what
    starts the value: a value that reads as a flag after a separator the engine
    splits on is not a dash value. Tests: `internal/docker/launch_privileged_test.go`,
    `internal/ui/oci_resources/launch_dash_values_test.go`,
    `internal/ui/oci_resources/launch_host_network_test.go`.

- **Four typed, read-only reads for the path trace** (`container_network.go`,
  DEVDESK-330, DEVDESK-331), each with a `context.Context` that kills the
  invocation: `InspectContainerNetworking(ctx, id)`, `ListContainersContext(ctx,
  all)` (`ListContainers(all)` is unchanged and does not use it),
  `EngineOperatingSystem(ctx)` and `InspectNetworkBridge(ctx, networkID)`. They
  run `container inspect`, `ps`, `info` and `network inspect` and nothing else. `InspectContainerNetworking` runs
  `container inspect --format <containerNetworkingFormat> <id>`: the format
  selects `Name`, `HostConfig.NetworkMode`, `State.Running` and, per network,
  `NetworkID` (`EndpointNetwork.NetworkID`, kept only when it passes
  `IsContainerID`, else empty), `IPAddress`, `Gateway` and `GwPriority`, so `Config.Env`, labels and mounts are
  never read into memory. It reads stdout only, and its error carries the
  command label, never the output. The ID must pass `IsContainerID` and is one
  argv element, never a name. The inspect and `info` reads refuse a non-Docker
  engine with `ErrDockerOnly` before any invocation; output over 256 KiB
  (`maxInspectBytes`) is refused. `InspectContainer`, which returns the whole
  `docker inspect` JSON (secrets included) with no context, was not reused for
  that reason. It returns **stdout only** (OC-04, DEVDESK-800): a warning the
  engine writes on stderr never enters the viewer's document; on failure the
  message carries the stderr that `cliRunner` leaves on the `*exec.ExitError`. The `info` format
  (`{{.OperatingSystem}}`) is a docker-only literal and is not in
  `engine.Templates`.
- **Container logs for the pager (`docker/containers.go`, OC-84, DEVDESK-801).** `LogsCmd(id, tail)` validates the ID with `IsContainerID` and builds `<engine> logs --tail N <id>` through `runner.Build`, with no stream set: the caller pipes it into the pager (`teaexec.Pipe`, no shell). A malformed ID builds nothing and returns an error wrapping `ErrInvalidContainerID` (text: `... is not a container ID`). `GetContainerLogs` validates the same way, so an ID such as `-f` is never read as an option.
- **Shell in a container (`docker/shell.go`, OC-04, DEVDESK-800).** `DetectShell(id)` runs `<engine> exec <id> which bash` through the runner and answers `/bin/bash`, else `/bin/sh`. Any failure answers `/bin/sh`, and so does no answer within `shellDetectTimeout` (8 s, the call's `dockerCmd.Ctx`): the `exec -it` that follows then reports the real problem in its own terminal. It waits on the daemon, so the containers view calls it only from a `tea.Cmd` (`detectShellCmd`), never from `Update`: `T` sets `shellDetecting` (footer `Opening shell…` with the activity dot, `WantsBlink` true) and returns the Cmd; its `ShellDetectedMsg` clears the flag and `handleShellDetected` starts the shell, in place (`teaexec.Process`) or in a new window (`uiterminal.ForCmd`). A second `T` while one is pending is refused with a footer warning. The answer is routed to the containers view wherever the user is (`App.routeShellDetected`, `docs/architecture/app-shell.md`): if the view is off screen the message arrives with `Abandoned` set, the wait ends and **no shell is started**. `ShellCmd(id, shell)` builds `<engine> exec -it <id> <shell>` through `runner.Build`; its `Args` are the argv given to the terminal emulator. **The ID is validated (OC-18, DEVDESK-816, DEVDESK-939).** `DetectShell(id) (string, error)` and `ShellCmd(id, shell) (*exec.Cmd, error)` call `requireContainerID` first: a malformed ID returns `ErrInvalidContainerID` (an empty shell for `DetectShell`), and nothing is built or run. `ShellCmd` also accepts only the two shells `DetectShell` answers, `/bin/bash` and `/bin/sh`: any other value returns `ErrInvalidShell` and builds nothing. A missing bash is still not an error (`/bin/sh, nil`). The session is always an argv array, one element per word, never an interpreter's command line: no `sh -c`, no joined string, in place or handed to the emulator (`internal/ui/terminal.ForCmd` appends the argv untouched after the emulator's own words, `terminal_test.go` holds it for each supported emulator). In the containers view, `T` refuses a container whose ID is not one before setting `shellDetecting` (no wait, no process), and `handleShellDetected` refuses a `ShellDetectedMsg` that carries a malformed ID or an `Err` the same way (defence in depth against a stale or forged message): both log the quoted value and show `Cannot open a shell — container ID refused` through `components.FooterMessage` at level Error, without echoing the ID.
- `internal/docker/netdiag.go` — **gone**. §3.33 took the five probes that did not need a container, §3.43 the ports table, §3.44 the topology (and with it `docker/topology.go` and the last `--privileged`), §3.47 the route trace and the file itself (the trace came back in the process as `internal/nettrace`, DEVDESK-327). §3.61 removed the OCI connectivity test, and with it the last container this application runs for a diagnostic: `networks.go` now holds only `ListNetworks`, `CreateNetwork`, `RemoveNetwork`, `PruneNetworks` and `InspectNetwork`.

### Tests and the fake CLI — `internal/testutil/fakecli` (OC-02, DEVDESK-797)

One fake binary stands in for `docker`, `podman`, `kind`, `kubectl` and the scanners in
tests; the code under test starts it by name through `PATH`, as it starts the real tool.

- `fakecli.Install(t, src, tools...)` links the test binary under each tool name in a directory that
  becomes the whole `PATH` (a copy on Windows), and returns a `*Fake` (`Dir()`, `Invocations()`).
  `Source` is `Exchanges() []Exchange`; `Scenario` is the literal one, and `fakecli.Fixtures` (OC-08a,
  below) is the recorded one. An `Exchange{Tool, Args, Prefix, Reply{Stdout, Stderr, Exit}}`
  matches an argv element by element: exact beats prefix, the longest prefix wins, then the first
  declared; an empty `Args` with `Prefix` is a catch-all, an empty `Tool` any tool.
- The test binary answers as the tool when `fakecli.Main(m)` is the body of `TestMain`
  (`os.Exit(fakecli.Main(m))`). A `TestMain` that prepares state first (a temporary `HOME`) calls
  `fakecli.AsTool()` before it, as `internal/ui/oci_resources`, `internal/scan` and `internal/ui/netdiag` do.
  Without it the fake would run the whole suite again each time it is started, so `Install` and
  `InstallAt` fail the test (`t.Fatalf`) unless `AsTool` ran in the process (an atomic flag set at the top
  of `AsTool`, which `Main` calls). A tool name installed twice in one directory fails too, instead of
  writing through the first link into the test binary.
- `(*Fake).InstallAt(t, dir, name)` links one more tool outside `PATH` and answers the same scenario: the
  binary a configuration names by its path (`scan.trivy_path`) or an engine shape's `Binary`
  (`engine.Shape.Binary`, `scan` container tests, `netdiag` path reads). The child environment is what
  the code under test gave it, and `Invocation.Env` reads it back.
- Strict mode: an argv no exchange covers is written on the tool's stderr as `%q`, exits 127, and
  fails the test at cleanup with the tool and the argv. `Invocations()` holds every run (tool, argv,
  environment, matched), oldest first; the journal is one JSON line per run in a file under
  `t.TempDir()`.
- `Install` sets `PATH` through `t.Setenv`: no `t.Parallel`, and no real tool is reachable.
- Strictness: `ui/oci_resources` declares every argv it expects (`fakeScript` keys are prefixes). The
  `scan` helpers `installTools`, `asScanner` and `installToolAt` answer every argv of their tools with a
  `Prefix` catch-all, so they are not strict, as before: those tests are about what detection or the
  runner concludes from the answer, not about the argv. A test that cares installs its own scenario.
- Migrated: `ui/oci_resources` (`installFakeDocker`/`installFakeTools` keep their prefix-script
  shape on top of `fakecli`), `scan` (`installTools`, `asScanner`, `installToolAt`, and the two
  `containerToolVersion` tests that reach the fake through `engine.Shape.Binary`), `ui/netdiag`
  (`recordedEngineCalls`), `engine`, `docker` (credential helpers). Still on their own fake, by decision: `nexusbench` (OC-02 follow-up), `app`
  (`scan_tools_hermetic_test.go`, witness scripts for trivy, gitleaks and plumber: temporary, no story yet), and `browser` and `clipboard` for good (not engine
  tools).
- Guard (`internal/testutil/fakecli/guard_test.go`, read with `go/parser`): F1, a non-test file
  importing `fakecli` (it imports `testing`); F2, a `_test.go` file that overrides `PATH` and copies
  the test binary (`os.Executable`) or writes a `#!` script. F2 has a list of exceptions,
  `legacyFakes`, that only shrinks (a stale entry fails). It does not see a `PATH` set through
  `Cmd.Env`, a helper in another package, a fake reached through `engine.Shape.Binary` or a path in the
  configuration rather than `PATH` (the ones found are migrated, but a new one is not caught), or a shim
  without either marker.
- Bytes (OC-08a): the streams of a `Reply` reach the code under test unchanged. The scenario travels from
  `Install` to the tool process as `[]wireExchange` (unexported; `Stdout` and `Stderr` are `[]byte`, which
  `encoding/json` writes as base64) and the tool writes them with `os.Stdout.Write`, so an invalid UTF-8 byte, a
  NUL or a CR is served as it is (it used to become U+FFFD). The public `Reply{Stdout, Stderr string}` is
  unchanged: a Go string holds any bytes. The argv of an exchange and the journal are still JSON, so an argv
  that is not valid UTF-8 is rewritten on the way; `enginefixture.Parse` refuses such an argv.

### Tests and the real engine — `internal/engineguard` (OC-03a, DEVDESK-798; OC-03b, DEVDESK-799)

Guard G1. Since DEVDESK-799 (OC-03b) a test binary that resolves an engine binary outside a directory `fakecli` declared
**panics**; the survey OC-03a introduced stays as an opt-in mode (below). `internal/engineguard` is
production code (standard library only, `testing` included, as `internal/ports` does for
`testing.Testing`; it cannot import `fakecli`, which `internal/docker` would then pull into production).

- `cliRunner.LookPath`, `Run` and `Build` (`internal/docker/exec.go`) each start with
  `engineguard.Check(op, binary)`. `Run` calls it after `name` is computed, so the credential helpers
  (`docker-credential-<helper>`) go through the guard as the engine does. Outside a test binary
  (`testing.Testing()`) `Check` returns at once and resolves nothing.
- In a test binary `Check` resolves the name with `exec.LookPath` (a name that resolves to nothing is not
  recorded: nothing can start), then compares the **directory** of the resolved binary, with symbolic
  links resolved, to the declared directories. A directory is declared exactly: not its parents, not its
  children. A binary elsewhere is a reach (`Hit{Test, Op, Binary}`, `Op` is `LookPath`, `Run` or `Build`) and
  the default answer is a panic: `<binary> was resolved outside a fakecli directory during a test
  (DEVDESK-799, G1): install a fake with fakecli.Install / AllowFakeCLIDir, or call
  engineguard.AllowRealEngine(t) in a contract test; see docs/architecture/network.md, "Tests and the real
  engine" (operation <op>, reached by <test>)`. A panic rather than an error, like DEVDESK-610 and
  DEVDESK-708: an error would look like an ordinary failed call and the test could pass. It happens before
  anything is started. In a goroutine or a `tea.Cmd` nobody recovers it, so it ends the whole test binary:
  that is the point, and it names the test binary and caller.
- **Survey mode (opt-in).** Only when `DEVDESK_ENGINE_SURVEY` is set to a file, a reach is not refused: it is
  kept in memory (`Hits()`) and appended to that file with `O_APPEND`, one `Test<TAB>Op<TAB>Binary` line per
  hit, and the call goes on exactly as before. Tests of the guard and of the runner that need that mode
  call `engineguard.SurveyMode(t)` (`Isolate` plus a survey file of its own).
- `Hit.Test` is `package.TestName`, read on the stack (the first `Test*` function of a `_test.go` file,
  innermost first, so a subtest closure names its top-level test). With no test frame (a goroutine, a
  `tea.Cmd`) it reads `(no test frame) <test binary> <package> <first caller outside it>`, the test
  binary being `filepath.Base(os.Args[0])` (such as `app.test`), which says which package was under test.
- Declaring: `fakecli.AllowFakeCLIDir(t, dir)` (`engineguard.AllowDir`) declares `dir` for the length of
  `t`. `fakecli.Install` calls it on its directory and `(*Fake).InstallAt` on the directory it links
  into; a test that builds a fake by another means calls it itself (a script engine reached through
  `engine.Shape.Binary`, such as the one of `ui/containers`, declares its directory with it).
  `engineguard.Isolate(t)` gives the test a fresh record and no survey file, so a reach panics whatever
  mode the suite is in and a test that reaches an engine on purpose stays out of the list.
- `engineguard.AllowRealEngine(t)` is for the contract tests that may reach a real engine: it calls
  `t.Fatalf`, never `Skip`, unless `DEVDESK_ENGINE_CONTRACT=1` and `CI` is empty
  (`engineguard.ContractGate`), then stops refusing that test's reaches. `Isolate`, `SurveyMode`, `AllowDir`
  and `AllowRealEngine` panic outside a test binary. A contract does not call it itself: `enginecontract.Require`
  does (below, "Engine contracts").
- Negative controls: `TestATestThatStartsARealBinaryPanics` (`internal/engineguard/panic_control_test.go`)
  runs an offending test in a child process (this test binary, one test) and checks that it dies on the
  panic and goes no further; `TestCliRunnerPanicsForAnEngineOutsideTheFakeCLIDirs` does the same through
  `cliRunner` (`recover`). Neither starts an engine.
- What a test uses instead of a real engine: `fakecli.Install` (the package needs a `TestMain` calling
  `fakecli.Main` or `AsTool`) or `fakecli.AllowFakeCLIDir` for a script it writes; a seam of the package
  when there is one (`docker.ImageNames`, stubbed in the `TestMain` of `ui/security`).
- `mise run engine-survey` runs `go test -count=1 ./...` with `DEVDESK_ENGINE_SURVEY` set to a temporary
  file, then prints `engineguard.Summary` (sorted, one line per distinct reach, or `none: ...` for a clean
  suite) through `internal/engineguard/cmd/engine-survey`. Since OC-03b it is the only way to list every
  offender at once instead of stopping at the first panic. It runs the whole suite, so it is heavy and is
  part of none of `ci`, `check`, `test` or `test-race`
  (`TestMiseEngineSurveyTaskStaysOutOfTheStandardChecks`).
- Limits: only `Test*` functions are recognised, not `Fuzz*`, `Benchmark*` or `Example*`, and not a
  method; a subtest started in a helper that is not a `Test*` function, a goroutine and a `tea.Cmd` have
  no test frame, so they read `(no test frame) ...`; each test process keeps its own record (the file is what joins them, and a process the code
  under test starts is not a test binary); a reach from a goroutine has no test name, only the caller;
  a name `LookPath` cannot resolve is not seen; the credential helpers are judged on the binary they
  resolve to, like the engine; the declarations, the record and the gate are globals, so no
  `t.Parallel` in a test that uses them.
- `mise run engine-survey` exits with the status of `go test`, and when it is not 0 the list is
  prefixed with a warning that it is incomplete (a package that does not compile records nothing), so a
  `none` is never read as a clean suite.

**Guard G2, the static half (OC-05, DEVDESK-802).** `internal/engineguard/launch_guard_test.go`
(`TestOnlyTheRunnersStartTheEngine`, package `engineguard_test`, a test of the whole module) answers "who may
start the engine" at the source level, where G1 answers it at run time. It parses every non-test `.go` file under
the module root (skipping `.worktrees`, `.git`, `vendor`, `testdata`, `docs-site`, `node_modules` and dot
directories; a floor of 200 files read keeps a walk that reads nothing from passing) with `go/parser`, no type
information, and reports each site with file, line and enclosing function:

- *named*: `exec.Command` / `exec.CommandContext` whose binary argument folds to `docker`, `podman`, `kind` or
  `kubectl` (a literal, a file-level constant, a concatenation, a path or `.exe` name, `engine.Docker`,
  `engine.Podman`, `config.EngineDocker`, `config.EnginePodman`; the import may be renamed);
- *binary*: a read of `engine.Shape.Binary` through `engine.Current()`, a variable assigned from
  `engine.Current`, `ShapeFor` or `Resolve` in the same top-level declaration, or a parameter typed
  `engine.Shape` / `*engine.Shape`. `exec.LookPath("docker")` is not a site.
- Runners (`launchRunners`): `internal/docker/exec.go`, `internal/scan/container_run.go`, both required to
  still carry a site, and `internal/kind/runner.go`, optional until it exists (kind and kubectl are refused
  elsewhere from now on). A runner is exempt as a whole.
- Exceptions (`launchExceptions`), keyed by file + function with an exact `Sites` count and a mandatory reason:
  `imageIsLocal` (`internal/scan/scanner.go`) and `Detect` (`internal/scan/toolbox.go`) are temporary, to move
  behind the docker runner; `(*App).resolveContainerEngine` (`internal/app/configuration.go`) is permanent
  (it reads `Shape.Binary` for a log line, nothing is started). The pager `V` is not an exception: since OC-84
  (DEVDESK-801) it reads `<engine> logs` from `docker.LogsCmd`, and `TestThePagerIsNotAnException` fails on one.
  The guard fails on an exception whose code is gone, whose `Sites` differs from the count, without a reason,
  or in a runner, and on a duplicate or a file that no longer exists.
- Limits (pinned as cases of `TestLaunchDetectorOnSources`, so lifting one is a visible change): a name built
  at run time (`exec.Command(tc.Name, ...)` in `internal/scan/run.go` and `tool_source.go` starts the engine
  this way, from what `containerRun.cmd` returned); a constant from another file of the package; flow followed
  only inside one top-level declaration, by name and without scopes; a dot import of `engine`, a function value,
  `syscall.Exec`, `os.StartProcess`, an `exec.Cmd` literal with a `Path`; a shell line, a script, the CI;
  `internal/nexusbench`, which has its own kubectl runner and guard. `_test.go` files are not scanned.

**Guard G3, the destructive verbs (OC-06, DEVDESK-803, requirements S1 and S2).** Two halves.

- *Static*: `internal/engineguard/destructive_guard_test.go` (`TestDestructiveVerbsStayInTheirPackage`, a test of
  the whole module, reusing G2's helpers) parses every non-test `.go` file and fails, with file, line and
  enclosing function, on a site that builds the argv of a destructive subcommand (`kill`, `rm`, `stop`, `rmi`,
  `prune` alone; `container|image|volume|network|system|builder|pod|machine|compose|buildx|stack|service|secret|config|context|manifest|plugin`
  followed by `kill`, `rm`, `remove`, `stop`, `prune`, `reset`, `down`, `untag` or `disconnect`; kind's `delete cluster`) outside `internal/docker` and `internal/kind`. A
  site is a `[]string` literal that spells the verb at any position of its leading run of constant words (a global
  option such as `--context x` before it does not hide it), or a call to `exec.Command(Context)` or to an argv helper
  (`Build`, `mutate`, `mutateContext`, `prune`, `dockerOutput`, `dockerCombined`) with such a run of words.
  `TestNoExportedDockerFunctionTakesAnArgv` pins that no exported function of `internal/docker` takes an argv, so
  that "the verbs are built only in the package" is enough: other packages reach them through
  `docker.RemoveContainer(id)` and its siblings, which validate the ID.
- *Run time*: `internal/docker/destructive_ids_test.go` checks, under both engines and through the strict stub,
  that `StopContainer`, `RemoveContainer`, `RemoveImage`, `RemoveVolume` and `RemoveNetwork` refuse a malformed
  identifier before any argv is built (`TestDestructiveFunctionsRefuseAMalformedID`), return an error wrapping the
  sentinel and quoting the value (`%q`, so ESC or U+202E come out escaped; the text reaches the log, the handlers
  show only `Action failed — check logs` in the footer, nothing was added there), and pass a valid one as one argv element,
  last (`TestDestructiveFunctionsPassAValidIDAsOneArgvElement`). `TestEveryDestructiveFunctionIsCovered` fails on
  an exported function of the package that spells a destructive word and is in neither table. The `Prune*`
  functions take no identifier; their argv is pinned. `RestartContainer`, `PauseContainer` and
  `UnpauseContainer` validate the container ID the same way (`lifecycle_ids_test.go`).
- Formats (`internal/docker/identifier.go`), taken from what the callers pass, which is always a value read from
  `ps`, `image ls`, `volume ls` or `network ls`: container `IsContainerID` (`ErrInvalidContainerID`, lowercase
  hex, 12 to 64); image `IsImageID` (`ErrInvalidImageID`, the same hex with an optional `sha256:` prefix, never a
  `repo:tag`: `oci_resources` removes by `Image.ID`); volume `IsVolumeName` (`ErrInvalidVolumeName`,
  `[A-Za-z0-9][A-Za-z0-9_.-]*`, so an anonymous 64-hex volume passes); network `IsNetworkRef`
  (`ErrInvalidNetworkID`, a name of the volume alphabet or an ID: `Network.ID`, which is what the table passes).
  `DetectShell` and `ShellCmd` validate the container ID the same way (OC-18, DEVDESK-816). Known limit: a volume created by a third-party
  driver may carry a name outside that alphabet (`a/b`, `a:b`); `IsVolumeName` refuses it, so `RemoveVolume`
  cannot remove it from the TUI (the engine CLI still can).
- `internal/kind` does not exist and is exempt in advance. `TestAnExemptHomeHasItsRunTimeInvariant` fails, with a
  message that says what to add, as soon as it has a non-test file without a `TestEveryDestructiveFunctionIsCovered`
  of its own (the counterpart of the one in `internal/docker`).
- Limits, listed in the test header: an argv assembled word by word, a verb read from a variable, field or
  function result, a constant from another file, a shell line or script, a function value; the check is on words,
  so a `[]string{"rm", path}` for `/bin/rm` is reported too; restart, pause and unpause are not verbs of this
  guard (they destroy nothing); an option whose value is a variable ends the run of constants, so the verb after it
  is not seen, and neither is a verb behind an option placed between a group word and its verb (`compose -p x down`); a package home is exempt as a whole, file by file.

**Guard G8, every pull goes through `imagepull.Pull` (OC-10, DEVDESK-808, requirement S6).** Two halves.

- *Static*: `internal/engineguard/pull_guard_test.go` (`TestEveryPullGoesThroughImagepull`, reusing G2's and G3's
  helpers) reads every non-test `.go` file and fails, with file, line and function, on (a) a `[]string` literal, an
  `exec.Command(Context)` or an argv helper call (the list of G3) in which any constant word is `pull` (so
  `image pull`, `compose pull` and a verb after a global option or a variable are seen), or `--pull`, or
  `--pull=<value>` other than `never` and `false`; (b) a reference to `docker.PullImage` or `docker.PullImageContext`
  (called or taken as a value), or a call to them by bare name inside `internal/docker`. `imagepull` builds no
  argv: it is handed `docker.PullImageContext` as `Deps.Pull`, so the guard has no exempt package for (a), and
  `internal/imagepull` is the home of (b) only. Three named exceptions in `pullExceptions`, all permanent:
  `PullImageContext` (the one place that spells the verb), `PullImage` (its context-free wrapper, no production
  caller) and the `pullImageContext` test seam of `internal/ui/oci_resources/image_update.go`. Same rules as G2
  and G3: key = file + function, `Sites` exact, a reason, an exception whose code is gone fails, and
  `pullMustSee` fails the test if the detector stops seeing the two sites the module has today.
  `TestAPullAddedElsewhereFailsTheGuardOnTheRealModule` and `TestPullDetectorOnSources` / `TestPullJudgement` show
  it fails on a pull added anywhere else, from sources held in memory.
- *Run time*: a launch never pulls. `buildLaunchArgs` and `VerifyEntrypoint` pass `--pull=never`
  (`internal/docker/launch_pull_test.go`: exact argv on both engines through the strict stub; a missing image
  gives the engine's refusal and no second call). The two real paths to `imagepull.Pull` are `pullOneImageCmd` (`internal/ui/oci_resources/commands.go`), reached
  from the registry browser and from the MCP `image_pull_start` (`internal/app/mcp_actions.go` emits
  `ImagePullRequestedMsg`, `handleImagePullRequested` in `oci_resources/mcp.go` starts it; the `"G"` of
  `mcpActionKeys` is only the equivalent key), and `G` itself (`updateImageCmd`, `image_update.go`). The signature check
  of the Dockerfile remediation (`internal/ui/security`) only calls `imagepull.Check`. `TestNoPullBypassesTheSignatureCheck`
  (`internal/ui/oci_resources/pull_verify_test.go`) is a textual test that covers part of G8 on its own, for that one
  package. The launch form only opens from a row of the
  local Images table (`openLaunchForm`).
- Limits, listed in the test header: an argv built word by word, a verb from a variable, a shell line, a function
  value, a dot import of `internal/docker`; the check is on words (`[]string{"git", "pull"}` and the two-word
  `"--pull", "never"` would be reported, a false positive accepted; a launch spells `--pull=never`); an
  implicit pull by `run`/`create`/`build` without a flag is not a verb: the scanners' own `run --rm`
  (`container_run.go`) pulls DevDesk's tool images unverified (declared exception, above); with
  `scan.image_verification` off `imagepull.Pull` pulls the tag unchecked, by the user's choice; kind's pulls
  (OC-71) are out of scope, and so is `internal/nexusbench/skopeo.go`, which runs `skopeo copy` from registry to
  registry (a development tool). `--pull=never` was measured on Docker 29.7.2 and a Podman of 2026-09-25
  (exit 125 on a missing image), not on the N and N-1 versions of `docs/measurements/2026-11-27-engines.md`.

**Guard I1, no shell or Windows command interpreter is given a dynamic command line (OC-18, DEVDESK-816).** A value inside `sh -c "..."` is code to the interpreter (`x; rm -rf ~`, `$(...)`); the same value as one argv element stays data.
`internal/engineguard/interpreter_guard_test.go` (`TestNoInterpreterIsGivenAConstructedCommandLine`, reusing the helpers of G2, G3 and S9) reads every non-test `.go` file and fails, with file, line and function, on a word list (a `[]string` literal, the arguments of `append` after the first, of `exec.Command(Context)` from the binary on, of an argv helper of G3) that holds an interpreter and, after it, a flag followed by a word that is not a constant. Interpreters, compared without regard to case: `sh`, `bash`, `zsh`, `dash`, `ash`, `ksh`, `fish` and their `/bin` paths, `cmd`, `cmd.exe`, `powershell`, `powershell.exe`, `pwsh`, `pwsh.exe`. Flags per family, also without regard to case for the Windows ones: sh family `-c` and combinations (`-lc`, `-ic`); `cmd` `/c`, `/k`, `/r`; PowerShell `-Command`, `-c`, `-EncodedCommand`, `-enc`, `-e`, `-ec` (`bash -e` is not a command line). The register `interpreterExceptions` (key = file + function, `Sites` exact, a reason, fails when its code is gone) holds two entries: `internal/testutil/stepshell` `Run` (**permanent**, a test helper that runs a recorded remediation step through `sh -c` on purpose, imported by `_test.go` files only) and `internal/docker/launch.go` `VerifyEntrypoint` (**temporary**, DEVDESK-947: `sh -c "command -v "+entrypoint` with the entrypoint typed in the launch form; it runs in a 600 ms debounce while the user types, before the form is validated, in a throwaway `run --rm` container on the default network with no host mount; to move to an argv or a validation of the name). Limits: a constant script (`sh -c "true"`) is not reported; a binary or a flag held in a variable, a list built word by word, a script written to a file and an abbreviated PowerShell flag (`-comm`) are not seen; only the word right after the flag is judged, so `cmd /c start <url>` (`internal/browser`, DEVDESK-940) is not reported; programs that run a command line given as an argument without being a shell are outside the guard (`python -c`, `node -e`, `perl -e`, `wsl -e`, `ssh host <command>`, `su -c`, `env`, `xargs`); the guard does not follow the shell the exception runs in the container, only the host-side word list; a runner is not exempt.

**Guard S9, no secret in an argv (DEVDESK-945, requirement S9).** A command line is readable by every local user
(`ps`, `/proc/<pid>/cmdline`) and ends up in audit logs: a secret travels in the child's environment (`-e NAME` in argv,
`NAME=value` in `cmd.Env` or `containerRun.Env`), on stdin (`--password-stdin`) or in a file.
`internal/engineguard/secret_argv_guard_test.go` (`TestNoSecretIsPutInAnArgv`, reusing G2's and G3's helpers) reads every
non-test `.go` file and fails, with file, line and function, on a word list (a `[]string` literal, the arguments of
`append` after the first, of `exec.Command(Context)` after the binary, of an argv helper of G3) that has: `-e`, `--env`
or `-env` followed by a word with an `=` and a part that is not a constant (`"-e", name + "=" + token`, the plumber bug),
a call counting as such a word when the literal texts of its arguments hold an `=` (`fmt.Sprintf("%s=%s", n, v)`,
`strings.Join(..., "=")`); a word `--env=`, `--password=`, `--token=`, `--secret=`, `--api-key=`, `--creds=`,
`--src-creds=`, `--dest-creds=` or `--build-arg=K=` with a dynamic part; `--password`, `--token`, `--secret`, `--creds`,
`--src-creds`, `--dest-creds`... followed by a dynamic word; `--build-arg` followed by `K=<dynamic>`; `-u` followed by
`user:<dynamic>` (curl); a word holding `scheme://` then a dynamic part before the `@` (credentials in a URL); `-H`/`--header`
followed by a dynamic word naming Authorization, a token, a bearer, a cookie or an API key, or by a word with nothing constant
in it (a header held whole in a variable; the real module has none); `login ... -p <dynamic>`. The register
`secretExceptions` is empty: the one site it held, `plumberSafeDirectory`, spells its value as a literal now
(`GIT_CONFIG_VALUE_0=/scan`, kept equal to `containerScanPath` by `TestPlumberSafeDirectoryFollowsTheMount`), because the
guard cannot fold a constant of another file and reads it as dynamic. Same rules as G2, G3 and G8 if one is ever needed:
key = file + function, `Sites` exact, a reason, fails when its code is gone. The survey of the module (DEVDESK-945)
found no other site: `docker login` uses `--password-stdin`, cosign passes `-e NAME`, git carries its credential in
`GIT_CONFIG_*` of the environment, `git credential approve` reads stdin, the launch form passes `-e NAME` (DEVDESK-812),
skopeo uses an authfile. Limits, listed in the test header and still not seen: a bare variable after `-e`
(`"-e", kv`) and a call whose literal arguments hold no `=`; an argv assembled across statements; a flag held in a variable;
a constant of another file (reported, so it needs an exception or a literal); a shell line; a secret passed as a bare
positional argument; a `-u` whose readable text has no `:`; a URL built by a call whose literal arguments hold no
`scheme://`; the environment lists are not checked.

### Engine contracts — `requireEngine`, `internal/testutil/enginecontract` (OC-09a1, DEVDESK-806; OC-09a2, DEVDESK-943; OC-09a3, DEVDESK-944; OC-09b1, DEVDESK-807)

A contract test calls the functions of a package against a real docker or podman, outside CI, in the disposable VMs
of `tools/engine-vms`. The third story (OC-09a3) adds the recorder, described under "Recording" below; OC-09b1 adds the helpers and the
contracts of the scenarios ("The first scenarios" below) and OC-09b2 versions their fixtures: 40 recorded files, ten scenarios on four
configurations, and `Verify` now compares. The first story gives the gate, the entry point
every contract starts with, the guard that holds contracts to it, and the task that runs them; the second gives the
resources a contract creates (labelled, named), the argv rule every call goes through, the cleanup that removes only
what carries the label of the run, the mark of a test VM, and the sentinel. The contracts there are
`TestContractEngineAnswersItsVersion` (`engine_contract_test.go`, read-only), `TestContractRemovingARunningContainerIsRefused`
and `TestContractCleanupTouchesOnlyThisRun` (`containers_contract_test.go`), all in `internal/docker`; OC-09b1 adds the nine
scenarios listed under "The first scenarios".

- **Where a contract lives.** In the package it contracts, as a `*_contract_test.go` file of that package, so it can
  call the unexported runner and record the argv DevDesk really builds. The decision is the environment, not a
  build tag: a contract is compiled, vetted and linted on every CI run and skips itself in under a millisecond.
  `internal/app/view_contract_test.go` (a view conformance test, no engine) became `view_conformance_test.go` so
  the suffix means one thing.
- **The gate (`enginecontract.Decide`, a pure function of the environment).** The first rule that applies wins:
  1. `DEVDESK_ENGINE_CONTRACT` is not `1`: **skip**, with a message that says how to run the contracts. A plain
     `go test ./...` therefore skips every contract.
  2. `CI`, `GITHUB_ACTIONS`, `GITLAB_CI`, `BUILDKITE` or `TF_BUILD` is not empty (`engineguard.CIMarkers`, one list
     for `Decide`, `engineguard.ContractGate` and `contract.sh`): **refuse** (`t.Fatal`), even with the variable. A
     contract never runs in CI. A runner that sets none of these is not seen: this is defence in depth.
  3. `DEVDESK_ENGINE_CONTRACT_TOOL` is neither `docker` nor `podman`: refuse. No engine is assumed.
  4. `DEVDESK_FIXTURE_RECORDING=1`: the recording mode, which needs an absolute `DEVDESK_FIXTURE_OUT`, else refuse.
  5. Otherwise the verify mode.

  "A marker set and the variable absent" is a skip, not a refusal: the first rule wins. `contract.sh` applies the same
  two rules on the developer's machine before it reaches the VM host, and the tests of the child process and of
  the script (below) hold both.
- **`requireEngine(t)`** (`internal/docker/contract_support_test.go`) is the first statement of every contract. It
  calls `enginecontract.Require(t, probeEngine)` first: the gate, then `engineguard.AllowRealEngine(t)` (the same
  gate read again, by independent code, which also lets guard G1 pass this test's reaches for its length), the run
  id, the probe (the engine does not answer: fatal), the version check against `tools/engine-vms/versions.env`
  (recording: a version outside N and N-1 is fatal; verifying: a skip, no fixture can exist for it), then the banner
  `engine-contract: podman 6.2.1 rootless, run 20261127-a1b2c3d4, mode verify` on stderr (once per process) and in
  `t.Logf`. It then starts the contract (`startContract`): the engine of the run is pinned, the recorder of the
  contract becomes the `runner` of the package, and two cleanups are registered, in this order, "put the runner and
  the engine back" and then the cleanup of the run (`t.Cleanup` is last in, first out, so the cleanup of the run
  goes first, also when the test failed, and still has the recorder). The gate decides before the probe: the probe
  is never called with the gate closed. The probe goes through `cliRunner` (the recorder comes after it), so it
  needs no resource and no lab mark.
- **The probe** asks two questions with the engine's own templates (`probeArgs`): docker
  `version --format {{.Client.Version}} {{.Server.Version}}` (the two must be equal) and
  `info --format {{json .SecurityOptions}}` (`name=rootless` listed means rootless); podman
  `version --format {{.Client.Version}}` and `info --format {{.Host.Security.Rootless}}` (`true` or `false`). The
  parsing is two pure functions, `parseEngineVersion` and `parseEngineMode`, tested without an engine, and the
  argv are held by `TestProbeEngineAsksExactlyTheTwoQuestionsOfItsEngine`. **The templates are the plan's and are
  still to be confirmed on the test VMs**, for N and N-1 of both engines: only a real engine can say.
- **The run id and the names.** `devdesk.test=<run id>` is the label of every resource a contract creates and
  `ddc-<run id>-<role>` its name (`role` is 1 to 16 lower case letters or digits); `^[a-z0-9][a-z0-9-]{5,31}$` is the alphabet of a run id, which also keeps an
  empty id from making a label filter match every run. `contract.sh` generates it and passes it as
  `DEVDESK_ENGINE_CONTRACT_RUN`; without it one id is made per process.
- **`internal/testutil/enginecontract`** holds what needs no engine: the gate, the run id, `Label`,
  `ResourceName`, `CheckSupported`, `Banner`, `VersionsFile` and `Require`. It starts nothing and spells no engine
  argv (G2, G3, G7 and G8 have nothing to find there); `internal/kind` will use it too. The environment variables
  (`LabelKey`, `ToolEnv`, `RunEnv`, `ImageEnv`, `VersionsEnv`, `OutEnv`, `RecordingEnv`) are constants there; a
  test pins their values, because the script and these docs spell them.
- **Guard C1** (`TestOnlyTestsImportEngineContract`, `internal/testutil/enginecontract/import_guard_test.go`): only
  `_test.go` files import the package (it imports `testing`, `engineguard` and `enginefixture`). Read with
  `go/parser`, file and line reported, a floor on the files read. E1 (`TestOnlyTestsImportEngineFixture`) admits
  the package as the one non-test importer of `enginefixture` besides `fakecli`.
- **Guard G5** (`TestGuardContractTestsCallRequireEngine`, `internal/engineguard/contract_guard_test.go`, `go/parser`
  over the module, `testdata` left out):
  - R1: in a `*_contract_test.go` file, every `Test*` function starts with `requireEngine(t)` or `x := requireEngine(t)`
    (or `x = ...`), `t` being the identifier of its own `*testing.T`. A nested call, a method, another argument or
    a statement before it is reported.
  - R2: such a file declares nothing but `TestContract*` functions (helpers go to `contract_support_test.go`, and
    `-run '^TestContract'` selects every contract), no package-level `var` or `const` (evaluated at init, before
    `requireEngine`), and calls no `t.Parallel()`.
  - R3: a `TestContract*` function outside a contract file, or a call to `requireEngine(` or
    `engineguard.AllowRealEngine(` outside the contract files, `contract_support_test.go`, `internal/engineguard` and
    `internal/testutil/enginecontract/` (`Require` calls `AllowRealEngine`).
  - R4: a package with a contract file defines `requireEngine` in a `_test.go` file and its first statement calls
    `enginecontract.Require(`; an empty one fails.
  - A floor (at least one contract file), an exception table `contractExceptions` (empty, and an entry whose code
    is gone fails, as for G2, G3 and G8) and self-tests on in-memory sources.
- **What keeps the contracts out of CI.** `TestEveryContractIsSkippedOrRefusedOutsideAContractRun`
  (`internal/docker/contract_gate_test.go`) runs the test binary again, in a cleaned environment: without the
  variable every contract is `SKIP` with the message; with `CI=true` and the variable every contract fails with
  `refused` and no `engineguard:` panic text (the refusal came before any engine call).
  `TestNothingOpensTheContractGate` reads `mise.toml` and the workflows: `DEVDESK_ENGINE_CONTRACT` appears there
  only in a comment or a task `description`. `TestMiseEngineContractTasksStayOutOfTheStandardChecks`
  (`internal/nexusbench/mise_tasks_test.go`) holds the task out of `ci`, `check`, `test` and `test-race`.
- **The task.** `mise run engine-contract -- <docker|podman> <n|n1> [--mode rootful|rootless] [--no-reset]` runs
  `tools/engine-vms/contract.sh verify`. The task never sets the gate variable: the user sets
  `DEVDESK_ENGINE_CONTRACT=1` for the one command (`DEVDESK_ENGINE_CONTRACT=1 mise run engine-contract -- docker n`)
  and **does not export it**, because an exported variable opens the gate for every later `go test` of the shell. The
  script skips (exit 0) without it and refuses (exit 1) when `CI` or another marker is set, before any connection (`TestTheScriptAppliesTheGateBeforeAnyRemoteCall` reads the order); invalid
  arguments exit 2. Then it builds one `linux/amd64` test binary per package that has a contract (`-trimpath`),
  restores the VM unless `--no-reset`, copies the binaries and `versions.env` to `/tmp/ddc-<run>` in the VM, runs
  them with an explicit environment (`env -i`), as root for rootful or as the unprivileged user for rootless
  (podman defaults to rootless, docker is rootful only), and removes the directory afterwards. The directory is made
  with `mkdir -m 0755` and no `-p`, so an existing path (a planted link, another run) fails the run instead of being
  reused, and only a directory the script made is removed. Verifying copies nothing back. Since OC-09a2 it also pulls
  `CONTRACT_IMAGE` into the VM and starts the sentinel before the contracts, and checks it after them (below). The run counts the top-level
  `--- PASS`, `--- SKIP` and `--- FAIL` lines: when no contract passed (verify mode skips an engine outside N and
  N-1, and a contract with no fixture) the exit code stays 0 and it prints `nothing was done by verify` (`record` for a recording). It acts on the Incus host (`DD_ENGINE_VMS_HOST`).
- **The lab mark (OC-09a2, security decision M1).** Before a contract creates or removes the least resource
  (`c.container`, `c.volume`, `c.network`, `c.cleanupNow`; `c.requireLab()`, `Session.RequireLab`), the run must prove
  it is in a test VM, on top of the gate: `DEVDESK_ENGINE_CONTRACT_RUN` is set and is a valid run id (`contract.sh`
  sets it; without it `Require` makes up one id per process, which suits a read) **and** `/etc/dd/current` exists and
  holds exactly `n` or `n1` (`enginecontract.CheckLab`, a pure function of the environment and a file reader; the file
  is the one `tools/engine-vms/vm/dd-use.sh` writes in each VM, which `verify.sh` and `contract.sh` already read
  back, so no new file is needed). An empty file, another word or two lines is refused. A read-only contract
  (`TestContractEngineAnswersItsVersion`) asks for neither and still runs outside a VM, in verify mode.
  `SetLabMarkerForTest` swaps the path for one test (it panics outside a test binary). **Limit: root on a developer
  machine can create `/etc/dd/current`. The mark stops an accident (an exported variable and a `go test`), not an
  adversary.**
- **Resources.** `c.container(role, cmd...)` runs
  `run -d --pull=never --label devdesk.test=<run> --name ddc-<run>-<role> <image> sleep 600` (the image is
  `DEVDESK_ENGINE_CONTRACT_IMAGE`; empty fails the contract, a contract never pulls), `c.volume(role)` and
  `c.network(role)` run `volume create` and `network create` with the label and the name. Each returns what the
  engine printed (checked to be an identifier) and registers the id and the name in `ours`. `tried` is set before
  the engine answers: a creation that failed may still have made the resource.
- **The argv rule (`contractArgvRule`)** is an allow list the recorder (`recordingRunner`) asks before anything
  starts; a refusal is reported to the test (`Errorf`) and returned as `errNotContracted`, and nothing runs. It
  reads **options**, not only verbs (review findings M1, M2, B1, B2):
  - Reads are allowed on anything (`version`, `info`, `ps`, `inspect`, `logs`, `port`, `top`, `images`, `stats
    --no-stream`, and `ls` or `inspect` of `image`, `volume`, `network`), because the cleanup inspects candidates it
    does not own yet; their options are not read.
  - `run` and `create` (also under `container`): the options up to the image are checked against a list, and the
    words after the image (the container's command) are not read. Allowed: `-d`/`--detach`, `--rm`, `--name` (must
    start with `ddc-<run>-`, so a name of another run cannot be reused), `--label`/`-l` (one must be exactly
    `devdesk.test=<run>`), `--pull` (every one `never`, at least one), `-e`/`--env` with an explicit `KEY=value` (a
    bare name would pass the host's; the value must not be a secret, a contract has none), `--network` (`bridge`,
    `none`, `podman` or a network of the run: never `host` or `container:`), `-v`/`--volume` (`name:/path[:opts]`
    where name is a volume of the run: never a path, so no bind mount and no engine socket), `--entrypoint`,
    `-u`/`--user`, `-w`/`--workdir`, and (OC-09b1) `--health-cmd`, `--health-interval`, `--health-timeout` and
    `--health-retries`: they run a command inside the container and touch nothing of the host. Everything else is
    refused with a message that cites `contractArgvRule`, and
    these say why: `--privileged`, `--replace`, `--mount`, `--cap-add`, `--device`, `--security-opt`, `--pid`, `--ipc`,
    `--uts`, `--userns`, `--volumes-from`, `--env-file`, `-p` and `--publish` (a port is published only inside a launch
    window, "Windows" below). A run with no
    `--name` is allowed (the engine names it); one with a `--name` outside the prefix is not.
  - `volume create` and `network create`: `--label` and exactly one name starting with `ddc-<run>-`; no `--opt`
    (a device option binds a host path), no driver. **`pod create` is refused** (label or not): no cleanup of pods
    exists, so one would stay in the store.
  - `rm`, `kill`, `stop`, `start`, `restart`, `pause`, `unpause`, `volume rm`, `network rm`, `network connect` and
    `disconnect` (and, OC-09b1, podman's `healthcheck run`) may name only resources of this run, by exact reference (`ours`; connect and disconnect check the
    container, the last name), and take **only the options of the verb**: `rm` `-f`/`--force`, `-v`/`--volumes`,
    `-t`/`--time`; `stop` and `kill` `-t`/`--time` and `-s`/`--signal`; `restart` `-t`; `volume rm`, `network rm`,
    `start`, `pause`, `unpause`, `connect`, `disconnect` and `healthcheck run` none. So `--depend`, `--all`, `--latest`, `--filter`,
    `--cidfile`, `--ignore` and a `-f` on a network or a volume are refused.
  - Refused as a whole: every `prune` (the functions of DevDesk have no label filter, so a prune touches the whole
    store), `system`, `pull`, `push`, `load`, `import`, `build`, `commit`,
    `tag`, `login`, `logout`, `exec`, `cp`, `save`, `export` and `rmi`. The three windows below are the only
    exceptions, each for one argv.
  - `--url`, `--connection`, `--remote`, `--root`, `--runroot`, `--storage-driver`, `--host`, `-H` and `--context` are
    refused **at any position** of the argv (the command after an image included), and an argv that starts with an
    option is an unknown verb. A verb the rule does not know and an empty argv are refused with a message that names
    `contractArgvRule`: a verb is added there with its reason.

  The recorder also refuses a credential helper, standard input, a context and an interactive command. Everything it
  runs goes through `cliRunner.Build`, the one path to the engine (G1); the journal (`exchangeLog`) keeps argv, exit
  code, stdout and stderr apart. Limits: with `Combined` the bytes are stdout then stderr, not the terminal's
  interleaving (the refusals measured write on stderr only); the options of a read are not checked; an environment
  value is not tested for being a secret.
- **The engine environment and the image (review findings B3 and B4).** `Require` refuses, after the gate and before
  the probe, an environment in which `DOCKER_HOST`, `DOCKER_CONTEXT`, `CONTAINER_HOST` or `CONTAINER_CONNECTION` is
  set (`enginecontract.CheckEngineEnv`, `EngineEnvVars`): the engine commands of a contract must reach the engine of
  the test VM, and the VM's contracts run under `env -i` anyway. **Not covered:** the current context in
  `~/.docker/config.json` (`currentContext`), podman's system connections in `containers.conf`, `DOCKER_CONFIG`. The
  image is checked twice: `contract.sh` requires exactly `docker.io/library/alpine@sha256:<64 hex>` in
  `CONTRACT_IMAGE` (it is the value it pulls and the only image the contracts start), and the Go side
  (`enginecontract.ValidImage`, where `DEVDESK_ENGINE_CONTRACT_IMAGE` is read) accepts any `repository@sha256:<64 hex>`,
  a digest and never a tag, because a contract may need another image one day and the digest is what matters.
- **The cleanup** (`cleanupNow`, registered as `cleanup`, which does nothing unless a creation was tried) removes
  what carries the label of the run, and only that, in this order: containers, networks, volumes. For each kind it
  lists with the filter of the engine (`ps -a -q --no-trunc --filter label=devdesk.test=<run>`, `network ls ...`,
  `volume ls -q ...`), then **inspects every candidate and reads its label again** (`{{index .Config.Labels
  "devdesk.test"}}` for a container, `{{index .Labels "devdesk.test"}}` for a network or a volume): only a label
  equal to the run id is kept. A candidate with another label or none is reported (`Errorf`) and never touched.
  Removal goes through `RemoveContainer(id, true)`, `RemoveNetwork` and `RemoveVolume`, which validate the identifier
  (G3), and the references removed are returned. When at least one verified candidate existed it lists again, and a
  leftover is reported. A listing or an inspect that fails removes nothing, is reported, and the cleanup goes on with
  the other kinds. An invalid run id (an empty one would make `label=devdesk.test=` match every run) makes it refuse
  to list. Without an engine, `TestCleanupNeverTouchesWhatDoesNotCarryTheLabelOfTheRun` simulates a filter that
  lets a stranger's and an unlabelled resource through and fails on any removal that names them. A process killed
  before its cleanup leaves labelled resources in the VM; `reset.sh` (the default of `contract.sh`) clears them.
- **The sentinel and `CONTRACT_IMAGE`.** The real negative control runs on the VM. `contract.sh`, after the reset,
  pulls `CONTRACT_IMAGE` (`tools/engine-vms/versions.env`: `docker.io/library/alpine@sha256:<digest of the multi-arch
  index>`, with its date and source in the comment, trust on first use like the other sums) as the user of the mode
  (`vm_as_mode`: `vm_user` for rootless, `vm_root` otherwise, so the sentinel lives in the store the contracts
  use); this is the only pull anywhere, and it is the script's, not Go code (G8). It then starts a container named
  `ddc-sentinel-<run>` **with no label**, passes `DEVDESK_ENGINE_CONTRACT_RUN` and `DEVDESK_ENGINE_CONTRACT_IMAGE` to the
  contracts, and after them (whatever their result: nothing between can leave the script) inspects the sentinel. If it
  is gone the run fails with "the cleanup touched a container without the label"; if the engine does not answer it
  fails with another message ("did not answer after the contracts"), which does not blame the cleanup. If the creation
  prints no id the script removes the container by its name before it gives up. It also gives the contracts the id
  of the sentinel in `DEVDESK_ENGINE_CONTRACT_SENTINEL` (`enginecontract.SentinelEnv`, in the shared `vars`, only when
  it is 64 hexadecimal characters, else the script stops with `die` and its trap removes the sentinel; "The sentinel in a
  fixture" below). It is removed by its id, and
  nothing else the script removes in the engine. The tests in `script_sentinel_test.go` read the script's text for the order and
  the shape of those steps, and `TestVersionsEnvPinsTheContractImageByDigest` the pin; that the sentinel survives with each engine is verified only on the Incus host.
- **Windows (OC-09b1, DEVDESK-807).** Some scenarios need an argv `contractArgvRule` refuses: a `prune`, a launch DevDesk
  builds (it carries no label and, for a port, publishes one), an `rmi`. A **window** is how a contract sends one, and
  it is narrow on every side: it names the one argv it grants, grants it **once** (an argv it refuses does not use the
  attempt), exists for the length of one call (`withWindow` puts the standard rule back when the call returns, also
  when the call ended the contract), and everything else the call does is judged by the standard rule as before. The
  window is a second judge in front of the standard one: it is never a loosening of `contractArgvRule`, and a refusal
  names both reasons. Three windows exist, each a pure function of its inputs (tested without an engine) and a method of `contract`:
  - **Prune** (`pruneWindowRule(kind)`, `c.expectPrune(campaign, scenario, text, kind, call)`). `kind` is `container`,
    `network` or `volume`, and the one argv is `<kind> prune -f`, what `PruneContainers`, `PruneNetworks` and
    `PruneVolumes` send: never `-a` or `--all`, never a filter, never `system prune` or an image prune, no other kind.
    The condition: **the whole store of that kind holds nothing but resources of this run**, read right before the call
    (`ps -a -q --no-trunc`, `network ls --format {{.Name}}` with the networks the engines make, `volume ls -q`). A
    foreign resource (named in the message), a listing that fails, a kind with no window or a missing lab mark ends the
    contract before the engine is asked to prune. A lab VM after a reset holds the sentinel container, so the container
    prune never passes there: the scenario that uses `expectPrune` today is the volume one.
  - **Launch** (`launchWindowRule`, `launchWindow{Name, Port}`, `c.expectRefusal(campaign, scenario, text, w, call)`). One
    `run` that names exactly `w.Name` (a name of this run), the pinned image, `--pull=never`, optionally `-p w.Port` (one
    fixed loopback port, `127.0.0.1:<port>:<port>`; `0.0.0.0`, no address, an IPv6 one or an engine-picked one are refused,
    and a window is declared whole before anything runs) and that the standard rule would grant once the label of the
    run is added to the argv it judges: that is the only thing added, and the argv is rebuilt from the parsed options, so
    a word read as a value is never read as an option by the rule. `expectRefusal` expects the engine to refuse (an
    exit code of 0 is reported) and afterwards looks at what carries the name of the window, in a call of its own
    (`inspect --type container --format '{{.Id}} {{index .Config.Labels "devdesk.test"}}' <name>`): docker creates the
    container of a port it cannot bind, then fails (**measured on docker 29.10.1 and 28.5.2: the container exists, exit 125;
    podman 6.2.1 and 5.8.11 make none, exit 126**). The name is the window's, so no other launch can carry it. A container that
    docker made has no label, and the template prints `<no value>` for it (podman may print an empty string): the line is cut at its
    first space (an id has none), the rest is the label, and `<no value>` or nothing means "no label" (`labelValue`; the cleanup reads
    its label the same way and says "has no label"). One with **no label** is removed by its full id (`RemoveContainer`), one
    of this run is the cleanup's, one with another run's label is reported and left alone. The look also happens when the
    call ends the contract. `c.portHolder(role)` creates its container through the same window rule (labelled, with
    `-p 127.0.0.1:18080:80`, `sleep 600`).
  - **rmi** (`rmiWindowRule(imageID)`, `c.expectRmiRefusal(campaign, scenario, text, imageID, call)`). The one argv
    `rmi <id>` of an image id of 64 hexadecimal characters that `usedImage` returned: never `-f`, a tag or a second image.
    The use of the image is read again right before the call (`ps -a -q --no-trunc --filter ancestor=<id>`) and must
    list a container this run made; otherwise the contract ends before any `rmi`, since an `rmi` that succeeded would remove the
    image every later contract starts (it is reported when that happens anyway).
- **Helpers (OC-09b1)**, all in `contract_support_test.go`, all labelled and named like `c.container` and all asking for the lab mark:
  `c.healthContainer(role)` (`--health-cmd true --health-interval 1s --health-timeout 2s --health-retries 1`),
  `c.waitHealthy(id)` (20 reads of `{{.State.Health.Status}}` a second apart through the `sleep` seam of `contract`; on podman
  each round first runs `healthcheck run <id>`, because the health state of a container of the lab VM does not move by
  itself, measured 2026-11-27; `unhealthy`, a failed read and 20 reads without `healthy` end the contract naming what was
  read), `c.portHolder(role)` (port **18080** on `127.0.0.1`, fixed so a fixture does not change between recordings; a
  VM with something else on it, or a machine that is not a lab, fails the contract naming the port),
  `c.usedVolume(volumeRole, containerRole)`, `c.usedNetwork(networkRole, containerRole)` (the container joins the network by
  name), `c.usedImage(role)` (a container and the id of its image, read with `image inspect --format {{.Id}}`, the algorithm
  stripped) and `c.public(ids...)` (the ids a fixture may keep, `Redaction.Public`: 64 lower-case hexadecimal characters, never
  an id of a resource of this run; `usedImage` declares the id of the image it read). `c.exchangeWith(cmp, ...)` is
  `c.exchange` with a Comparer for verifying.
- **Comparers (OC-09b1).** `enginecontract.Tolerating(kinds ...Tolerance)` judges two exchanges equal when their streams are
  equal once every value of the tolerated kinds is replaced by one placeholder on both sides: `Durations` (`Up 3 seconds`,
  `Created Less than a second ago`, `Up About an hour`: a number and a unit, `Less than a second`, `About a/an <unit>`, nothing
  abbreviated), `Timestamps` (`2026-11-27 12:00:00 +0000 UTC`, with a fraction, a zone name or a monotonic reading, and
  RFC 3339: the CreatedAt of `ps` and `network ls`) and `Health` (`(healthy)`, `(unhealthy)`, `(starting)`,
  `(health: starting)`). Nothing else is tolerated (the number in `Exited (137)`, a port, a name, the word `ago`, a status
  with a marker against one without). **`Health` makes `(healthy)` equal to `(unhealthy)`: never use it when the health state is the subject
  of the contract** (`ps-health` tolerates durations and timestamps only, and asserts `(healthy)` itself); `compare_test.go` pins that
  any marker equals any other, so the kind is documented, not narrowed to `starting`. The argv and the exit code are still compared exactly first; an unknown kind panics when
  the Comparer is made. `SameUpToDurations()` is `Tolerating(Durations)` and `SameUpToDurationsAndHealth()` is
  `Tolerating(Durations, Health)`. **`ContainerIDs`** (OC-09b2, `SameUpToContainerIDs()`) tolerates the id of a container right after `container ` or `used by`
  (12 characters on docker, 64 on podman): the refusal of an `rmi` names **one** of the containers that use the image, and the engine takes the first it
  finds in a table with no order, the sentinel or the container of the run, so a second recording or a verification may name the other (`images/rmi-used`
  uses it). Narrow: the image id in the same message, `running` against `stopped`, the other words and an id after any other phrase still count.
  The comparers spell no argv.
- **The sentinel in a fixture (OC-09b1).** The sentinel container of `contract.sh` is in the store while the contracts run, so a
  `ps` they record lists it: its id (a new one at every run) is the variable `DEVDESK_ENGINE_CONTRACT_SENTINEL`, and
  `c.redaction()` maps it to the role `sentinel` (full id and the 12 characters `ps` prints), so two recordings give the same
  fixture; its name `ddc-sentinel-<run>` loses the run id like every name. A value that is not 64 lower-case hexadecimal characters
  ends the contract (the script sets it only for such an id), and **a recording without it ends the contract too** (verifying does not
  need it): a recorded list would hold a container whose id nobody redacted. Nothing else foreign is expected in a list of a freshly reset VM: a
  container, a volume or a network of its own would appear in the fixture and make it vary, which a recording on the VMs shows.
- **The first scenarios (OC-09b1).** Nine contracts, one scenario each, next to the existing `containers/rm-running`; their fixtures are
  versioned since OC-09b2 (`internal/enginefixture/fixtures/<campaign>/<scenario>--<tool>-<version>-<mode>.cli`, `fixtures/README.md` lists them):
  `containers/ps-health` (`TestContractPsReportsTheHealthOfAContainer`: `ListContainers(true)` after
  `healthContainer` and `waitHealthy`, compared with durations and timestamps tolerated, not the health), `containers/inspect-absent` (`TestContractInspectOfARemovedContainerIsRefused`: a container removed, then inspected;
  the inspect of a container that is there is out of scope: its id is unknown and its values volatile), `images/rmi-used`
  (`TestContractRemovingAnImageInUseIsRefused`), `networks/rm-used` (`TestContractRemovingANetworkInUseIsRefused`), `volumes/rm-used`
  (`TestContractRemovingAVolumeInUseIsRefused`), `volumes/ls` (`TestContractListingVolumesShowsTheVolumesOfTheRun`),
  `volumes/prune-unused` (`TestContractVolumePruneOfAStoreOnlyThisRuns`: docker 23 and later prunes anonymous volumes only, so
  the named volume stays and its cleanup removes it; the contract logs which of the two happened), `launch/name-taken`
  (`TestContractLaunchWithATakenNameIsRefused`: exit 125 on both engines, `already in use`) and `launch/port-taken`
  (`TestContractLaunchOnATakenPortIsRefused`: exit 125 or 126, `already allocated`, `already in use` or `Address in use`).
  The exit code and the words of the other refusals are logged where the two engines differ, and the fixture holds them.
  The files are `containers_`, `images_`, `networks_`, `volumes_` and `launch_contract_test.go`; the helpers and the tests of the helpers
  (`contract_window_internal_test.go`, `contract_prune_internal_test.go`, `contract_creation_internal_test.go`,
  `contract_sentinel_internal_test.go`, `compare_test.go`, `compare_tolerating_test.go`) run with a `fakecli` engine and no real one.
- **A recorded list holds this run and the sentinel (OC-09b1 hardening).** `c.exchangeListing(kind, cmp, ...)` is `exchangeWith` for a
  call whose output lists a whole store (`ps-health`, `volumes/ls`): first it reads the store as a prune does (`ps -a -q --no-trunc`,
  `network ls`, `volume ls -q`). Recording ends the contract before anything is recorded when the read fails or the store holds
  anything that is neither of this run nor the sentinel (a container a killed run left behind must not enter a fixture); verifying only
  logs it. `findContainer` never matches a row with an empty id (an empty string is a prefix of every id).
  `volumes/prune-unused` requires that the report and the state agree (`volumePruneAgrees`: a report that names the volume means it is gone,
  one that does not means it is listed); the outcome differs by engine and version: podman 5.8.11 removes the unused named volume,
  podman 6.2.1 and docker 28 and 29 do not.
- **A label that reads `<no value>` (limit).** The templates that read a label (`{{index .Config.Labels "devdesk.test"}}`) print `<no value>`
  for a missing one on docker, so `labelValue` reads `<no value>` as no label, and a label whose value is literally `<no value>` is read
  the same way. The ambiguity is kept: a template that tells the two apart (`{{json .Config.Labels}}`) changes the argv the cleanup and the
  stray look pin (`resourceKinds`, `strayInspectArgv`), and the effect is small: the label of this run is a run id, and the container a
  window removes must also carry the exact name of that window, which no other launch can have.
- **Limits of OC-09b1, and what only the VMs say.** `expectRefusal` does not handle `-e NAME`: the launch DevDesk builds for an
  environment variable passes the name only, which the standard rule refuses (a bare name would pass the host's), so a scenario
  with an environment is not a window. A process killed while a launch window is open can leave a container with **no label**
  that the cleanup by label does not see (`reset.sh`, the default of `contract.sh`, does). The recorder does not pass an
  environment to the engine (`dockerCmd.Env` is ignored). **To confirm on the four engines:** the wording of the
  refusals (the assertions are loose on purpose); that `inspect` of a missing container says "no such" (`removeStray` and
  `inspect-absent` rely on it); the health state of podman rootful and rootless with `healthcheck run`; that docker with
  the containerd image store gives `image inspect --format {{.Id}}` an id the `ancestor` filter knows (the contract ends
  with a message that says so when it does not); that `ps` prints the CreatedAt in the forms the comparer knows; and that the
  stderr of docker's port refusal holds endpoint ids that `Redact` is not told about: it does (two words of 64 hexadecimal characters
  on docker 29.10.1, the dry-run of OC-09b2), and `c.anonymous` is the answer ("Anonymous ids" below).
- **Contracts on the developer's own machine.** A read-only contract can run there, in verify mode, with the
  variable and the tool set by hand (not exported): `go test -run '^TestContractEngineAnswers' ./internal/docker`. One
  that creates or removes a resource is refused without the lab mark (above): outside a test VM it fails with a
  message that points to `mise run engine-contract`.
- **Recording (OC-09a3, DEVDESK-944).** `DEVDESK_ENGINE_CONTRACT=1 mise run engine-contract:record -- <docker|podman>
  <n|n1> [--mode rootful|rootless] [--keep-out <dir>]` runs `contract.sh record`, which does what `verify` does and
  more. The log line names the action (`... mode record`). `--no-reset` is **refused** (exit 2, the message says why): the
  fixtures describe what a clean VM answers, so a recording always restores the clean snapshot. `--keep-out <dir>`
  (absolute, record only) sends the result there instead of the repository: the dry run. Like the gate, these checks come
  before any connection.
  **A recording needs the external deny list** (security finding M1): `DEVDESK_FIXTURE_DENYLIST` must name a file with at least one
  active term (domains, project and customer names, one per line, outside the repository). `contract.sh record` refuses (exit 1,
  `record needs a deny list: set DEVDESK_FIXTURE_DENYLIST to a file of terms ...`) when it is unset, unreadable or without a term,
  after the arguments and before anything is built or sent to the VM, and imports with `--require-denylist`. Verifying needs none.
  - *In the VM.* `contract.sh` creates `/tmp/ddc-<run>/out` (`mkdir -m 0700`, then the `chown` of the work directory, so a
    rootless recorder owns it) and adds `DEVDESK_FIXTURE_RECORDING=1` and `DEVDESK_FIXTURE_OUT=/tmp/ddc-<run>/out` to the
    explicit environment of the test binary, **only for a record and only there**: they are not in the shared `vars`, in
    `mise.toml` or in any workflow, and the script never reads them from the host (`TestTheScriptSetsTheRecordingVariablesOnlyForARecordInTheVM`).
    `Decide` turns them into mode Record, which needs an absolute `OutDir`; `Require` refuses a version outside N and N-1
    in this mode (verifying skips).
  - *`c.exchange` and `Session.Record`.* `c.exchange(campaign, scenario, text, call)` still demands exactly one exchange in
    its window and returns it raw. In a recording session it also builds the `Redaction` (the ids of `c.ours` of 64 hex
    characters, each with its role, the run id, `os.UserHomeDir`, `user.Current` and `os.Hostname`) and calls
    `Session.Record`, else `Session.Verify`. `Record` needs Mode Record, an absolute `OutDir` **and the lab mark**
    (`RequireLab`: a developer who set the variables by hand on his machine writes nothing), checks the campaign and the
    scenario (`^[a-z0-9][a-z0-9-]*$`, no `--`), redacts, refuses an id nobody announced (`UnknownEngineIDs`), formats, and runs `CheckLeaksAs` with `MachineIdentities()`: a
    leak ends the test naming the rules and lines, **never the text**, and nothing is written. The file is
    `<out>/<campaign>/<scenario>--<tool>-<version>-<mode>.cli` (`recorded` is today in UTC, `recorded-by:
    engine-contract:record`), created with `O_EXCL` and never through a link: an output directory or a campaign directory
    that is a link, and a file already there (two contracts for one scenario), end the test.
  - *Verifying.* `Session.Verify(t, campaign, scenario, raw, redaction, cmp)` loads the embedded campaign, takes the
    fixture of the scenario for the engine (tool, version, mode; a skipped one is not selected), redacts the exchange and
    compares: argv and exit code exactly, the streams through the `Comparer` (`func(recorded, got Exchange) error`, nil =
    exact; `enginecontract.Tolerating` and the two named Comparers give one to the scenarios that carry a duration, a
    timestamp or a health state, "Comparers" below). A difference is an `Errorf` (the contract goes on)
    with the lengths and the first line that differs; a line is cited **only if it passes `CheckLeaksAs`** (the exchange is
    redacted, not checked), else the report says it is not shown and names the rules (security finding B3). **When the campaign
    has fixtures for this engine (tool, version, mode) but none for the scenario, `Verify` fails (`Fatal`): a misspelled or
    renamed scenario would compare nothing. With no fixture at all for the engine, `Verify` is a failure since OC-09b2 (`enginecontract.RequireFixtures` is
    `true`, raised in the same change as `minVersionedFixtures = 40`; `TestVerifyFailsHardOnceFixturesAreVersioned` reads that floor from
    `stale_test.go` and fails if the floor is above 0 and the constant is not)**: it said `no recorded fixture for ... record it with mise run
    engine-contract:record` and returned before, because a `t.Skip` ends the test (`runtime.Goexit`) and the assertions a contract makes after
    `c.exchange` would never run on a checkout without fixtures. **A green run proves its comparisons** (OC-09b2): each time `Verify` compares it logs `compared <campaign>/<scenario> against <fixture file name>`
    (also when the comparison then reports a difference), and `no recorded fixture ...` when it compared nothing; `contract.sh` counts the first in each test
    binary's output (`-test.v`), prints `N scenarios compared` in its summary line and, in verify mode, **warns** (it does not fail) when contracts passed and
    none was compared. Negative controls with no engine, on the versioned fixtures: one byte of stdout changed fails `Verify`, and a scenario nobody recorded
    fails it (`verify_compared_test.go`). `Session.MissingFixturesOK` is the one relaxation, set by `labSession` (the tests of the helpers) and by nothing
    else: `TestOnlyTheSessionOfTheHelperTestsRelaxesTheFixtures` fails if another file of `internal/docker` names it, and `Require` never sets it. It exists for the tests of the helpers
    of `internal/docker`, which run scenarios nobody recorded on a fake engine; `VerifyAgainstNotRequiring` holds the old branch in the tests. `VerifyAgainst(t, fixtures, ...)` is the
    seam the tests use. `VersionsFile` defaults to `enginefixture.VersionsSourceFile()`.
  - *Back on the workstation.* Only when the contracts and the sentinel check passed (and at least one contract
    passed), the script packs **the one directory** `/tmp/ddc-<run>/out` (`vm_root ... tar -C ... -cf -`, capped at
    16 MiB; nothing else leaves the VM: no `scp`, `rsync` or `file pull`). **The extraction is strict, because the tar
    is untrusted input** (`unpack_recorded` in `tools/engine-vms/unpack_recorded.sh`, which `contract.sh` sources and
    `script_unpack_test.go` runs on booby-trapped archives): it is first listed twice (`tar -tf`, `tar -tvf`) and refused as a
    whole when the archive is over 16 MiB, when an entry is not `./`, `<campaign>/` or `<campaign>/<name>.cli` (so no absolute
    path, no `..`, no odd name), when an entry is not a regular file or a directory (a symbolic or hard link, a device or a
    FIFO), or when the sizes the listing announces add up to more than 16 MiB (security finding B4: a sparse member is a few
    kilobytes in the archive and gigabytes extracted); it is then extracted into a new
    `mktemp -d` under the temporary directory of the script, with `--no-same-owner` and `--no-same-permissions` (never
    `-P`), and `chmod go-rwx`. That directory goes to `go run ./internal/enginefixture/cmd/engine-fixture-import --from
    <it> [--into <keep-out>]`, which parses and checks everything again (below), after the sentinel check and only on
    success. The script writes nothing under `internal/` itself. A failed import fails the run. Limits: the checks of
    the listing are about shape, `Import` is the one that reads the content; the listing was exercised with GNU tar only.
- **Only verifiable on the Incus host (OC-09a3).** A real recording with each of the four engines (the ids, home, host
  and user that really appear, that `Redact` then `CheckLeaksAs` is clean on them), the tar of a rootless output
  directory, `contract.sh record` end to end, and the import of that batch with `--keep-out`.
- **Only verifiable on the Incus host.** The probe templates above on the four versions; `contract.sh` end to end
  (cross-compilation, the copy, `vm_user` rootless, the cleanup trap); the real behavior of the pilot contract; the
  label filter and the `inspect` label templates of the cleanup on the four versions; that the sentinel survives a
  real cleanup; the pull of `CONTRACT_IMAGE` into the VM. None of this ran in CI or in the session that wrote it.
- **Limits.** R2 refuses any package-level `var` or `const` in a contract file (its initializer would run in every test binary
  before `requireEngine`, e.g. `var _ = exec.Command(...).Run()`); a `var` that calls `requireEngine` elsewhere is an
  R3 site. G5 misses a function value (`re := requireEngine`), an aliased import of
  `engineguard`, and a contract in a file not named `*_contract_test.go` that calls neither `requireEngine` nor
  `AllowRealEngine` (G1 makes it panic at run time instead). `TestGateOpenersOnSources` documents what the
  `mise.toml` and workflow guard cannot see: a name assembled at run time, and a script a task or a workflow calls
  (`contract.sh` passes the variable to the VM on purpose and applies the gate itself). The gate is the environment:
  whoever exports the variable and unsets every CI marker opens it. `mise run test-race` and `ci` never set it.
  What resources add to the limits: the lab mark can be forged by root, and the argv rule and the cleanup rest on
  the engine's own label filter and `inspect` template, which are to be confirmed on N and N-1 of both engines (a
  template an engine rejects fails the contract, or the cleanup reports and removes nothing). Also:
  - **What the code under test creates through the recorder is not cleaned and not gated.** Only `c.container`,
    `c.volume` and `c.network` ask for the lab mark and register what they make. A creation the function under
    contract makes (for example `LaunchContainer`) goes through the recorder and its rule, so it must carry the
    label to be allowed at all, but it is neither registered nor cleaned unless the cleanup's label filter finds it.
  - **The sentinel is one container.** There is no sentinel network or volume: the negative control of the
    cleanup on the VM covers containers only. Networks and volumes are covered without an engine, by the stub test.
  - **inspect, then rm.** The cleanup removes a volume by name after reading its label; in between, another
    process could replace a volume of that name. A test VM has no other process; a developer machine has the lab
    mark as its only protection.
  - The call of `contract.sh` is a script: its text is tested, its behavior is verified on the Incus host only.

### Recorded fixtures — `internal/enginefixture` (OC-08a and OC-08b, DEVDESK-805, DEVDESK-942)

What an engine answered to one argv, kept as a text file and replayed byte for byte. `internal/enginefixture`
is production code that starts nothing and never imports `testing` (`TestProductionCodeDoesNotImportTesting`);
the helpers that serve a fixture are `_test.go` files of `internal/docker` and `internal/testutil/fakecli`.
**Since OC-09b2 `fixtures/` holds 40 real recordings** (user decision of OC-08, Q1 (A): none before): the ten scenarios of the contracts of
`internal/docker` (`containers/{ps-health,rm-running,inspect-absent}`, `images/rmi-used`, `networks/rm-used`,
`volumes/{ls,rm-used,prune-unused}`, `launch/{name-taken,port-taken}`) on docker 29.10.1 and 28.5.2 rootful and podman 6.2.1 and 5.8.11
rootless, `recorded-by: engine-contract:record`, read by hand before they were committed. The mechanisms are still tested on the synthetic
files of `internal/enginefixture/testdata/synthetic/*.cli`, each marked `recorded-by: synthetic`. **A fixture is a recording, never edited by
hand:** record again with `DEVDESK_ENGINE_CONTRACT=1 mise run engine-contract:record -- <docker|podman> <n|n1> [--mode ...]` (a deny list is
required), review the diff, commit. What differs by engine is in the fixtures, on purpose: the wording and the exit codes of the refusals
(docker 1 and 125, podman 2, 125 and 126), the order of the rows of a `ps` (docker lists the newest first, podman the oldest), the image
column, the volume prune (podman 5.8.11 removes the unused named volume and reports it; podman 6.2.1 and docker 28 and 29 keep it) and the
mount point of a volume. The recorded values that move are fixed (timestamps, durations) or tolerated by the Comparer of the contract; the
sentinel row of `ps-health` depends on the test VM, not on the run.

- **File.** One exchange per file, `<campaign>/<scenario>--<tool>-<version>-<mode>.cli`, for example
  `rm-running--podman-6.2.1-rootless.cli`; `Parse` checks that the name agrees with the headers. Headers
  `# key: value` (one space after the colon, no space at either end, no control character) in **one canonical
  order**: `tool` (`docker`, `podman`), `version` (`^\d+(\.\d+)+(-[0-9A-Za-z.]+)?$`), `mode` (`rootful`, `rootless`),
  `recorded` (`YYYY-MM-DD`), `scenario`, `redacted` (what the recorder replaced, or `none`), `record` (optional:
  `replay`, `pending`, `skip: <reason>`), `recorded-by` (optional: `synthetic`, `engine-contract:record`), `argv`,
  `exit` (0 to 255), `stdout`, `stderr`. A different order, an unknown or a duplicate header is refused. `argv` is
  one line of compact JSON without HTML escapes (DEL and the C1 controls are written `\u00xx`, so the header holds no raw control character), without the binary name, not empty, valid UTF-8, no element with CR
  or LF. `stdout` and `stderr` read `<n> bytes, text|base64`, `n` being the **decoded** length.
- **Body.** A blank line, then `--- stdout\n`, the `n` bytes of stdout, one extra `\n`, `--- stderr\n`, the `m` bytes
  of stderr, one extra `\n`. **The length is what delimits a text stream**: a `--- stderr` line, a blank line or a
  missing final newline inside a stream is data. An empty stream is an empty line. The extra `\n` is not part of the
  stream, so stdout `[]\n` shows a blank line after `[]`. A mismatch is an error that gives the declared and the
  found length. A base64 stream is cut at 76 columns and delimited by its terminator (the alphabet has no `-`).
- **One writing only.** `Format(Parse(b)) == b` for every file `Parse` accepts (`FuzzParse`; `go test` runs the seeds
  only, explore with `go test -fuzz=FuzzParse -fuzztime=60s ./internal/enginefixture`). LF only (a CR anywhere is
  refused: `.gitattributes` cannot change a byte). `Format` writes **text** for UTF-8 without a control character
  other than LF and TAB, and base64 otherwise; `Parse` refuses a `base64` stream that could have been `text`, a
  `text` stream holding a control character (ESC, NUL, BEL, DEL or a C1 control would hide text from a reviewer or act
  on a terminal; CR is one), and a non canonical base64. A `pending` file (the recorder writes it before it has run
  anything) stops after `argv` and the blank line; it carries no `exit`, `stdout`, `stderr` or body. `LoadFS` and
  `Load` refuse it with an error that names the file and says `pending`; `NewReplay` does too.
- **No normalization at replay**: identifiers, timestamps, durations (`Up 5 seconds`) and the order of lines stay as
  recorded. A test that reads a duration compares it to a clock it fixes. stdin is never recorded and the replay
  ignores `dockerCmd.Stdin`.
- **Loading.** `Load(campaign)` (embedded) and `LoadFS(fsys, campaign)` read `<campaign>/*.cli`, sorted by name, ignoring
  other files and sub directories; an unknown campaign is an error that names it. `Campaigns()` lists the embedded
  campaigns that hold a `.cli` file, sorted; `All()` loads them. `ForConfig(fixtures, tool, version, mode)` keeps one
  configuration (whole version: `29.10` is not `29.10.1`) and **leaves a skipped fixture out**, without touching its input.
- **Replay.** `NewReplay(fixtures...)` refuses no fixture, a pending one, a skipped one (`record: skip: <reason>`, below) and
  two with the same key; the key is the
  **tool and the argv, exact, element by element**, in order (no flag is reordered); version and mode are not in it,
  so a test **chooses** its configuration (`ForConfig`) and two fixtures that differ only by them collide, and the error
  names both. The same argv under `docker` and `podman` are two keys. `Answer(tool, argv)` returns a **copy**
  (a caller's edit does not reach the next answer); `Unused()` lists, sorted, the fixtures never answered. Safe for
  concurrent use. A sequence of states (`ps` before and after) is two `Replay`s.
- **`fixtureRunner`** (`internal/docker/fixture_runner_test.go`, a `dockerRunner`, so a test file of that package;
  `useFixtures(t, fixtures...)`). Immediate and in memory: tier T1. It looks up the fixture of `engine.Current().Name`
  and `dc.Args`; with none it fails the test with `no fixture for <tool> <argv %q>` and returns `errUnexpectedArgv`;
  at cleanup it fails for each fixture nothing asked for (the rigour of `stubStrict`), and restores the runner and the
  engine. `dc.Helper` is refused (the credential helpers are not replayed). A `dc.Ctx` already done returns
  `ctx.Err()`, like `exec.Cmd.Start`. Exit 0: stdout (then stderr when `Combined`), nil error. Non-zero: a
  `*fixtureExit` (`Error()` is `exit status N`, `ExitCode()`), with `Stderr` unless `Combined`, as `CombinedOutput`
  leaves `ExitError.Stderr` empty. `errors.As(err, &*exec.ExitError)` works (`InspectContainer` reads the engine's own
  diagnostic that way) and yields an `ExitError` holding `Stderr`, **but its `ProcessState` is nil**: its `ExitCode()` is
  `-1` and its `Error()` is `<nil>`. **Read the exit code through `interface{ ExitCode() int }`**, which both a real
  `*exec.ExitError` and `*fixtureExit` satisfy (the rule for OC-24 and later). `Build` checks the argv with the same
  rigour and returns a command to a path that does not exist, so running it reaches no engine; a test that must run
  the result goes through `fakecli`.
- **`fakecli.Fixtures(t, fixtures...) Source`** (`internal/testutil/fakecli/fixtures.go`). One exact exchange per fixture
  (`Prefix` false, `Tool` the fixture's `# tool:` header, never `engine.Shape.Binary`, so guard G2 has nothing to
  see) with the recorded bytes and exit code. A selection the replay cannot serve fails the test at once
  (`Fatalf`, naming the fixtures). The strictness of `Install` applies: an argv no fixture holds fails the test
  at its end with the tool and the argv; an unasked fixture is not a failure of `fakecli` (a source may be shared).
- **Equivalence** (`internal/docker/fixture_equivalence_test.go`): each synthetic fixture goes through `fixtureRunner`
  and through the real `cliRunner` against `fakecli.Fixtures`, with `Combined` false and true, and the bytes, the error
  text, the exit code (by the interface) and `ExitError.Stderr` must be equal; `InspectContainer` and `ListContainers`
  give the same result both ways. The `ps` fixture holds the argv DevDesk builds from `engine.Templates.PS`: if the
  template changes, the fixture goes false, on purpose, and the test says so. The view (`internal/ui/containers`,
  `fixture_smoke_test.go`) shows the rows of that fixture through `fakecli.Install(t, fakecli.Fixtures(t, f), "docker")`.
- **Guard E1** (`TestOnlyTestsImportEngineFixture`, `internal/enginefixture/import_guard_test.go`): a non-test `.go` file
  outside `internal/enginefixture` and `internal/testutil/fakecli` that imports `internal/enginefixture` fails the
  suite with file and line (`go/parser`, imports only; floor of 200 files read; `TestE1DetectorOnSources` holds the
  negative controls). The fixtures therefore never reach the `dk` binary (`go list -deps .` does not list the package).
  G1, G2 and G3 need no exception: the package has no `exec.Command`, no read of `Shape.Binary` and no argv literal, the
  argv living in `.cli` files and in `_test.go` files, which G3 does not read. Limits: an import path built at run time,
  a `go:embed` of the directory from another package, a copy of the files.
- **Limits**: with `Combined` the replay writes stdout then stderr, as `fakecli` does; a real engine may interleave them.
  Go truncates a real `ExitError.Stderr` above 64 KiB (head and tail), the replay does not. stdin is ignored. A
  non-UTF-8 argv is refused by `Parse`.
- **`record: skip: <reason>`** (OC-08b). A skipped fixture is a recorded exchange kept in the tree that no test replays
  (an answer that depends on the clock, the machine or an ordering). `Parse`, `Format`, `Load`, `LoadFS` and `All` keep it,
  so the leak and stale checks read it like any other; `ForConfig` leaves it out; `NewReplay` refuses it with an error that
  names the fixture and says it is skipped, so `fakecli.Fixtures` refuses it too. A test that hands one over gets a failure,
  not a silent miss.
- **The leak check** (OC-08b, `internal/enginefixture/leaks.go`). A fixture is a text file read in review and kept in the git
  history for good, so nothing of the recording machine may be in it. `CheckLeaks(name, data, exact) []Leak` and
  `CheckLeaksAs(name, data, exact, local)` return `Leak{Fixture, Line, Rule}`: **never the text found**, and each rule once
  per line. A valid file is read by its parts (the headers, the `argv` included, so a `pending` file is checked; each stream
  **decoded**, so base64 hides nothing; a leak in a base64 stream is reported at the line of its `# stdout:` or `# stderr:`
  header); an invalid file is read as raw text, line by line (a broken file is no way round). Rules:

  | Rule | Refuses | Admitted |
  |---|---|---|
  | `secret-token-format`, `jwt-like-value` | the formats of `fixtureleak` (GitHub and GitLab tokens, AWS key ids, a PEM private key; `eyJ` and 10 base64url characters) | |
  | `host-not-allowed` | the authority of an `http(s)` URL outside `AllowedHosts` (`127.0.0.1`, `localhost`, `index.docker.io`, `registry-1.docker.io`, `docker.io`, `quay.io`), or with `user@` | |
  | `private-or-link-local-address` | a private, link-local, CGNAT or unique-local address | `EngineBridgeNets`: `172.17.0.0/16` to `172.31.0.0/16` (the Docker pools) and `10.88.0.0/16`, `10.89.0.0/16` (Podman). `192.168.0.0/16`, `172.16.0.0/16` and a `10.x` lab bridge are refused |
  | `public-address` | a public IPv4 address or a global IPv6 address (`2000::/3`), in any header, stream or decoded base64 stream (OC-09a3, security finding M2; `enginefixture` only, `fixtureleak` and Nexus are unchanged) | loopback, `0.0.0.0/8`, multicast, `240.0.0.0/4` (netmasks, broadcast), the documentation ranges (`192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`, `2001:db8::/32`), `EngineBridgeNets`, and what the private rule owns. Not an address: a dotted quad followed by `.<digit>` (OID, five-field version), preceded by `.`, `-`, `v` or the word `version`, an octet above 255. Heuristic, erring toward reporting: `1.2.3.4` alone is an address |
  | `internal-name` | a name in `.home.arpa`, `.lan`, `.internal`, `.local` | `AllowedInternalNames` (exported, name or `*.suffix` to a reason): `host.docker.internal`, `host.containers.internal`, `*.svc.cluster.local` (kind). `x.host.docker.internal` and the bare `svc.cluster.local` are refused |
  | `personal-path` | `/home/<x>`, `/var/home/<x>`, `/Users/<x>`, `/mnt/<drive>/Users/<x>` (WSL), `/root`, `C:\Users\<x>` (raw or JSON-escaped), at the start of a line, after `file://`, or after a blank or one of `" ' = : , \| ( [`; JSON-escaped slashes (`\/home\/x`) are read as slashes | `<x>` is `fixture-user` (`PlaceholderUser`) or a key of `ImageHomes` (a home an image keeps, such as `/home/node`; empty at first, each entry with its reason) |
  | `lab-identity` | `LabTerms`: the names already public in `tools/engine-vms/README.md` (the lab hosts and the lab user). The short term `dd` (under three characters) is matched at **strict boundaries** only: start or end of the line, a blank, a quote, `/ : @ , ; ( ) [ ] = \ \| < >`, never `.` or `-`, so `dd.conf` and `yyyy-MM-dd` pass | |
  | `local-identity` | the host name and the user name of the machine that runs the test, **handed over by the test** (`MachineIdentities`, filtered by `LocalIdentityTerms`: three characters at least, not `root`, `user`, `admin`, `runner`, `ubuntu`, `docker`, `podman`, `ci`, `build`, `test`, `localhost`, `debian`, `fedora`, `alpine`, `node`, not `fixture-*`) | |
  | `exact-term` | each term of the external deny list (`DEVDESK_FIXTURE_DENYLIST`, one term per line, never in the repository) | |
  | `sensitive-env-value` | `NAME=value` where `secretenv.Default().Sensitive(NAME)` holds; a quoted value is a value (`X_PASSWORD="x"`, `export DB_PASSWORD='x'`) | an empty value (`NAME=`, `NAME=""`, `NAME=''`), `<redacted>` (quoted or not), and a name of `AllowedSensitiveEnvNames` (exported, name to a reason): `GPG_KEY`, the public key id of the python image |
  | `invisible-character` | U+200B to U+200F, U+202A to U+202E, U+2060, U+2066 to U+2069, U+FEFF: valid text that a reviewer cannot see (the stream encoding lets it through). The line is also read without them, so a split name is seen | |

  The name rules (`lab-identity`, `local-identity`, `exact-term`) match **at the word, ignoring the case, also
  JSON-escaped and URL-escaped** (`fixtureleak.ExactVariants`): a boundary is any character that is not a letter, a digit or
  an underscore, so `-`, `/`, `@` and **`.`** are boundaries and `zedbox` is seen in `zedbox.corp.example`, in
  `srv.zedbox.example` and before the dot that ends a sentence, but not in `zedboxes` or `ddm-run` (the one exception is the short lab term `dd`, above). `CheckLeaks` is
  `CheckLeaksAs` without local identities: the test adds them, the check never reads the machine itself.
  The detectors without state (formats, URL authorities, addresses, internal names, exact variants, the deny list reader)
  are **`internal/fixtureleak`**, shared with `internal/nexusfixture` (the same code, moved unchanged; `TestDetectorsAreDeclaredOnce`
  fails if either fixture package declares them again); the HTTP automaton of Nexus and the CLI format stay apart.
  `TestFixturesLeakNothing` runs it on every `.cli` file of the tree (`fixtures/*/*.cli` and `testdata/*/*.cli`, pending files
  included) with the deny list and the machine identities, and logs how many terms it applied (a missing, empty or
  comment-only deny list says so: it never passes for an applied list).
  **Limits** (known false negatives and positives; none is hidden by a test):
  - *False negatives.* A host name that is arbitrary, outside every internal zone, the lab and the local machine, is seen only
    through the external deny list. A fully qualified host name whose **short name** is not in `MachineIdentities` is not seen:
    `MachineIdentities` keeps the host name as `os.Hostname` returns it, so when that is an FQDN its first label is not a term
    of its own. A local network in `172.17.x` passes (it is an engine network). A home written in a form the rule does not list
    (a path outside `/home`, `/var/home`, `/Users`, `/mnt/<drive>/Users`, `/root`, `C:\Users`, after a prefix that is not in the
    list) is not seen. A name that is not at a word boundary (`xzedbox`) is not seen. A leak hidden by an encoding other than
    base64 (an escape such as `\u0064\u0064` inside text) is not seen. A URL to `host.docker.internal` is refused by
    `host-not-allowed` (the exception list is for names, not for URL hosts).
  - *False positives.* The short lab term `dd` still matches the command `dd if=...` (a blank is a strict boundary) and `dd` in a
    path or after `=`. `sensitive-env-value` is as blunt as `secretenv` (`KEY` in `KEYBOARD=...`), and a variable of an image that
    holds a public value under a sensitive name needs an entry in `AllowedSensitiveEnvNames` (`GPG_KEY` has one). A home an image
    keeps (`/home/node`) needs an entry in `ImageHomes`. A name an engine writes itself in an internal zone needs an entry in
    `AllowedInternalNames`.
  - *Exception lists.* `ImageHomes`, `AllowedInternalNames` and `AllowedSensitiveEnvNames` are exported variables, empty or short
    at first, **kept by hand**: each entry carries its reason, and an entry is a decision a reviewer can disagree with.
- **Versions: N and N-1** (OC-08b, `internal/enginefixture/versions.go`). `tools/engine-vms/versions.env` is the single source
  (the VMs of OC-85 and the measure of OC-01 read it too); `ParseVersions` reads `DOCKER_N_VERSION`, `DOCKER_N1_VERSION`,
  `PODMAN_N_VERSION`, `PODMAN_N1_VERSION` (plain `NAME=value`, `#` comments including at the end of a line, other keys ignored;
  a missing key, an empty value or a value that is not a dotted version is an error that names the key) into a
  `Supported` (`map[Tool][]string`, N first). Currently Docker 29.10.1 and 28.5.2, Podman 6.2.1 and 5.8.11: the numbers in
  this file are copied by hand, and a bump edits `versions.env`, `versions_test.go` and this section in the same change.
  `TestFixturesAreNotStale` (`stale_test.go`, the comparison lives in the test file as in Nexus) fails when a versioned fixture
  was recorded from an engine that is neither N nor N-1 of its tool, naming each file, its version, the supported ones and the
  way out (record again with `mise run engine-contract:record`); a pending fixture has no
  answer to be stale, a skipped one is checked. **`minVersionedFixtures = 40`** since OC-09b2 (a deleted fixture fails; a new scenario or configuration raises it in the change that records it), the same function runs on `testdata/synthetic`, and `TestSyntheticFixturesCoverBothVersionsOfBothTools`
  keeps N and N-1 of both tools exercised. The floor never goes down. A Go list of the supported
  versions for the run time (OC-83) will add its own test against `versions.env`.
- **`record: pending` and the recording session** (OC-08b, `pending_test.go`). A pending file is the recorder's request without an
  answer: `Load` refuses it, so a campaign that holds one is out of the replay, stale and round-trip checks while it stays.
  `TestNoPendingFixtureIsVersioned` fails on one under `fixtures/` and names it, unless `DEVDESK_FIXTURE_RECORDING=1` and `CI` is
  empty (a local recording session, with a note). `DEVDESK_FIXTURE_RECORDING=1` with `CI` set is **an error even without a pending
  file** (the verdict is a pure function of `getenv` and the file list, `recordingVerdict`); only the value `1` opens a session.
  `TestRecordingSessionIsNeverSetByTheStandardChecks` reads `mise.toml` and `.github/workflows/*.y*ml`: none names the variable
  outside a recorder task (`engine-contract:record`, `nexus-bench:record`), so no standard check can turn the guard off. The leak
  check still reads the pending files.
- **`Redact`** (OC-09a3, `redact.go`, no `testing`, starts nothing). `Redact(ex Exchange, r Redaction) (Exchange, string)` turns what
  a real engine answered into what a fixture may hold; the string is the `redacted` header (the categories that replaced
  something, `home, host, ids, run, user` sorted, or `none`). It works on bytes (argv, stdout, stderr, a stream that is not UTF-8
  included), does not edit its input and leaves the exit code alone. `Redaction{RunID, IDs (real 64-hex id -> role), Home, User,
  Host}`; `PlaceholderID(role)` is `hex(sha256("devdesk-fixture/"+role))`, 64 characters, so the validators of `internal/docker`
  accept it. Order: the full ids; their 12-character prefix (as `ps` prints it) as a whole hexadecimal word only; the home, as a path
  prefix (at the start, after a blank or a character that opens a value, or `file://`; followed by the end, `/` or a character that
  cannot continue a name: `/var/lib/root` and `/rootfs` are not `/root`); the run id (`PlaceholderRun`, `fixture-run`); the host
  (a whole word, any case, a dot or a dash being a boundary as in `local-identity`; a host written as a fully qualified name also
  loses an internal suffix `.home.arpa`, `.lan`, `.local` or `.internal`, so `fixture-host.lan` never stays for `internal-name` to
  refuse, and the first label of a qualified host is replaced on its own); the user (a whole word, any case). The host and the user are replaced
  **only when `LocalIdentityTerms` keeps them**: `root` and the lab user `dd` (two characters) stay, and `CheckLeaks`
  (`lab-identity`) refuses them, which makes the recording fail instead of leaking: that is the net. Durations and timestamps are
  not normalized (decision of OC-08). `Redact(Redact(x)) == Redact(x)`.
- **`UnknownEngineIDs`** (OC-09a3, security finding B2, `unknownid.go`). `Redact` leaves a 64-hex word it was not told about alone (it may
  be a public digest), so the recorder asks `UnknownEngineIDs(redacted, redaction)` and ends the recording under the rule
  `unknown-engine-id` (`RuleUnknownEngineID`, naming the count, never the id) when a whole word of exactly 64 hexadecimal characters
  is neither the placeholder of a role of `Redaction.IDs` nor in `Redaction.Public` (the ids a contract declares public: an image id;
  the digest of the pinned image, `DEVDESK_ENGINE_CONTRACT_IMAGE`, is added by `Session.Record`). It is not one of the rules of
  `CheckLeaksAs`, which cannot tell a digest from the id of a stranger's container. Chosen over a replacement by a placeholder
  because it keeps `Redact` simple and idempotent and fails where a human decides; the cost is that a scenario that prints an image
  id declares it in `Redaction.Public`.
- **Anonymous ids** (OC-09b2, DEVDESK-807, `anonymous.go`). Docker's refusal of a published port names the network endpoint it set up for the
  container it made and could not start (two words of 64 hexadecimal characters on docker 29.10.1, new at every run), and `network rm` of a
  network in use names the endpoint of the container, `id:"<12 hex>"`. No resource of the run owns such an id, so `unknown-engine-id` refused
  the recording of `launch/port-taken` on docker, and a fixture of `networks/rm-used` would have changed at every recording. A scenario that
  has such a message declares **how many it accepts**: `c.anonymous(n)` (1 to 8; a contract helper, in `contract_support_test.go`) sets
  `Redaction.Anonymous`, the most **distinct** unknown ids the scenario accepts to replace. With it above 0 `Redact` replaces each
  hexadecimal word of 64 or of 12 characters that is not known (a role of `IDs`, a `Public` id, the placeholders) by `PlaceholderID("anon-1")`,
  `("anon-2")`, ... in the order they appear (argv, stdout, stderr), the same word by the same placeholder, in any case, a word of 12 by the first
  12 characters of the placeholder, and a short word that starts a long one met before (or after) shares its placeholder. A word **beyond the number
  is left as it is**, and `UnknownEngineIDs` counts it (it also counts unknown words of 12 when `Anonymous` is above 0, and never otherwise), so the
  recording is refused as before: the limit is never widened silently. The category in the `redacted` header stays `ids`. `Session.Record` and
  `Session.Verify` give `Redact` the same `Public` as `UnknownEngineIDs` (with the digest of the pinned image), so the digest in an argv is not
  anonymized. Declared where the message names such an id: `launch/port-taken` (`c.anonymous(2)`) and `networks/rm-used` (`c.anonymous(1)`).
  Read and not declared: `volumes/rm-used` (docker names the container, which the run owns), `images/rmi-used` (the short id of the image is the
  prefix of a public id, the container's is the run's), `containers/rm-running`, `launch/name-taken` (the container of the run). **Limit:** a
  scenario that declares a number accepts any unknown word of 12 or 64 characters up to it, including one that is not an id (a 12-digit number);
  keep the number as low as what was observed, and the other docker and podman versions are confirmed only by a recording.
- **Fixed timestamps and durations** (OC-09b2, DEVDESK-807, `normalize.go`). The CreatedAt column of a `ps` holds the date of the recording to the
  nanosecond and its status how long ago the container started, so a second recording would write another file. A scenario that declares it
  with `c.normalize(enginecontract.Durations, enginecontract.Timestamps)` (`ps-health` today; `volumes/ls` prints no clock, and `network ls` is not
  recorded) gets `Redaction.FixedTimestamps` / `FixedDurations` **in a recording only**: in stdout and stderr, never in the argv, every timestamp
  becomes `2026-01-01 00:00:00 +0000 UTC` (`2026-01-01T00:00:00Z` for an RFC 3339 one) and every duration `5 seconds` (`Up 3 minutes`, `Up Less
  than a second` and `Created About an hour ago` give `Up 5 seconds` and `Created 5 seconds ago`), and the `redacted:` header names `durations` and
  `timestamps`. Nothing is fixed unless declared, and `Health` is judged, never fixed (`normalize` refuses it). The patterns are
  `enginefixture.TimestampPattern` and `DurationPattern`, the ones `Tolerating` reads, so verifying the fixed fixture against a real output of another
  day passes with `Tolerating(Durations, Timestamps)`, which the contract passes to `exchangeListing`; that comparer still sees a state, a health
  marker and a name. **The sentinel row of the `ps` is accepted:** its id and name are already redacted to the role `sentinel`, so the fixture of
  `containers/ps-health` depends on the lab (a container with no label next to the one of the run), not on the run.
- **`volumes/prune-unused` records what each engine does (OC-09b2).** The volume prune differs by engine and version, and the fixture is the truth of each
  configuration: podman 5.8.11 removes the unused named volume and reports it on stdout, podman 6.2.1 reports nothing and keeps it (empty stdout), docker 28 and
  29 keep it (`Total reclaimed space: 0B`). The contract requires only that the report and the state agree (`volumePruneAgrees`).
- **`Import`** (OC-09a3, `importer.go`). `Import(ImportOptions{From, Into, Supported, Deny, Local}) (ImportReport, error)` is what
  the workstation runs on the directory the recorder wrote, before anything reaches `fixtures/`; it trusts nothing the VM did and
  is **all or nothing**. `From` holds only regular files `<campaign>/<name>.cli` (campaign `^[a-z0-9-]+$`): a link (also as a
  campaign), any other file (also at the root), a sub directory, an empty batch is an error. Each file is parsed again and says
  `recorded-by: engine-contract:record` (`RecordedByContract`; empty, `synthetic` are refused) with no `record` header (pending,
  skip, replay are refused). **The whole batch is one configuration** (tool, version, mode), whose version is one of `Supported`
  (N and N-1), and `CheckLeaksAs(Deny, Local)` finds nothing. An error names files and rules, never the text found; **a file that does not parse is reported as
  `<campaign>/<file>: invalid format (<rule>)`**, the rule being `ParseError.Rule` (the message template, without its values), never a
  header value, since the file came out of a VM; the file that is opened is checked to be regular (`fstat`). **All or nothing,
  precisely:** every check (read, parse, one configuration, N and N-1, leaks, the existing target) happens before the first write and
  is all or nothing. The writing is in two phases (temporary files next to the targets, then one rename each) and the rename phase is
  **not atomic for the batch**: if the second rename fails, the first fixture stays installed (an I/O failure, reported). Chosen over a
  rollback because the files installed are valid and checked, and the next run reports them as `Unchanged`. `Unchanged`, `Kept` and
  `NotProduced` compare with `Into`: **with `--keep-out` that is the keep-out directory, not the repository**, so a first dry run
  reports everything as written. On any error
  the target is not touched (not a file, a directory or a temporary file). An existing file of `Into` that parses not, or is not a
  regular file, is an error too. Then: identical to the existing file but for `recorded` is `Unchanged` (not rewritten: bytes and
  modification time stay); an existing `record: skip: ...` is `Kept` (never overwritten); anything else is `Written`, in two
  phases (temporary files next to the targets, then renames). The files of `Into` of the same configuration that the batch did
  not produce are `NotProduced`: listed, **never deleted** (a skipped one is the human's and not listed). `SourceDir()` and
  `VersionsSourceFile()` locate the fixtures directory and `tools/engine-vms/versions.env` through `runtime.Caller`; under
  `-trimpath` they answer an empty path and an error that names the source tree.
- **`engine-fixture-import`** (OC-09a3, `internal/enginefixture/cmd/engine-fixture-import`). A production `main` under the
  directory E1 already admits (it starts no process: no `os/exec`, `testing`, `enginecontract`, `fakecli`, `docker`, `engine` or
  `engineguard`, `TestTheCommandStartsNothingAndImportsNoEngineCode`). `engine-fixture-import --from <dir> [--into <dir>]
  [--versions <file>] [--require-denylist]` (defaults: the source tree's `fixtures/` and `versions.env`) applies `MachineIdentities()`
  and the external deny list of `DEVDESK_FIXTURE_DENYLIST` (an unreadable file is an error that names the variable).
  **`--require-denylist`** (what `contract.sh record` passes; security finding M1) refuses the import, exit 1, when the variable is
  unset or names a file with no active term: the names of an instance (domains, projects, customers) are known to nobody but you, and
  without them a fixture could carry one. Without the option, a missing list is only a note on stdout. Exit 0 done, 1 refused or failed
  (nothing written, nothing on stdout, `engine-fixture-import: <error>` on stderr), 2 usage. The first line of stdout is
  `engine-contract:record: <tool> <version> <mode>: N written, N unchanged, N kept, N not produced by this run`, then one line per
  fixture of each list and the state of the deny list. `contract.sh record` runs it (above).
- **Recording a new version** (the procedure of a bump): edit `versions.env`, `versions_test.go` and this file (the numbers above),
  then run `mise run engine-contract:record` for the four configurations (Docker N and N-1, Podman N and N-1, plus Podman rootful),
  review the diff (the format is meant to be read), and commit. Nothing is deleted by a recording: remove a fixture of an old
  version by hand (`TestFixturesAreNotStale` says which).
- Not here: the scenarios of the other zones (kind, scan); a version bump records the four configurations again.

## OCI registries — `internal/oci`

`internal/oci/oci.go` — an OCI **registry** HTTP client: list tags,
download and extract a tar.gz (a repository template is one — `templates.md`). It speaks the distribution spec over `net/http`
and never shells out, so it is engine-agnostic and podman changed nothing in it.

**Typed status errors and context variants (DEVDESK-298, IS-18).** A registry,
token endpoint or registry manager's management API answering with a
non-success status returns `*oci.StatusError{Code, Source, Challenge}`; read it
with `errors.As`. `Source` is `SourceRegistry` (the v2 API), `SourceToken` (the
Bearer token endpoint) or `SourceManagementAPI` (a manager's own REST API, today
Nexus `/service/rest`, DEVDESK-509). `Challenge` is the `Www-Authenticate`
header of a 401 whose challenge is not Bearer. `Error()` keeps the three
historical messages to the character (`registry returned 404`,
`token endpoint returned 403`, `unexpected auth challenge: ...`), so nothing on
screen or in a log changed for them. The management API message is
`management API returned N`: the code only, never the status line, the body, a
header or the URL. It replaces the Nexus client's former `HTTP N`.
Failures that are not a status (unreadable body, missing realm, no token
field) stay plain errors and do not match `errors.As`.

`oci.ManifestDigest` (the `HEAD` on a manifest) is typed too (DEVDESK-631): a
non-200 answer returns `fmt.Errorf("%w for %s:%s", &StatusError{Code, Source:
SourceRegistry}, repo, tag)`, so `errors.As` finds the `*StatusError` and the
text stays `registry returned 404 for repo:tag` to the byte. A 200 without a
`Docker-Content-Digest` header stays a plain error (`registry gave no digest for
repo:tag`). Its callers (`imageupdate`, the signature stage of `scan`) only log
the error and mark the reference failed; none reads the text.

A 404 whose body says the proxy's upstream is blocked is marked (DEVDESK-632):
`registryStatusError(resp)` (`internal/oci/status_body.go`) builds the
`*StatusError` of every non-200 registry answer (tags, catalog, `ManifestDigest`)
and, for a 404 only, reads at most 4 KiB of the body (`maxStatusBody`, a
hypothesis like `maxNexusErrorBody`). When the body is JSON and the `message` of
the first `errors[]` entry starts with `Remote Auto Blocked` (exact, case
sensitive, no leading space), `UpstreamBlocked()` is true. A missing, empty,
plain-text, longer or cut body, another message, another status (502,
403) leaves it false. A body read that ends on a deadline (the client's 15 s
delay, the caller's context) is no answer: `registryStatusError` returns the
`*NoAnswerError` (verdict `failed`, `no answer within N s`) instead of a plain
404; any other read failure (a truncated body, an unexpected EOF) keeps the plain,
unmarked 404. `ManifestDigest` is a `HEAD`, which has no body: a "Remote Auto
Blocked" 404 on a manifest can never be marked `UpstreamBlocked()` and stays a
plain 404 (`refused`). The body never reaches the error: `Error()` is `registry
returned 404: the proxy's upstream is temporarily blocked, try again later`, with
no date and no duration. `imagesearch.Classify` ranks it `failed` (temporary),
not `refused`; the tags footer says `Could not load tags: the proxy's upstream
is temporarily blocked` (`registryFailureReason`) and the journal line carries
`temporarily blocked`. Every other 404 stays `refused` with `registry returned
404`.

The unexported `unexpectedChallenge` field is a deviation from the plan, which
had only `Code`, `Source` and `Challenge`: with those three, a 401 with a
non-Bearer challenge and a 401 returned by the registry after a token exchange
both read `Source == SourceRegistry`, `Code == 401`, and `Error()` could not
give each its own message. The flag tells them apart without widening the
exported surface.

`ListRegistryTagsContext` and the `...Context` variants of `registryGET`,
`registryDo`, `doRequest` and `exchangeBearerToken` take a `context.Context`
(the token endpoint request included) and check `ctx.Err()` at the head of
every page. The old names delegate with `context.Background()`, signatures
unchanged. A deadline or a cancel reaches the caller as an error that
satisfies `errors.Is(err, context.DeadlineExceeded)` or `context.Canceled`,
and never as a `StatusError`.

**The abandon after 15 s is typed (DEVDESK-633).** `oci.RequestTimeout` (15 s,
unchanged) is the delay of `registryHTTPClient` and the value of
`imageSearchDeadline`. A registry that does not answer in time ends the request
with a `*oci.NoAnswerError`, whose text is exactly `no answer within N s`, N read
from the constant (no `15` literal outside the tests). It wraps only
`context.DeadlineExceeded` (`Unwrap`), so `errors.Is` still finds a deadline,
while the URL, userinfo, host, `/v2/` path and query never come back in the
message. It is applied to the errors of `doRequestContext`, of the token
endpoint's `Do` and `readToken`, and of the body reads of `registryGETContext` and
`registryGETBoundedContext` (so tags, catalog, catalog under a prefix, the
manifest digest and the token exchange are covered). `context.Canceled` is not
touched, and a 502 received in time stays a `*StatusError{502}`, `failed`, with no
"no answer". `registryFailureReason` returns the error's own text for it (a constant
by construction), so the footer (Error) says `Image search on <alias>: no answer within 15 s` and `Could not load
tags: no answer within 15 s`, and the journal reads `ERROR [oci_resources] image
search on <alias>: no answer within 15 s`. **Limit:** the client's own delay and the
context's deadline are still one case: both give the same error and the same text,
and `imagesearch.Classify` (DEVDESK-303, IS-12) puts it in the `failed` verdict.
Only a delay that satisfies `errors.Is(err, context.DeadlineExceeded)` becomes a
`NoAnswerError`: a transport-level "TLS handshake timeout" keeps its own text. The
Docker Hub client of `internal/imagesearch/hub.go` is typed too since DEVDESK-667:
see "Why a failure says it at the footer" below.

**Why a failure says it at the footer (DEVDESK-667, S-04b).** The Docker Hub search
(`HubSearcher`) uses `oci.RequestTimeout` as its client delay and wraps a deadline with
`oci.NoAnswer` (the exported form of the internal `noAnswer` conversion, one rule for
both), so its failure is a `*oci.NoAnswerError` like the catalog's, with no URL or
`q=` in the text; any other transport error keeps its `*url.Error`, so `errors.As`
still finds the `*net.OpError` inside. `registryFailureReason(err, access)`
(`internal/ui/oci_resources/registry_failure.go`) turns an error into one constant
sentence, tested in this order, and returns `""` for anything else (so a 500, a
503, a 401/403/404/405 or an unknown error changes nothing):

| Error | Reason |
|---|---|
| `context.Canceled` | none (the user left) |
| `oci.ErrTokenRealmOffOrigin` | `token realm on another host` |
| `*oci.StatusError`, upstream blocked | `the proxy's upstream is temporarily blocked` |
| `*oci.StatusError` 429, registry or token endpoint | by access, below |
| `*oci.NoAnswerError` | `no answer within N s` |
| `*net.OpError` with `Op == "proxyconnect"` | `could not reach the proxy` |
| `*net.DNSError` | `could not resolve the registry host` |
| any other `*net.OpError` | `could not reach the registry` |

The no-answer reading is tested before the net errors: a connection that timed out is
both. The 429 wording follows `registryAccess`, which says how the request identified
itself: `Docker Hub anonymous rate limit reached, try again later`,
`Docker Hub rate limit reached, try again later` (entry with credentials),
`rate limit reached (HTTP 429), try again later` (any other registry). `accessFor`
derives it from the entry's resolved `authMode` (no credential store read in
`Update`); the Hub search is always `accessHubAnonymous`, since it sends no
credentials whatever the entry holds, and a tag listing is `accessHubLogin` only for a
Hub entry whose mode uses credentials. The reason never carries `err.Error()`, a host
or a typed text. The footer is `m.footer.Error` only (Rule 128): `Image search on
<alias>: <reason>` at level 1, `Could not load tags[: <reason>]` for the tags; the
tables stay tables (Rule 139) and nothing is retried.

**Credentials stay on their origin (DEVDESK-410).** Four guards keep the Basic
credentials and the Bearer token from leaving the registry they were meant for:

- **The tag list never follows a `Link: rel="next"` to another origin.**
  `ListRegistryTagsContext` checks each `next` with `sameOrigin` (the catalog
  walker's guard) before the next request. A foreign `next` is an error that
  wraps `ErrTagsNextOffOrigin` (test it with `errors.Is`) and names the
  destination host only. No partial list is returned: a short list could pass
  for the whole one, and `imageupdate` and the security remediation would read
  a false "up to date". The catalog walker behaves otherwise: it returns
  `truncated=true` with the names already read, and no error.
- **`registryHTTPClient` has a `CheckRedirect`, `RefuseOffOriginRedirect`**
  (`internal/oci/guard.go`, exported since DEVDESK-412). A
  redirect is refused when its scheme, host or port differ from the first
  request of the chain (`via[0]`, not only the previous hop), so a chain that
  leaves the origin and comes back is refused too. The error reads
  `refusing redirect to another origin (<host>)` and wraps
  `ErrOffOriginRedirect` (`errors.Is`). It holds neither the target URL, nor
  its path or query: `StripRedirectTarget` strips the `*url.Error` that carries
  them, so a signed URL reaches neither the footer nor the logs. The limit of
  10 redirects (`MaxRedirects`, the `net/http` default) is kept; past it the
  error wraps `ErrTooManyRedirects` (`stopped after 10 redirects`) and no
  longer carries the last target URL either. It applies to everything that uses
  the client: tags, catalog and the token endpoint.
- **The token endpoint's answer is capped at 1 MiB** (`maxTokenResponse`). A
  larger body is an error (`token response over 1048576 bytes`), not a silent
  cut.
- **A Bearer realm on another origin gets no credentials (DEVDESK-629,
  D168).** `exchangeBearerTokenContext` calls `checkRealmOrigin`
  (`internal/oci/realm.go`) right after `checkTokenRealm`: when credentials
  are configured (username and password both set, the condition under which
  Basic is sent) and the realm's scheme, host or port differ from the request
  that received the challenge, the exchange stops before any request to the
  realm with an error wrapping `ErrTokenRealmOffOrigin`
  (`token realm on another host (<host>)`, `errors.Is`), naming the realm's host
  only and not a `StatusError`. Without credentials the anonymous token request
  goes out as before. The comparison is `sameOrigin`'s, exact: `host` and
  `host:443` are two origins (H2). Two exceptions, frozen in `realmExceptions`
  (a test pins the list), one way, https on both sides, host exact with no
  port: Docker Hub (`registry-1.docker.io` to `auth.docker.io`) and GitLab.com
  (`registry.gitlab.com` to `gitlab.com`). A self-hosted GitLab registry whose
  realm is on the GitLab host is refused. The refusal shows at the footer, level
  Error, as `token realm on another host`, in the three interactive flows of the
  registry browser: tags opened from the search (`Could not load tags: token
  realm on another host`), tags opened from the form (`Could not load tags: token realm on
  another host` since DEVDESK-667; it was the bare reason before) and image search (`Image search on <alias>: token realm on
  another host`), through `registryFailureReason`
  (`internal/ui/oci_resources/registry_failure.go`); the search verdict is
  `failed`. The realm's host stays in the journal. The other callers
  (`imageupdate`, signature, remediation) have no footer.

**Behavior change.** Some redirects that were followed are now refused: `http`
to `https` on the same host, `host` to `host:443`, and a host written with a
different case. The comparison is on the exact scheme and `host:port`, and the
guard refuses when in doubt.

**Behavior change (DEVDESK-629).** A registry configured with credentials whose
Bearer realm is on another origin (other than the two exceptions above) used to
receive them as Basic at its token endpoint; it now fails with
`token realm on another host`. Without credentials nothing changes.

**The other registry clients (DEVDESK-412).** The same two guards, plus a few
more, cover the clients DEVDESK-410 left out. The shared code is
`internal/oci/guard.go`, which `internal/registrymgr` imports (`internal/oci`
imports nothing from `internal/`).

- **`guard.go`** exports `MaxRedirects`, `ErrOffOriginRedirect`,
  `ErrTooManyRedirects`, `RefuseOffOriginRedirect` and `StripRedirectTarget`
  (which also unwraps the unexported `errInsecureRedirect` and the blob's local
  refusal), and two helpers. `ReadAllLimited(r, limit)` accepts `limit` bytes
  and fails with an error wrapping `ErrBodyTooLarge` (`response body over
  <limit> bytes`) on the next one: never a silent cut. `RedactURL(raw)` masks
  the password of a URL's userinfo (`xxxxx`) and returns `<invalid URL>` for
  anything that does not parse or has no host, so it never echoes it.
- **`oci.Client` (templates, `oci.go`) has two `http.Client`, both with a 1
  minute timeout for the whole request, redirects and body included**
  (`templateClientTimeout`). `httpClient` (tag list, manifest) uses
  `RefuseOffOriginRedirect`. `blobClient` (the layer blob) follows a redirect to
  another origin on purpose (decision of the user, D156): a registry hands blobs
  to a CDN or an object store. The credentials never go with it
  (`oci_blob.go`, `followBlobRedirect`):
  - at most 10 hops, to `http` or `https` only;
  - never from `https` to `http`, compared with the previous hop
    (`refusing redirect from https to http (<host>)`);
  - once a hop left the origin of the first request, `Authorization` and
    `Referer` are deleted from every following request, one that comes back to
    the origin included. `net/http` alone would copy `Authorization` to the same
    host on another port, across a scheme change and to a subdomain, and it does
    so before `CheckRedirect` runs, hence the explicit deletion;
  - when the registry is not local, a redirect to `localhost`, `*.localhost` or
    a literal loopback, link-local or unspecified (`0.0.0.0`) address is refused
    (`isLocalHost`). A name that resolves to such an address (DNS rebinding) is
    caught at the dial by `Dialer.Control` (`blobDialControl`), which sees the
    address about to be dialed. The `ip:port` of a configured proxy
    (`HTTP_PROXY`, resolved when the proxy is chosen for a request) is allowed
    at the dial, so a local proxy keeps working. A name in `/etc/hosts` or in
    the DNS that points to `127.0.0.1` is not "local" for the literal check:
    only the dial control looks at what it resolves to. A registry is local by
    the same rule (`localhost`, `*.localhost`, a literal address), which turns
    the two checks off.
  - **The digest is always checked, redirected or not.** `parseLayerDigest`
    accepts `sha256:` and 64 lowercase hex digits only, before the URL is
    built (the digest goes into it); anything else, `sha512` included, fails
    with `unsupported layer digest: only sha256 is supported`. The compressed
    stream is hashed while read, the bytes after the gzip stream included
    (`checkBlobDigest`); a mismatch is `ErrBlobDigestMismatch` and no entry is
    returned.
  - **The compressed bytes are capped** at `blobCap(Limits)`:
    `Limits.Bytes + Limits.Files*2048 + 1 MiB`, no cap when `Limits.Bytes` is
    zero. Over it is `ErrArchiveTooLarge`. The decompressed `Limits` do not see
    what the network carries (a directory entry with a large size, PAX headers).
  - Errors name the host of the failing hop, never its path or query
    (`download layer from <host>: <cause>`, `blobFetchError`). The status line
    a server wrote is not repeated, only the code and its standard text.
- **A Bearer `realm` is checked before any request** (`checkTokenRealm`, applied
  by `exchangeBearerTokenContext` itself). A realm that is neither `http` nor
  `https` (a relative one included) is refused. An `http` realm behind an
  `https` registry is refused too, **with or without credentials**: otherwise
  the Basic credentials, or a token request, would go in clear. The error is
  `refusing a token realm that is not a safe http(s) URL for this registry
  (<host>)`: the realm's host only, never its userinfo, path or query
  (`errInsecureTokenRealm`). A missing or unparsable realm passes this check
  and keeps its own errors, which no longer repeat the realm.
- **URLs are redacted in errors and logs.** The error prefix of
  `doRequestContext` and of the `(retry)` request use `RedactURL`; a request
  that cannot be built reports the cause and the redacted URL
  (`buildRequestError`). A registry address in a log goes through
  `oci.RedactRegistryURL` (`guard.go`), which takes an address with or without a
  scheme: without `://` it prefixes `https://`, redacts, then removes the prefix
  so the host name stays readable; an unreadable address gives `<invalid URL>`.
  `browser_bridge.go` (tags and tag-meta logs) and the login and logout logs of
  `registries.go` and `commands.go` use it. In `internal/template/fetch.go`,
  `redactGitURL` masks the source URL in `fetching ...` for `http(s)` URLs (the
  scp form `git@host:path` is kept as written); it is being extended to mask a
  token written as the user name of an `http(s)` URL as well. A password written
  in a registry URL (`https://alice:...@host`) reaches neither the footer nor
  `log.Printf`.
- **The Nexus detector** (`internal/registrymgr/nexus.go`) has the same origin
  guard (`nexusHTTPClient.CheckRedirect = oci.RefuseOffOriginRedirect`, 8 s
  timeout unchanged) and reads each reply through
  `oci.ReadAllLimited(..., maxNexusResponse)`, 1 MiB. The cap is a hypothesis,
  not measured. The anonymous retry of the group members still happens on any
  error but a refused off-origin redirect (see `registries.md`); the first
  attempt decides the outcome, the retry only changes it by succeeding.
  `fetchGroupMembers` no longer swallows its errors (DEVDESK-510): a
  `StatusError` from the management API with 401 or 403, or the 404 Nexus
  answers a pull account (DEVDESK-578), becomes
  `*registrymgr.MembersRefusedError` (`errors.Is(err, ErrMembersRefused)`,
  `Group`, `Status`, `Anonymous`; its text holds the group and the status,
  never a URL or a body; for the 404, `Cause` is the plain
  `*oci.StatusError`), any other error is returned as it is. `nexusGET` reads
  the body of a 404 only, up to 4 KiB (`maxNexusErrorBody`, a hypothesis: the
  measured body is about 50 bytes), and only to check that its JSON `message`
  is `Repository not found`; the body never reaches an error or a log. A 404
  with any other body, or a longer one, stays a plain status error. A reply
  without a `group.memberNames` key (empty body, `null`, `{}`) is an error;
  `memberNames: []` is a group with no member, a non-nil empty list. The format
  segment of the path is escaped (`url.PathEscape`). A non-200
  status is `*oci.StatusError{Source: oci.SourceManagementAPI}`
  (`management API returned 401`). A context error (cancel, deadline) is never
  a `StatusError`: when the context is done, a body cut mid-read reports the
  context's own error. A 200 reply that is not a repository document (empty or
  whitespace-only body, JSON without a `type` such as `{}` or `null`, invalid
  JSON) is an error wrapping `errEmptyReply`, `errNoRepositoryType` or the
  `encoding/json` error, never "not a group": the caller does not cache it
  (D23), so the members last known stay.
- **Each member is typed by one more GET (DEVDESK-636).** After the member names
  are known (either endpoint), `memberTypes` asks
  `/service/rest/v1/repositories/<member>` for each one (the name through
  `url.PathEscape`, `nexusHTTPClient`, the group's credentials or none, at most
  `maxTypedMembers` = 32 members and `typeLookupWorkers` = 8 at once) and keeps
  `type` when it is `hosted`, `proxy` or `group`. A failure (status, deadline,
  redirect refused, invalid JSON, `{}`, `null`, any other value) leaves the
  member's `GroupMember.Type` empty (unknown), is logged at INFO with the server
  values quoted (`%q`, an unrecognized `type` cut to 64 characters first), and
  never fails the discovery or changes the list. No lookup is started once the
  context is done (the loop checks `ctx.Err()` and its wait for a free slot
  listens to `ctx`); the members not reached stay unknown. A
  group refused or not a group asks for no type. Cost: up to 32 more requests per
  `ctrl+r` on a Nexus group, none at start-up or at search time. The measured
  generic answers (`rest-repositories-docker-{hosted,proxy}--*`) carry `type`
  for all three identities.
- **Docker Hub tag dates** (`fetchDockerHubTagsMeta`, `commands.go`) read the
  page through `oci.ReadAllLimited(..., maxHubTagsMetaResponse)`, 4 MiB.
  Measured on 2026-11-16 with `page_size=100`: 530,629 bytes for
  `library/postgres` (the largest), 443,387 for `library/python`, 259,560 for
  `library/node`; the cap is about 7.9 times the largest. A page over it is an
  error wrapping `ErrBodyTooLarge` and no dates are returned (no partial map).
  `ociHTTPClient` is unchanged (15 s timeout, anonymous call, no credentials).

**Behavior change (DEVDESK-412).** A Bearer realm in `http://` behind an
`https` registry is refused, and so is a realm with another scheme. A template
blob redirected from `https` to `http`, or to a loopback or link-local address
from a registry that is not local, is refused. A registry that serves its
layers with a digest other than `sha256` can no longer be used as an OCI
template source. A Nexus URL that redirects to another origin gives no members
(the `http` to `https` redirect of the same host included: enter the URL in
`https`).

**Residual risks.** The accepted ones (private networks not filtered, local
registry named by an alias, proxy behavior, CPU before the digest check, sha256
only, Nexus limits, and the rest) are listed in D156 in `docs/backlog.md`.

**Catalog walk (DEVDESK-306, IS-03).** `ListCatalogContext(ctx, apiURL,
username, password, maxPages, pageSize)` returns `(names, truncated, err)`. It
asks `<apiURL>/v2/_catalog?n=<pageSize>` and follows `Link: rel="next"` for at
most `maxPages` pages (non-positive limits are an error). `truncated` is true
when names were left: the page cap was reached with a `next` link, more than
`maxPages*pageSize` names came back (the list is cut to that), or the `next`
link points to another origin. A name already returned is skipped silently
(first appearance order kept; a Nexus group repeats names across pages, the
measured `n=2` walk, DEVDESK-634), and the `maxPages*pageSize` cap counts
distinct names. Each page body is capped at 1 MiB; a larger
page is an error, not a silent cut. A non-200 answer is a `*StatusError`.
Authentication is the tags' flow: Basic, then Bearer on a challenge, the token
kept across pages.

- **`next` is followed only to the same origin.** `sameOrigin` compares scheme
  and host strictly (`host` and `host:443` differ), so a registry cannot make
  the walk send the Basic credentials or the token elsewhere. A foreign or
  differently written `next` ends the walk as `truncated`, with no error.
- **HTTP 3xx redirects are hardened too** (DEVDESK-410): the walk goes through
  `registryHTTPClient`, whose `CheckRedirect` refuses a redirect to another
  origin (see "Credentials stay on their origin" above).
- **Under a prefix, `next` must also stay on the member's catalog path.** A
  non-empty prefix follows a `next` only when its escaped path is exactly
  `/v2/<prefix>/_catalog` (same origin already checked); a link to
  `/v2/_catalog` or to another repository would attach the names of another
  catalog to the prefix, so it ends the walk as `truncated`, unfollowed. With
  an empty prefix any same-origin path is followed, as before.

**Catalog under a prefix (DEVDESK-579, NX-12).** `ListCatalogUnderContext(ctx,
apiURL, prefix, username, password, maxPages, pageSize)` is the same walk on
`/v2/<prefix>/_catalog`, the catalog of one Nexus group member; `ListCatalogContext`
is it with an empty prefix, and the two rules above apply to it unchanged. Names
come back as the server sent them, without the prefix (the caller rebuilds
`<prefix>/<name>`). Each prefix segment goes through `url.PathEscape`, so a
space, `%`, `?`, `#` or an already-encoded `%2F` stays one segment on the wire.
A prefix is refused before any request, with an error that gives the rule and
never carries the prefix or a credential, when it has an empty segment (leading,
trailing or doubled `/`), a `.` segment, any `..` (read literally, `a..b` too),
a backslash, a segment of spaces only, a control character (NUL included), a
segment over 255 bytes or a prefix over 1024 bytes. The blank segment, the
control characters and the length bounds are a choice: no repository name holds
them. Under a prefix, `next` is also held to the member's catalog path (last
bullet above).

## Image search — `internal/imagesearch` (DEVDESK-298, IS-18)

Pure Go, no UI. Types and one interface; two adapters, `HubSearcher` (IS-02)
and `CatalogSearcher` (IS-03); dhi.io is IS-17:

- `Target`: one registry to search (`EntryKey`, `Alias`, `RegistryURL`,
  `APIURL`, `RepoPrefix`, `Username`, `Password`; the credentials are empty for
  an anonymous entry, D12).
- `Query{Text, Limit}`: `Limit` 0 is the adapter's default.
- `Image`: one result (`EntryKey`, `Name`, `Repo`, `Description`, `Stars`,
  `HasStars`, `Official`). `Repo` is the final path, with the prefix and
  `library/` applied once (D39).
- `Searcher.Search(ctx, Target, Query) (images, partial, err)`, and
  `SearcherFunc`, the function adapter.
- `HubSearcher` (DEVDESK-301, IS-02, `hub.go`): anonymous, one GET on
  `https://index.docker.io/v1/search` (the endpoint `docker search` uses, not a
  public Hub contract), one page of at most `HubLimit` (50) results, no
  credentials (Target's are ignored), no redirects, 15 s client timeout, body
  capped at 1 MiB. A non-200 answer is an `*oci.StatusError`. A result without
  `star_count` keeps `HasStars` false (the Stars cell stays empty, never 0).
  Names are validated against a regex (`hubComponent`: lowercase alphanumerics
  joined by `.`, `_`, `__` or runs of `-`, as in the OCI distribution spec); a
  name that fails is dropped (`-rm`, `--help`, uppercase, `a//b`, a leading or
  trailing separator). A description is sanitised: escape sequences, control
  characters and Unicode format characters (category Cf: bidi overrides,
  zero-width, BOM) are removed. Privacy: the text the user typed is sent to
  Docker Hub as soon as a Hub entry is checked.
- `ByRegistry(hub, other)` (`route.go`): a target whose host is exactly `dhi.io`
  (DEVDESK-303, `isDHI`: case, scheme, port and path ignored; `registry.dhi.io`
  and `notdhi.io` are not dhi.io) is refused first, with `ErrUnsupported` and no
  request to either searcher, since dhi.io has no documented catalogue API
  (IS-17). Otherwise a target for which `oci.IsDockerHub(RegistryURL)` is true
  goes to `hub`, any other to `other`. A nil searcher answers `ErrUnsupported`
  with no request at all. Production uses
  `ByRegistry(HubSearcher{}, CatalogSearcher{})`: Hub goes to the Hub search,
  every other registry to its catalog. `isDHI` parses the URL (`url.Parse`, with
  `https://` added when there is no scheme) and compares `Hostname()`, so
  userinfo (`dhi.io@evil.example`), a trailing dot and a port cannot fool it.
- `CatalogSearcher` (DEVDESK-306, IS-03, `catalog.go`): searches a private
  registry by walking `/v2/_catalog` through `oci.ListCatalogContext` (or, for a
  Nexus member, `/v2/<prefix>/_catalog`, see "Catalog under a prefix" below) and
  filtering **client-side**: the text the user typed is never sent to the
  registry. Steps, in order:
  1. The query is trimmed; fewer than `CatalogMinQueryRunes` (2) runes returns
     `ErrQueryTooShort` with **no request**.
  2. The walk: `CatalogPageSize` (1000) names per page, at most
     `CatalogMaxPages` (10) pages, so at most 10 000 names. A cut walk sets
     `truncated`. An error from the walk (status, deadline, unreadable page)
     is returned as is, with no row.
  3. Guard R1 (before the filter), **root catalog only**: when the target has a `RepoPrefix`, only the
     names that start with `<prefix>/` are candidates. If the walk is complete and
     no name carries the prefix, the answer is `ErrPrefixNotInCatalog` (the
     cause names the prefix); this protects against a catalog that does not
     list names the way the entry reaches them and would otherwise read as a
     false "no result". The assumption that a Nexus catalog lists names prefixed
     by the repository was **contradicted** on the bench (Nexus 3.98.1-01
     Community, 2026-11-17, NX-03, hypothesis H1 of IS-03; D167): Nexus lists
     names without the prefix, so a Nexus member no longer goes through R1 (see
     `CatalogUnderPrefix` below). R1 still applies to every other registry that
     has a `repo_prefix`; none of them was measured. If the walk is truncated, the
     prefix may be on a page not read: the answer is no result with
     `partial` set, not an error. A target with `CatalogUnderPrefix` skips this
     step (see below).
  4. Filter: case-insensitive substring match on the part **after** the prefix.
     `Image.Name` is that part, `Image.Repo` the full catalog name.
  5. Only valid names are kept (`validRepoPath`, `names.go`: every
     `/`-separated component follows the distribution-spec grammar, as for Hub);
     others are dropped.
  6. Order by `MatchTier`, then name, then `Repo`; at most `CatalogMaxRows`
     (100) rows (`Query.Limit` can only lower that). Rows cut by the limit set
     `partial`, as does a truncated walk.

  `MatchTier` (ranks 0 to 4, see the order below) now lives in `imagesearch`
  (`names.go`) so the searcher and the table share one ranking;
  `oci_resources.matchTier` delegates to it. The 15 s per-registry deadline
  (`imageSearchDeadline`, below) covers the whole walk; hitting it is a
  `failed` verdict with no row. `CatalogSearcher.List` replaces
  `oci.ListCatalogContext` in tests. Walks per search
  (DEVDESK-311, IS-07, `walks.go`): `WithSharedWalks(ctx)` puts a memo on the
  batch context and `CatalogSearcher` goes through `walkCatalog`, so a search
  queries `/v2/_catalog` once per API URL and identity. Without the memo
  (any other caller) every call walks on its own.
  - Key: the exact API URL (trailing slash ignored), the catalog prefix
    (`catalogPrefix`, empty for the root catalog; DEVDESK-579) and the identity, an
    HMAC-SHA256 of username and password under a random per-batch key: no
    secret is held in clear and the key prints no userinfo. Two identities
    never share a walk, because Harbor, Artifactory and GitLab filter the
    catalog per account. A `credentials` entry with no effective secret shares
    the walk of an anonymous entry. A member on another port or subdomain has
    another API URL and is not grouped. Two Nexus members of one host read two
    different catalogs, so they have two keys and two walks: D140 no longer
    merges them (D167 amends it). Entries on the root catalog still share one
    walk per API URL and identity.
  - Leader and followers: the first call for a key walks, the others wait (or
    return on their own context). It is a memo, not a singleflight: the key is
    kept after the walk, so a member arriving later reuses the result, and it
    dies with the batch (no cache between two searches).
  - A shared failure (a 503, say) is the same error for every member, so the
    same verdict (`failed` or `refused`), one message per entry. A result
    caused by the leader's own context ending is not memoized: followers
    already waiting on that flight receive the leader's context error (verdict
    `failed`), and only members arriving afterwards walk again. A
    panic in the leader gives followers a sentinel error (`errWalkAborted`),
    never an empty catalog.
  - `repo_prefix` filtering, `partial`, `ErrPrefixNotInCatalog` and the
    `May omit images` mention stay computed per member.
  - Members of one Nexus host do **not** answer on the same `/v2/_catalog`
    (measured, Nexus 3.98.1-01): each answers on its own path catalog, which is why
    the key carries the prefix. This contradicts the shared-walk premise of
    IS-07 / D140 for Nexus members (measured on the bench, Nexus 3.98.1-01
    Community, 2026-11-17, NX-03; D167). For other registries on one host
    (Harbor, Artifactory, GitLab) the premise is neither confirmed nor
    contradicted: not measured.
  A catalog may be disabled or filtered to the account's rights (Harbor,
  Artifactory, GitLab): the answer is then `refused` or a shortened list.
- **`Target.CatalogUnderPrefix` (DEVDESK-579, NX-12).** A Nexus member has no
  usable root catalog: `/v2/_catalog` answers 404 on the HTTP port, and on a
  group connector it lists the members' names together with no member attribution.
  With the flag set, `CatalogSearcher` hands the lister `catalogPrefix = RepoPrefix`
  and reads `/v2/<prefix>/_catalog`. Differences from the root catalog:
  - Nexus returns names **without** the prefix; `Image.Repo` is rebuilt as
    `<prefix>/<name>`.
  - No guard R1: every name belongs to the member. An empty catalog is
    `no result`, not `refused`.
  - A prefix that fails `validRepoPath` is `refused` before any request. That
    check refuses uppercase, so a catalog name outside the OCI grammar is
    ignored and a member whose repository name has uppercase is refused. Not
    measured: whether Nexus serves `/v2/<UpperCaseName>/_catalog`.
  - Verdicts: 404, 401, 403 and 405 are `refused`; 5xx, 429 and network errors
    are `failed`.
  - The walk key holds the prefix (D140 amended, above). No request goes to
    `/v2/_catalog` for these targets.
  Who sets the flag is the UI's decision: see "Image search and Nexus members"
  below. Every other target keeps the root catalog and R1.
- **Verdicts (DEVDESK-303, IS-12, `verdict.go`).** `Classify(err, n, partial)`
  is a pure function that turns one registry's answer into an `Outcome{Verdict,
  Partial, Skipped}`. `Verdict` is one of `VerdictOK` (`ok`), `VerdictNoResult`
  (`no result`), `VerdictRefused` (`unsupported or refused`) and
  `VerdictFailed` (`failed`); the zero value is not a verdict (`pending`) and
  `Classify` never returns it. `Partial` and `Skipped` are marks, not verdicts:
  `Partial` with `ok` or `no result` (invariant Q1: a walk cut short that found
  nothing is still `no result`, marked `Partial`), never with `refused` or
  `failed`, never with `Skipped`; `Skipped` only with `no result`. Rules, first
  match wins:
  1. no error: `n > 0` is `ok`, `n == 0` is `no result`; `Partial` is kept as
     given in both cases;
  2. `ErrQueryTooShort` is `no result` with `Skipped`;
  3. `ErrUnsupported` or `ErrPrefixNotInCatalog` is `refused`;
  4. a `*oci.StatusError` is `refused` when `refusedStatus` holds: 401 and 403
     from either source, 404 and 405 from `SourceRegistry` only (a 404 from the
     token endpoint is a failure), except a 404 marked `UpstreamBlocked()` (a
     Nexus proxy whose upstream is blocked, DEVDESK-632), which is `failed`;
     any other status, 429 and 5xx included, is `failed`;
  5. anything else is `failed`: deadline, cancel, network, unreadable or
     oversized body, invalid request.

  `ErrQueryTooShort` and `ErrPrefixNotInCatalog` are returned by
  `CatalogSearcher` (DEVDESK-306).

## Image updates — `internal/imageupdate` (§3.88)

Three tables carry an **Update** column (`internal/ui/updatecol`): oci's Images
tab, the containers list, and the Remediation tab of a scan result. It shows an
arrow when the image's registry holds something newer, and says what:

| Kind | When | Cell |
|---|---|---|
| `NewPatch` | the tag has ≥ 3 version components and a tag differing only by the last, same variant, is higher (`20.11.1-alpine` → `20.11.4-alpine`) | the arrow and the tag |
| `NewBuild` | the digest the tag points to now is none of the local image's digests | the arrow alone (`IconArrowDown`) |

A newer patch wins over a new build. Every other state says why there is no
arrow, in grey — a blank used to stand for all of them, which in the
Remediation tab (bases rarely pinned, often not held locally) meant a column
that was silent without saying why:

| Kind | Cell | When |
|---|---|---|
| `UpToDate` | the check mark | the local digest is the tag's |
| `Pending` | `IconHourglass` | no answer yet |
| `Failed` | `IconHelpCircle` | the registry did not answer (logged) |
| `LocalBuild` | `IconHammer` | images, containers: no registry digest |
| `NotLocal` | `IconCloud` | Remediation: unpinned, and not held by the engine |
| `Pinned` | `IconPin` | a digest with no tag: nothing can move |
| `None` | blank | the question does not apply (untagged image, unresolved `FROM`) |

Every state but a newer patch is one glyph (`updatecol.Cell`): the words cost
eleven cells in the widest table there is, and the reason is still spelled out
by `G`'s greyed shortcut in the footer and by `?`. `Status.Label` keeps the
word, which is what the filter matches. `Evaluate` takes what "no local digest"
means to its caller (`LocalBuild` or `NotLocal`). The column's filter only matches an update's label, or `/ca`
would find every `local build`. A tag with fewer components (`3.20`,
`20`) has no patches of its own — it floats over them, so a new patch moves the
tag and the digest says it. That is also the only answer a floating tag
(§3.79) ever gets.

**The registry's side is cached, the comparison is not.** `imageupdate.Check`
keeps, per reference, the tag's digest and the newer patch tag in
`~/.devdesk/cache/image-updates.json` — 6 hours for an answer, 30 minutes for a
failure (unknown repository, no credentials) so it is not asked on every
refresh. `Evaluate` compares with the local digests each time a row is built,
so a pull clears the arrow without asking the registry again.

**Two requests at most per reference.** The digest is a `HEAD` on the manifest
(`oci.ManifestDigest`) accepting an OCI index and a Docker manifest list — a
multi-platform tag's digest is the index's, the one `docker pull` records in
`RepoDigests`. Docker Hub does not count a `HEAD` against its pull rate limit.
The tag list (`oci.ListRegistryTags`) is only read for a tag with a patch
component. At most four references are asked at once; credentials are the
engine's (`docker.GetStoredCreds`), as for the Remediation tab. For a URL with
a path the lookup is exact key first, then `host[:port]` as written (never the
host alone when the port differs, no default port made explicit), shared with
`RegistryLoginState` through `credentialPhases` (DEVDESK-635); logout stays on
the exact key. These callers pass the image's own registry host, so the login
they find goes to that same host; only the group discovery can name a second one
(`management_url`), see `registries.md`.

**What each view compares with:**

- *Images* — the image's own `RepoDigests`, now read by the same `image
  inspect` that reads its size (`templates().ImageInspect`). Only a tagged image
  **with** a digest is asked about: one built or loaded here has none, and its
  name would send the question to a registry that never heard of it.
- *Containers* — the digests of the image the container was **created from**
  (`docker.ContainerImageDigests`: `container inspect` for the image ID, then
  `image inspect`). After a pull the container still runs the old image, so the
  arrow stays until it is recreated — which is what it is there to say. The
  digests are read again only when the set of containers changes, not on every
  two-second refresh.
- *Remediation* — the digest the Dockerfile pins, or else the image the engine
  holds under that name (`docker.ImageRepoDigests`). A base the engine does not
  hold only gets the patch side.

`imageupdate.Tracker` is what each view keeps between checks: the facts it has
and the references a check is out for, so a reload asks only what is new or
stale. Every view reaches the network through a package variable its tests
replace.

**Updating — `G` on the Images tab.** It pulls what the column points at (the
newer patch tag, or the same tag again for a new build) and removes the image
it replaces, by ID and never forced. It refuses while any container, running
or stopped, was created from that image (`docker.ContainersUsingImage`, `ps -a
--filter ancestor=`): removing it would break the container, and keeping it
would leave the update half done. The check is made twice — from the engine's
container count to grey `G` (Rule 130; docker only, podman reports none), and
against the engine just before the pull, since a container can appear in
between. A pull that brings the image already held removes nothing
(`docker.SameImageID`). It runs as a pull job, so the row spins and `K` stops
it; every refusal and failure is a footer line naming why (the containers in
use, a failed pull, an old image that could not be removed).

**On a narrow terminal**, the containers table drops the two gauges before
Update (they are `DropFirst`, §3.71), then the I/O counters, then Update. Both
gauges now need 180 columns instead of 160.

**Not done**: a digest for a tag the engine holds under another name, and the
`podman` side of `RepoDigests` has not been measured against a real `podman
system service` — the field exists there under the same name.

**From a container to its image — `J`** (§3.89). The containers view sends
`ociresources.FocusImageRequestMsg` with the image reference exactly as the
engine printed it; the router switches to the OCI view and hands it the
request, which waits for the first listing if the view was just built
(`deferUntilListed`, the same queue agents use). Matching is the OCI view's
(`imageMatches`, `focus.go`): an implicit `latest`, docker's `nginx` against
podman's `docker.io/library/nginx`, a registry port that is not a tag, a
`repo@sha256:` digest against `RepoDigests`, and a bare ID — what `ps` shows
once the tag has moved to a newer image. A search hiding the target is
dropped; an image that is no longer listed is a footer `Warn`. There is no
`esc` back: `:ct` returns, as from any other view.

## The OCI tables and the activity column — `internal/ui/oci_resources` (DEVDESK-146, A-09a; DEVDESK-153, A-09b)

The four tables of `:oci` (Images, Networks, Volumes, Registries) and the
registry browser's tag table are on the activity column (Rule 125; the router
side is in `app-shell.md`).

**Periodic refresh and terminal focus (DEVDESK-210).** `refreshInterval` is 10 s
(a var, so a test can shorten it). `RefreshTickMsg` used to refresh once and
never again; it now re-arms `tickCmd()` on every tick. While `Model.blurred` is
set (from `shared.TerminalFocusMsg`) the tick only re-arms and skips
`refreshReads()` (images, scan cache, networks, volumes; it never scans, Rule
126). On the focus edge (`Resume`) it runs `refreshReads()` once, with no new tick.

**Registry browser (DEVDESK-218, A-09a2).** The tag table
(`registry_browser.go`, built by `newTagTable`, which `ActivityTables` also uses
so the guard sees the real table open or not, as `oci/registry-browser`) takes
`datatable.IndicatorColumn` as column 0, an 8th column; Registry, Tag and
Updated are 1 to 3 (`tagColumnTag` = 2, `tagColumnUpdated` = 3). `tagRow` holds
`Activity components.RowActivity` and `ScanRunning`, stamped by
`rebuildTagTable` from `components.DescribeRow` (scan and pull, `ociKinds`,
target `jobs.TargetKey(imageName)`) over the snapshot the router pushes through
`SetJobs(runs, failures, context)`; the cell at rest is empty. While a scan
really runs (not queued) the C/H/M/L cells hold `…` in `DimStyle`; a queued scan
or a pull keeps the counts. The blink phase arrives with `SetBlinkPhase` and
`WantsBlink` is true for a visible running tag row. Rule 116: Updated drops
under 102 columns, L under 84, M under 78, H under 72, C under 66; at 60 only
the dot, Registry and Tag remain. The browser has no spinner of its own
(DEVDESK-374): a search shows the shared activity dot in the footer
(`Searching registries...`), and `WantsBlink` is true while it runs. After the search the
footer describes the selected tag (`Status{Detail}`). `tagScanSpinnerIdx` and
the local frame are deleted. `DescribeRow` filters by context: a scan of another
context no longer shows on a tag (D108).

**Cancelling a lot and ignoring late answers (DEVDESK-298, IS-18).** The
browser launches requests in lots (one per validation that starts something).
`browserGen` is a package-wide `atomic.Uint64`, incremented only from `Update`
(`nextBrowserGen`). `beginRequests()` cancels the previous lot (one
`CancelFunc`, `cancelRequests`, per lot, not per registry), takes a new
generation and resets `pendingSearches`; the `context` is born in `Update`
(Rule 110). `MultiRegistryTagsLoadedMsg`, `MultiRegistryTagsMetaMsg` and
`ImageSearchResultMsg` carry the `Gen` they were started with, and their
handlers return before logging anything when it differs from the open
browser's. The counter is global rather than per browser so that a browser
closed and reopened, or a new `Model` after a context switch, cannot accept the
answer of an older instance. `closeMultiRegistryBrowser` calls
`cancelInFlight()`, which also covers closing through `G` (a pull). A validation
that starts nothing (empty name, no registry checked) cancels nothing and
leaves `gen` alone. The tag lookups go through the `listRegistryTags` variable
(`oci.ListRegistryTagsContext`), the seam tests swap to capture the context;
there is no deadline on a tag lookup, only `maxTagPages` and the per-request
client timeout. `Model.RegistryBrowserGen()` exposes the generation to tests
only.

**Level 1: image search (DEVDESK-299, IS-01; Open Tags DEVDESK-300, IS-11).**
The browser has three states: `browserStateInput` (form), `browserStateImages`
(level 1, appended last in the `iota` list) and `browserStateTags` (level 2).

*The form's buttons.* `formButtons()` (`browser_form.go`) is the single source
of the buttons: the keyboard, the rendering (`renderFormButtons`, one line) and
the header (`FormEnter`) all read it. Without a `Searcher` there is one button,
`Browse Tags` (action `actionSearch`). With one there are two, `Search`
(`actionSearch`) then `Open Tags` (`actionOpenTags`).

*Field indices.* Repository is `brFieldRepo` (0); the picker checkboxes are
1..n (n = `len(b.rows)`); the buttons follow: `brFieldButton(i)` = `1 + n + i`,
and `brFieldLast()` is the last button. `focusedButton()` maps the focus back to
a button (false on Repository or a checkbox). `down` stops at `brFieldLast()`.

*Validation.* `submit(action)` is common to both actions: an empty name starts
nothing; no registry checked sends `RegistryBrowserWarnMsg{"No registry
selected"}`, which the `Model` shows as a footer warning (the browser has no
footer; this also applies to a name that contains `/`, where the tag flow used
to refuse silently). Then `actionOpenTags` always goes to `startTagLookup`
(the former code extracted as is, name as typed). `actionSearch` follows the
IS-01 rule: a name with no `/` goes to `startImageSearch` when `b.searcher !=
nil`, anything else to `startTagLookup`. `enter` on Repository runs
`actionSearch`; `enter` on a button runs that button's action; `enter` on a
checkbox only advances the focus. The selection applies to both actions.

*Header.* `FormEnter()` returns the label and the lit state of the header's
single `enter` entry. The label follows the focus: `Search` on Repository and
on the Search button (`Browse tags` without a `Searcher`), `Open tags` on the
Open Tags button. `enter` is lit on Repository (Rule 130: it submits there) and
on a button, and greyed on a checkbox.

*Help.* `registryBrowserHelpBody(searchEnabled)` (`view.go`) depends on the
mode (Rule 114): without a `Searcher` it names Browse Tags only; with one it
describes Search, Open Tags and the shared selection, and the level 1 sentence.

**`defaultImageSearcher` is `ByRegistry(HubSearcher{}, CatalogSearcher{})` in
production (DEVDESK-301, IS-02; catalog since DEVDESK-306, IS-03), so level 1
and Open Tags are active.** `newRegistryBrowser`
copies it into `b.searcher`. Tests do not use it: `TestMain`
(`ociresources_test.go`) saves it in `productionImageSearcher` and sets
`defaultImageSearcher` to `nil` for the whole suite (no test may reach the real
Hub), and tests inject a fake with `withImageSearcher`.
`TestProductionWiresTheHubSearcher` reads the saved value. `internal/app`
injects nothing: its tests avoid level 1 by typing a name with a `/`.

`startImageSearch` runs one `searchImagesCmd` per checked registry, with
`targetFor(entry)` (same credentials as the tag flow, `credsFor`). Each command
wraps the lot's context in `context.WithTimeout(ctx, imageSearchDeadline)`:
`imageSearchDeadline` is 15 s **per registry**, the whole walk included, on top
of the 15 s client timeout per request (`imageSearchDeadline = oci.RequestTimeout`,
see "The abandon after 15 s is typed" above). A registry's
answer arrives as `ImageSearchResultMsg{Gen, EntryKey, Images, Partial, Err}`
and `AddImageResults` takes it (false when stale; `pendingSearches` never goes
under 0).

**Verdicts per registry (DEVDESK-303, IS-12).** `AddImageResults` calls
`imagesearch.Classify` in `Update`, never in the `Cmd` (Rule 110), and stores the
result in `RegistryBrowser.outcomes`, keyed by `EntryKey`. The map is reset by
`startImageSearch`, so it holds one lot only; a registry with no entry is still
pending. Only an `ok` verdict appends rows. `Outcome(entryKey)` reads one
verdict (false while pending). `Model.RegistryBrowserOutcomes()` returns a copy
of the map, or nil when the browser is closed; it is for the tests and for IS-13.

The Nexus verdicts below rest on measures made on the local bench, not on a
company instance: read "Measured Nexus behavior" after this section before
trusting a verdict for another Nexus.

**Image search and Nexus members (DEVDESK-579, NX-12).** `targetFor` sets
`CatalogUnderPrefix` on two kinds of entry: a plain entry that lists cached
content only and has a `repo_prefix` (a `parent:` entry of a `provider: nexus`
group), and a discovered member of a Nexus group that has a `repo_prefix`. The two
tests are equivalent, because `checkRepoPrefixes` refuses a `repo_prefix` on a
group. Any other entry keeps the root catalog and guard R1.

`searchUnits` decides what a lot searches. Discovered members **without an
address of their own** (same URL and same `repo_prefix` as their group, with the
group's slug as `ParentSlug`) are merged into **one** unit, the group, labelled
`<alias> (group)` (`groupUnitSuffix`). It is walked once, and its rows name no
member: Nexus does not say which member holds a name. By design this bypasses the
picker's selection: ticking one bare member searches the whole group, and a row can
carry the name of a member that was unticked. A group that was not discovered (a
pull account) is searched as itself, with its bare label. The Registry column
keeps the whole suffix when it cuts a cell (`datatable.Column.KeepSuffix`, see
`ui-components.md`). `lotUnits` holds the units of the current lot until the next
image search; `entryFor` falls back on it, so labels, the `→` tags drill of a group
row and the `r` filter work on a unit. The `?` help says: `'nx (group)': images
found through the group; Nexus does not say which member holds each one.`

**Measured Nexus behavior (NX-03, NX-04, NX-05).** Where the code rests on how
Nexus answers, this is what was observed and what was not. Every row was
**measured on the local bench**: Nexus Repository **3.98.1-01 Community**
(`docker.io/sonatype/nexus3:3.98.1`), k0s cluster, HTTP only, no reverse proxy,
no TLS, a file blob store (reported, not independently verified), three repositories holding six image names, one pull account with rights on
all Docker repositories. It was **not measured on the company's Nexus instance**:
the user decided on 2026-11-17 not to cross-check it, and that has not been done
since. The only datum from a company instance is the 2026-08-10 note (a pull
account refused on the detailed endpoint, Nexus version not recorded); it was
not re-measured there. The raw answers and the dates are in the frozen notes
`docs/measurements/2026-11-17-nexus-catalog.md` (NX-03),
`2026-11-20-nexus-cached-only.md` (NX-04) and `2026-11-20-nexus-access.md`
(NX-05); those notes describe the code of their day, this section the code now.

| Behavior | Measured (3.98.1-01 Community, bench) | Verdict on the old assumption | Code now |
|---|---|---|---|
| `_catalog` prefix (2026-11-17) | Names carry **no** repository prefix, whatever the URL form. `/v2/_catalog` on the HTTP port is a 404 (HTML): no root catalog. A hosted or proxy repository answers on `/v2/<repo>/_catalog` (200); a group answers on its own connector (root of its port: the union of its members, no attribution) and 404s under `/v2/<group>/_catalog` | H1 of IS-03 (prefixed names) and IS-07 (members of one host share a root catalog): **contradicted** (D167) | `ListCatalogUnderContext` reads `/v2/<prefix>/_catalog` and rebuilds `<prefix>/<name>`; one walk per member; a group is one unit |
| `_catalog` rights (2026-11-17) | The pull account and the anonymous account get 200 and the full list, same as admin | H2 (a pull account may call `_catalog`): **confirmed** for the bench accounts | `refused` only on 401, 403, 404, 405 |
| Detailed group endpoint (2026-11-17) | `GET /service/rest/v1/repositories/<format>/group/<name>`: the pull account gets **404** `Repository not found`, anonymous gets 403, admin gets `memberNames`. The generic endpoint answers all three, without members | The D161 expectation of a 403 for the pull account: **contradicted on the bench**; the 403 seen on 2026-08-10 on a company instance (version unknown) is **not settled** (DEVDESK-578) | the 404 with that exact message is a members refusal (`registries.md`) |
| Proxy content (2026-11-20, NX-04) | Upstream reachable: `_catalog` and `tags/list` list what the **upstream** holds (`mirror/uncached` is listed and pullable); a HEAD on a manifest pulls it **and writes it to the proxy cache**, `tags/list` and `_catalog` do not (cache writes read from `status` output, reported, not independently verified; NX-03 saw the same on an unrequested HEAD, attribution inferred). Upstream cut: the first `_catalog` read gets a 502 after about 20 s (reported, not independently verified), then the proxy auto-blocks and answers 404 `Remote Auto Blocked until <date>` to `_catalog` and `tags/list`, even for an image in its cache; the block is temporary (Nexus lifted it 21 s to 79 s after the cut, and `restore-upstream` lifts it at once); a HEAD still serves the cached image (200) and 404s the uncached one. The group's `_catalog` then omits the proxy's names with a 200 and its `tags/list` of a proxy image is an empty 200 | H3 / IS-14 / D137 (a proxy lists its cache, "limited to cached content"): **contradicted** | D168 (DEVDESK-629): `May omit images` replaces `Cached only`; the blocked 404 is `failed`, marked `UpstreamBlocked()` (DEVDESK-632); a delay is `no answer within 15 s` (DEVDESK-633) |
| Routing (2026-11-20, NX-05) | By path on the HTTP port (`/v2/<repo>/...`, also `/repository/<repo>/v2/...`) and by the group's connector: all answer 200 for admin, pull and anonymous | path and group connector: **confirmed** | DevDesk calls `/v2/<repo>/...` and the connector; it never uses the `/repository/` form (D39) |
| Bearer realm (2026-11-20, NX-05) | An anonymous `GET /v2/` is a 401 `Bearer` whose realm is on the same origin as the request, with the connector or repository path; the token request answers 200 even anonymously, but replaying `GET /v2/` with that token was a 401 again (nominal and with `DockerToken` removed). A wrong password gets a 401 `BASIC`, no `Bearer` challenge. With valid credentials DevDesk sends Basic first and never enters the Bearer flow. The realm is built from the announced host: `X-Forwarded-Host` and `X-Forwarded-Proto` change it | same-origin realm: **confirmed**. Token replay: on `GET /v2/` the token did not make the request accepted (the NX-05 note says so for this bench); NX-03 had a token accepted on a group-connector catalog. No other authenticated Bearer flow measured, and the answer to the token request with anonymous access cut is not | D168: a realm on another origin gets no credentials (`ErrTokenRealmOffOrigin`, two frozen exceptions, see "Credentials stay on their origin") |
| Redirects (2026-11-20, NX-05) | No 3xx and no `Location` in any answer (`tags`, `_catalog`, manifest HEAD, blob GET on `docker-hosted`, `/v2` without a slash) | none observed on this bench (same origin); off-origin: inconclusive (file blob store, no S3, no reverse proxy: reported, not verified; nothing read through the proxy or group) | the off-origin refusal stays (`RefuseOffOriginRedirect`), covered by hand-written tests only |
| `Link` and `n` (2026-11-17, 2026-11-20) | `n` is respected, `Link` is absolute and same-origin; `?n=1000` on `/v2/<repo>/_catalog` is a 200 for hosted and proxy, a 404 for the group by path and for the HTTP root. A group on its connector with `n=2` repeats names across pages (5 pages for 6 names) | confirmed (repeat: a new fact) | the catalog walk drops repeated names (DEVDESK-634) |

**Still open: not measured, or not conclusive.** Each one says what is missing.

- **An account restricted to some repositories** (a silent filtering that would
  read as a false "no result"; also the 403 or 404 of the detailed endpoint for
  such an account): the bench has no such account. Needs a role and a user limited
  to one Docker repository, which is a write on the bench.
- **A different Nexus version, a Nexus Pro, a Nexus behind a reverse proxy, HTTPS,
  LDAP, an S3 blob store, `forceBasicAuth`**: one version on one HTTP bench. The
  403-versus-404 difference with the 2026-08-10 note may come from the version;
  needs the version of that instance or a second bench.
- **A realm or a redirect on another origin**: the bench produces none (only the
  `X-Forwarded-*` probe shows the realm follows the announced host). Needs a
  reverse proxy in front of the bench, or an S3 blob store.
- **Subdomain routing and a dedicated connector for a hosted or proxy
  repository** (also `docker.subdomain`): not tried (W7 and W8 not done); the
  subdomain connector is documented as Pro, unverified on Community.
- **Whether the group's 404 under `/v2/<group>/_catalog` comes from
  `pathEnabled`** (on the three repositories), and the tolerance of `?n=1000` on
  `/repository/<repo>/v2/` paths and for the pull and anonymous identities of the
  group connector.
- **An uppercase repository name** (`/v2/<Name>/_catalog`).
- **An anonymous call with anonymous access cut**: the 401 `Bearer` is known, the
  answer of its token request is not.
- **The duration of the auto-block on `_catalog`**, a proxy with an empty cache,
  and the group's `tags/list` of a homonym with the upstream cut.
- **Logout of an entry with a path** (`U`): probably clears every repository of
  the host (D168, R6); not measured.
- **Why a group repeats names across pages**: the group probably merges the pages
  of its members; not settled.

**Level 1 header summary (DEVDESK-305, IS-13).** `searchHeaderInfo`
(`browser_summary.go`, called from `GetHeaderInfo`) says what the lot did, one
header line per class, in this order: `Images` (the row count, always), then
`Pending` (a count, live lot) or `Interrupted` (names, the lot was abandoned
with the left arrow before they answered), `Failed` (red), `Refused`, `Partial`
(orange), `May omit images` (orange, DEVDESK-308 IS-14, reworded by DEVDESK-629 F2), `No result` (dim) and `Skipped` (dim, catalog not walked because the
query is shorter than `imagesearch.CatalogMinQueryRunes`). A class with nothing
to say has no line, so a lot where every registry answered shows only `Images`.
`summarizeSearch` (pure) sorts the lot's keys into the classes. A name is the
Registry column's label (`entryLabel`: alias, else URL, else key), in form order.
`Partial` lists registries cut at a cap, `No result` those that answered with
nothing (Skipped excluded). A registry that is both partial and empty appears
on both lines, on purpose.

`May omit images` lists the registries that may not have listed every image
they hold, by their role in a Nexus group (DEVDESK-629, F2; it replaces the
`Cached only` line of DEVDESK-308, whose "limited to cached content" reading the
measures of NX-04 contradicted). `browserRegistryEntry.role` is a `nexusRole`
(group, proxy, hosted, unknown), set once by `buildEntries`: a
`provider: nexus` + `kind: group` entry is a group; a discovered member of one
takes `memberRole(RegistryGroupMember.Type)` (`hosted`, `proxy`, `group`; any
other value, case included, is unknown); a declared entry whose `parent` is the
slug of a Nexus group takes the type of the discovered member at its exact
`(URL, RepoPrefix)`, and is unknown when none matches or the cache predates the
type (H5); a group unit (`groupUnit`) is a group. `isNexusGroupOrMember` (the
former `listsCacheOnly`) only drives `catalogUnderPrefix`. The pure rule is
`mayOmit(role, outcome)`: a group whatever it answered (it says nothing about
what a member it could not reach left out), a proxy only when it `failed` or was
`refused`, a hosted repository never, an unknown role never (no mention is better
than a false one), and never a `Skipped` outcome. `summarizeSearch` takes a
`func(string) nexusRole` and reads it by entry key (`entryRole`, not by slug:
members share their group's slug). A group that answered with no result is
marked ` (no result)` on that line and is not on `No result`; the mark does not
mean the image is absent, only that this registry listed nothing and may have
left out what it could not reach, while `No result: quay` means the registry
answered and holds nothing that matches. A `failed` or `refused` mention stays on
its own line too and carries no mark; a partial registry appears on `Partial` and
on `May omit images`. Docker Hub, generic registries and a Nexus outside a group
never get the line. `Skipped` and `May omit images` exclude each other (`Skipped`
means no catalog entry answered), which keeps the budget below. The search sends
no request to the members of a group (F4): the group answers on its own connector,
and a member the group could not reach is invisible to DevDesk; the line says it
may happen, it cannot say it did. The MCP server is unchanged.
The Nexus catalog tests of `internal/imagesearch` (`TestCatalogOnMeasuredNexusAnswers`)
replay answers measured on the bench (Nexus 3.98.1-01, NX-03), not hand-written
ones: the verdicts `ok`, `No result` and `Refused` (prefix absent from a real
catalog) are checked there. `Failed` and the group line are measured since NX-04
(DEVDESK-580, PR3): a proxy's catalog lists names it does not hold
(`mirror/uncached`) while its upstream answers, and a group on its connector answers
`no result` for `mirror` once a member's upstream is cut, which is why `(no result)`
does not mean absent.
See "Recorded fixtures" in [`nexus-bench.md`](nexus-bench.md).

Each name list is fitted by `fitNames` to the room a value has
(`shortcut.HeaderValueWidth`), so the `+N` is never lost: all the names, else
the first k names then `+N`, else `+N` alone. `Skipped` carries the hint
` (type N+ characters)`, which is the first thing dropped when the header is
narrow. The summary has a budget of `shortcut.HeaderRows` (7) lines, `Images`
included; classes after the sixth are left out (with `May omit images` the seven
classes fit only because `Pending`/`Interrupted` and `Skipped`/`May omit images`
exclude each other). Before the first
`WindowSizeMsg` nothing is abbreviated. At level 2 (tags) the header is the
usual tag count.

Limit, accepted (R2): below about 50 columns `HeaderValueWidth` is 0 or 1,
nothing fits, `fitNames` returns `+N` anyway and the router clips it.

Logging, by verdict, through `logSearchOutcome` (the alias falls back to the
entry key):
- `failed`: `ERROR [oci_resources] image search on <alias>: <cause>`;
- `refused`: `INFO [oci_resources] image search on <alias>: unsupported or
  refused: <cause>` (a refusal is expected for dhi.io and for a registry whose catalog
  is disabled, forbidden or not under the entry's prefix, so it does not fill
  the error log);
- `ok` and `no result`: nothing.

The cause is `searchErrorCause(err)`: a `*url.Error` keeps only its Op and Err
(the URL carries the typed text and can carry credentials), then
`jobs.SafeCause`. Nothing else reaches the log. Nothing reaches the screen
either, and no error text reaches `:jobs` (Rule 128).

Level 1 shows `searchTable` (`datatable.Model[searchRow]`, built by
`newSearchTable`, also used by `ActivityTables` as `oci/registry-search`).
Columns: the activity column (`IndicatorColumn`, always `Idle`, `IconDocker` in
the `IconRoleImage` role), Name (flex, searchable), Registry, Stars (empty when
`HasStars` is false), Official (`official` or empty) and Description
(searchable). All cells are plain text (Rule 122). When the terminal narrows,
Description, then Stars, then Official leave whole (`Optional`, with
`DropFirst` on Description and Stars). Stars reserves 7 cells (title plus sort arrow). On Docker Hub rows this makes Description and Stars each leave one cell later than before DEVDESK-309 (Description at 92 columns instead of 91, Stars at 70 instead of 69). Official does not move (61), because Stars is already gone when Official leaves (`TestSearchColumnsLeaveAtTheMeasuredWidths`, `hubRows` fixture). At 60 columns only the activity column,
Name and Registry remain, and the line spans width-2 at every width (Rule 116). Results are
ordered by `sortSearchRows` (`browser_images_order.go`), a pure function of the
rows and the typed name (`RegistryBrowser.searchQuery`, set by
`startImageSearch`), so the order never depends on which registry answered
first. Ranks (`imagesearch.MatchTier`, case-insensitive): 0 name equals the query, 1 last
path segment equals it, 2 last segment starts with it, 3 name contains it, 4 no
match; then name, registry alias, `EntryKey` and `Repo`. Stars play no part in this default order; they serve the
`.` sort below. The
cursor follows the selected row by `EntryKey` + `Repo`, never by index:
`rebuildSearchTable` remembers the selection before `SetItems` and puts the
cursor back with `SetCursor` (all in `Update()`, `datatable` untouched). With
nothing selected (empty table, so also a new search, which starts from
`SetItems(nil)`) the cursor goes to the first row received; a row that
disappears (the cursor's row is no longer listed) sends the cursor to the first row (`GotoTop`). **Sort (DEVDESK-309, IS-05).** `.` calls `cycleSearchSort`, which advances
`datatable.CycleSort`: Name ▲, Name ▼, Registry ▲, Registry ▼, Stars ▼ (Stars
opens descending, `DescFirst`), Stars ▲, then back to the default order above.
Name compares lower-cased, Registry on its lower-cased label (`Less` on each
column); ties keep the default order. `starsKey` gives rows without stars
(`HasStars` false, private registries) one contiguous block, last in ▼ and first
in ▲, apart from an image with zero stars. The cursor follows the selected row
by `EntryKey` + `Repo` (`reselect`, shared with `rebuildSearchTable`). A new
search (`startImageSearch`) resets the sort and clears a confirmed `/` filter.
`.` typed in the open filter bar stays a character of the field.
**Registry filter (DEVDESK-310, IS-06).** `r` calls `cycleSearchRegistryFilter`,
which advances `searchFilter` (a `resultFilter`, apart from the tags' `registryFilter`)
with `nextFilter`: groups, then individual registries, then off. The stops come
from `searchFilterStops` -> `filterStopsFor` (shared with the tags table through
`filterStops`): they follow the form order (`b.searchKeys`) and keep only the
registries that have at least one row. With one registry that produced results
there is no stop, `r` does nothing and its shortcut is greyed. `filterLabel`
(shared with `registryFilterLabel`) names the active stop: the group label; for a registry outside a group, `parent/alias` when it has a parent, else its alias, else `registryRef`. `syncSearchFilterToken` shows it as one active
`components.FilterToken` in the bar, which appears while a filter exists and while `/` is being typed
(the footer grows by 2 lines). The token is display only: `rebuildSearchTable`
prefilters the rows (`searchFilter.matches`) before `SetItems`, and `datatable`
is unchanged. Order of computation: `r` filter, then the `/` query (name or
description, case-insensitive, no network request), then the `.` sort. `r`, `.`
and `:` typed in the open bar are characters of the field. `esc` closes the bar,
then clears the query, then goes back to the form; it does not clear the `r`
filter (cycle to off with `r`). The `r` filter survives `→`/`←` (tags). A new
search clears it, with the sort and the `/` filter. When the row under the
cursor is hidden by `r`, the cursor goes to the top. A filter that leaves no row
leaves a header-only table (Rule 139); `Images` counts the visible rows.
Rule 139: an empty table keeps its
header. The header reads `Images: <visible rows>`, the title carries the typed
name (`... > Registry Browser > <name>`) at level 1 (once `→` opens a row, the
title carries that row's name, see below), and the footer shows the shared
activity dot with `Searching registries...` while `pendingSearches > 0`. Once
the search is done, the footer describes the selected row (`Status.Detail`): its
full reference (`searchRow.ref`, built by `searchRowReference`: host, then
`Repo`; Docker Hub always `docker.io`) and the registry label (`Image <ref> on
<label>`, no ` on` when the label is empty); `Searching registries...` keeps
priority. The keys are in
`ui-components.md`.

**Fix at level 2:** `filterStopsFor` dropped the group stop wrongly when a
foreign registry had as many keys as the group had members; it now counts only
the results inside the group.

**`→` at level 1: open the tags of a row (DEVDESK-304).** `→` opens the usual
tag table for the row under the cursor (`openSelectedImageTags`). It sends one
request, to the row's own registry (`selectedImage` finds the entry by
`EntryKey`), for the row's final repository: `searchRow.img.Repo` is passed as
is, with no prefix and no `library/` added again (`normalizeRepoForRegistry` and
`joinRepoPrefix` run only in `startTagLookup`, the typed-name flow). Both flows
build the request through `newTagRequest` (alias, API address, `credsFor`) and
open the lot through `startTagRequests`, so the tag table, the pull and the scan
references are the same as in the full-name flow. `startTagRequests` also
records where the table came from: `tagsFromSearch` (true for `→`) and
`tagsImage` (the row's `Name`, empty for the full-name flow). `GetTitle` shows
`tagsImage` in the tags state instead of the typed name.

- **No row.** `imageDrillIn` (one computation for the header and the handler,
  Rule 130) reports `reasonNoImage`. `→` changes nothing and warns through
  `RegistryBrowserWarnMsg`: footer Warn `No image selected`. The `→ Open tags`
  shortcut is greyed.
- **Failed listing.** `handleMultiRegistryTagsLoaded`, when `tagsFromSearch` is
  set, leaves the table with its header only (Rule 139), shows `tagsLoadFailedText`
  (`Could not load tags`) as a 3 s footer Error, and logs
  `ERROR [oci_resources] registry tags on <alias>: <cause>` where the cause comes
  from `searchErrorCause`: no credentials, no URL. The full-name flow (opened from
  the form) says the same since DEVDESK-667: `handleMultiRegistryTagsFailed` shows
  `Could not load tags`, plus `: <reason>` when `registryFailureReason` has one,
  whichever way the listing was opened; it keeps its own journal line
  (`registry tags <repo> from <url>`).
- **Search in flight.** `→` while the level 1 search still runs cancels it
  (the browser has one lot at a time, `beginRequests`): the rows already shown
  stay, the late ones are dropped. Unchanged by IS-03, although catalog walks
  make searches slower and `→` will cut them more often: the user reopens the
  search. Keeping the lot alive behind the tags would be a separate story.

**Back to level 1 (DEVDESK-307).** `←` and `esc` on the tag table call
`leaveTags`, which returns no Cmd (so no new request). Typing in the tag filter
comes first: `esc` closes the bar and `←` stays in the text field. `leaveTags`
reads the origin from `tagsFromSearch`:

- **Opened with `→`:** back to level 1 (`browserStateImages`), untouched. The
  rows, the sort order, the cursor and the filter are the ones the user left;
  `searchTable` is neither emptied nor rebuilt (`rebuildSearchTable` would move
  the cursor).
- **Opened by full name:** back to the form (`browserStateInput`), focus on the
  Open Tags button as before.

On both origins `leaveTags` calls `abandonLot`: it cancels the tag lot, takes a
new generation and zeroes `pendingSearches`, so a late tag answer or failure is
dropped by the generation test and cannot show `Could not load tags` over the
form or level 1 (this closes open point 5 of D132). It also resets the tag
filters (`registryFilter`, `filterInput`, `filterActive`) and `tagsImage`, so
the title does not keep a stale `[alias]` or image name. Level 1 itself:
`←` goes back to the form (`backToForm`, which also calls `abandonLot`) and
keeps the typed name and the selected registries, even with a confirmed filter
query. `esc` still clears a confirmed query first. While the filter bar is being
typed in, `esc` closes the bar and `←` stays in the text field. There is no `←`
entry in the shortcuts header: `esc Go back` already shows in both states
(Rule 130, D133).

**Networks, Volumes and Registries (DEVDESK-153, A-09b).** Each takes
`datatable.IndicatorColumn` as column 0. Rows carry no `components.RowActivity`:
the datatable reads its own busy marker (`MarkBusy`), and the column's
`Indicator` function returns `Idle` (or `Running` when `registryRow.refreshing`
is set). A remove, login or logout in flight is that marker, reported as
`Running` with its label (`BusyLabel`). On these three tables a failure never
leaves a `⊗` dot, only a footer message. On Registries, a group whose members are being discovered
(`refreshingGroups`) is `Running` too, and the Members cell is static dim text:
the row's dot says it, and `WantsBlink` is true while a group refreshes or a
row action runs, so the periodic refresh alone never blinks. The row spinner is
gone from the whole view: `advanceBusySpinners`, `busyTick`, `spinnerFrameIdx`
and every `AdvanceSpinner` call are deleted. The view's `spinner.Model` and `loadingRegs` are
deleted (DEVDESK-374): a tab's load, the tag search and the network inspection
show the shared activity dot in the footer (Rule 128), and `footerLoading()`
feeds `WantsBlink` with the same priority order as `formStatus()`. A load shows
only while the active tab's table is empty: `ctrl+r` on a filled table is
silent.

**Images (DEVDESK-146, A-09a).**

- **Column 0 is `datatable.IndicatorColumn`.** At rest it shows `IconDocker` in
  the `theme.IconRoleImage` role; while something runs it shows the state glyph.
  `imageRow.Activity` is stamped from `m.rowActivity` (`activity.go`), which
  calls `components.DescribeRow` on the router snapshot (`m.jobs`), the
  remembered failures (`m.failures`) and the snapshot's context.
- **The key is the raw reference, `jobs.TargetKey(img.Name())`.** There is no
  canonicalization: `nginx` and `docker.io/library/nginx` are two keys. Two
  images named `<none>` share one key, as they already share the scan cache
  entry. Kinds read are scan and pull, whoever started them.
- **Static words, no spinner frame in a cell.** The Scanned cell reads
  `scanning` and the ID cell reads `pulling` while the operation runs
  (`scanRunning`, `pullRunning`). The blink phase is read at render, so a frame
  rebuilds no row.
- **Removal is this view's own observation.** The busy `●`/`○` the table used to
  draw is gone; a removal in flight is reported as `Running` with its label
  (`BusyLabel`) and goes through the same column.
- **The footer describes the selected row** (`status()` in `view.go`), after the
  active tab's load state and beside the action line or the batch progress. A
  failed row's detail is fixed, so the spinner stays; a running or queued
  row's detail replaces it.
- **`failedScans` is removed.** A failure is read from `Failures` in the router
  snapshot and stays on the row until the next attempt.
- **After a successful removal the view calls `jobs.ForgetTarget(name)`**
  (`results.go`), so a deleted image does not keep a stale failure.

**Widths (Rule 116).** The activity column costs 4 cells (2 of width and 2 of
padding). Each optional column now appears 4 columns later (the before and after
thresholds, in terminal columns):

| Column | Before | After |
|---|---|---|
| Update | 48 | 52 |
| Secrets | 57 | 61 |
| C | 63 | 67 |
| H | 69 | 73 |
| M | 75 | 79 |
| L | 81 | 85 |
| Sig | 86 | 90 |
| Disk Usage | 100 | 104 |
| Content Size | 116 | 120 |
| Scanned | 132 | 136 |

At 60 columns the visible columns are `●`, ID, Name and Update. At 70 they are
`●`, ID, Name, Update, Secrets and C. At these widths the columns that give way
are required ones, which Rule 116 allows: a whole column goes, never a
truncated value.

## The Containers table and the activity column — `internal/ui/containers` (DEVDESK-152, A-14)

The `:containers` table is on the activity column (Rule 125; the router side is
in `app-shell.md`).

**Terminal blurred (DEVDESK-210).** The two-second tick keeps re-arming itself
but skips `fetchContainers` and `fetchMetrics` while `Model.unfocused` is set
(from `shared.TerminalFocusMsg`). On the focus edge (`Resume`) `setTerminalFocus`
reads both once, with no new tick, since the chain never stopped.

- **The state comes from docker's `Status` text.** `docker.ParseStatus`
  (`internal/docker/status.go`) reads the `Status` column of `ps` once, into
  `Container.Facts` (`ExitCode`/`HasExitCode`, `Age`/`HasAge`). The exit code is
  exact. The age is a **lower bound**: docker rounds ("2 weeks" is 14 days, "About
  an hour" is 1 hr). The restart count is **not available**. Health suffixes
  and podman's trailing "ago" are tolerated. Text it does not recognise yields
  the zero value: no code, no age. `docker inspect` was rejected (D106).
- **Column 0 is `datatable.IndicatorColumn`** (`columnIndicator`), same shape
  and widths as before. `observedState` (`activity.go`) maps the engine state:
  `running` rests on `IconContainer` (role `IconRoleContainer`, theme key
  `icon_container`, default `ColorText`); `restarting` `◕`; `paused` `◐`;
  `created` and any unknown state `◌`; `exited`/`stopped` with code 0 `⊖`;
  `exited`/`stopped` with code ≠ 0 or **unknown**, and `dead`, `⊗`. Podman's
  `stopped` reads like `exited`; other podman states fall to `◌`.
- **An action in flight shows `●`/`○`** (stop, restart, pause, resume, delete,
  through `startAction`), and it wins over the observed state. The row shows
  its observed state again at the next list. `Model.WantsBlink()` is true
  while a row is busy or while the footer shows the load dot: a refresh or a
  `restarting` row never blinks. The blink phase comes from `jobs.FrameMsg`.
  The first load (no rows yet) shows the shared dot in the footer; `ctrl+r`
  with rows on screen stays silent, so the footer does not flicker on every
  refresh. The view holds no `spinner.Model` (DEVDESK-371).
- **The selected row is red only for `Failed`**, so a clean exit is not red under
  the cursor.
- **Footer.** `status()` shows the load first; otherwise the selected row's
  detail (`selectedDetail`, `observedText`): "Running since 3 hr ago" (no
  glyph; "Running for less than a minute" under a minute), "Restarting — last
  exit code 1", "Paused", "Created, never started", "Exited with code 137 — 5
  min ago" ("— just now" under a minute), "Exited cleanly (code 0)", "Exited
  (exit code unknown)", "Dead — the engine could not stop it cleanly". Any other
  state is named as reported. `progressLine()` (ex `actionLine`) follows:
  "Pruning containers…", "N actions running…", or the single label unless the
  selected row already says it. A failed action stays a footer `Error`.
- **Help (`?`)** shows `indicator.Legend(indicator.ContainersLegend...)`; the busy
  entry reads "an action (stop, restart, pause, resume, delete) is running".
- The state filters `r p s t z` are unchanged.

## Verified pulls — `internal/imagepull` (§3.82)

**Every pull goes through `imagepull.Pull`** — the registry browser's `G`, an
update's `G`, an agent's `image_pull_start`. `TestNoPullBypassesTheSignatureCheck`
(`internal/ui/oci_resources`) walks `internal/` and fails on any other file
naming `docker.PullImageContext`: a second path would be a pull nobody
verified. The engine's pull survives in the OCI view only as the seam
`pullImageContext`, handed to `imagepull.Deps` by `pullDeps` and called nowhere.

The sequence, in `pull.go`:

1. `scan.image_verification: off` pulls the tag, as before §3.82.
2. `~/.devdesk/trust.yaml` is read on every pull. A file that does not load
   **refuses** the pull, naming the error: an unreadable policy is not an empty
   one, and falling back to the built-in rules would drop the user's in silence.
3. The tag is resolved to a digest (`oci.ManifestDigest`, the index digest
   cosign signs). A registry that will not give it is a Failed verdict decided
   by the rule that would have applied — a user rule refuses, anything else
   pulls the tag and warns.
4. `trust.Evaluate` on that digest, with the local image of the same repository
   (`RepoDigests`) for continuity. `Block` is an `imagepull.BlockedError`,
   whose message is the reason, naming the rule.
5. `pull repo@digest`, then `docker.TagImage` back to the name asked for.
   Pulling the tag instead would let it move between the check and the pull —
   the attack. Measured on Docker and Podman; under Docker's containerd store
   `repo@sha256:…` also shows in `RepoTags`, `docker images` lists one row.

What the views do with it: a refusal is a footer **Error** naming the rule, and
the `:jobs` detail an agent reads is the same sentence; a Warn goes ahead and
says why after the pull. The Images tab header shows `Signatures: off`, or
`trust.yaml invalid` — read once when the view starts (`checkTrustPolicyCmd`).

**The `Sig` column** of the Images tab (`signatures.go`, the shared
`internal/ui/sigcol`) is each local image's verdict on the content on disk:
`imagepull.LocalDigest` picks the `RepoDigests` entry of the image's own
repository, and `imagepull.Check` asks about that digest against the rules
alone — continuity would compare the image with itself. An image with no such
digest was built or loaded here (a hammer, as in Update). Checked in the
background on `ImagesListMsg`, four at a time; `sigAsked` keeps the digest each
image was asked about, so an unchanged list asks nothing, a pull that changes
the digest asks again, a verdict about a former digest is dropped, and a Failed
one is retried after 30 minutes. An image no rule covers is decided without
cosign, and one DevDesk pulled comes from the verdict cache. Nothing acts on
the column.

**`run` never pulls**: `buildLaunchArgs` and `VerifyEntrypoint` pass
`--pull=never` (a missing image exits 125, both engines). An image reaches the
machine through a verified pull or not at all. DevDesk's own tool images —
Trivy, cosign, the netdiag image — are pulled by their `run` and are **not**
verified: a declared exception.

## Network Diagnostics View

`internal/ui/netdiag/` — four tabs (Diagnostics, Ports, Interfaces, Forward; the Path tab went in DEVDESK-471, the trace lives under Diagnostics):
- **Diagnostics tab** (`model.go`): target, port and resolver, then
  `internal/netcheck`'s pipeline — resolve, route, reach, proxy, connect, TLS, HTTP —
  chained one stage per message so the footer can name the question being asked.
  The seven tool checkboxes are gone (§3.33): the checks follow from the target
  and from what has already failed.
  **The route stage** (`netcheck/stage_route.go`, DEVDESK-474, D151) sorts each
  lookup error with `nettrace.IsNoRoute`: a proven absence of route is a finding,
  any other failure proves nothing (D20). Only the first 8 resolved addresses
  (`maxRoutedAddresses`) are looked up. The verdict:
  - every address proven without a route: **Fail** (`<host> has no local route`);
  - none routed and at least one undetermined: **Unknown**;
  - some routed and at least one proven without a route: **Warn**,
    `partial-route`; only the proven addresses are counted, and undetermined ones
    are added as `route undetermined for N`;
  - some routed, none proven, at least one undetermined: **OK**,
    `route-undetermined`.

  The system's error text (in the machine's language) appears only in the facts
  `No route` and `Route undetermined`, never in the summary (Rule 129). In the
  Fail case the results header and the MCP verdict read FAIL, where they read
  UNKNOWN before.

  The stage also keeps its readings on the check (DEVDESK-473): `Check.Routes`
  holds one `RouteLookup{IP, Hop, Err}` per address looked up, in the order
  asked, with the lookup's own error untouched (so `nettrace.IsNoRoute` still
  applies to it), and `Results.RouteLookups()` returns a copy of them, nil when
  the stage did not run (skipped or cancelled placeholders carry none). It is
  data for a later reader, so the path trace can reuse the reading instead of
  asking the routing table again; it is never shown and never serialized (the
  MCP tool maps a `Check` to its own DTO). Verdict, summary, reason and facts
  are unchanged. The path trace reuses these readings; see "The route is read
  once" under the trace sub-level.

  Real coverage is Linux and WSL2 only. `go-netroute` returns a
  `windows.Errno` that `IsNoRoute` does not recognize on Windows, and an error
  with no errno on macOS: both end as `undetermined`, which is the safe side.
  Two limits to know: if `go-netroute` stopped wrapping the netlink errno with
  `%w`, Linux would fall back to `undetermined` silently, with no failing
  build; and `prohibit` routes (`EACCES`) stay `undetermined`. Windows and macOS
  support is a follow-up story, to be created (see D151 in `docs/backlog.md`).
- **Ports tab** (`ports_model.go`): the machine's own socket table, re-read
  every `ports_refresh_interval`, filtered by protocol, state and text. `K`
  terminates a process, after a confirmation. It refuses DevDesk itself, its
  direct parent, PID 1 and, on Windows, PIDs 0 and 4, and it acts on the
  process the confirmation named, captured when it opened (DEVDESK-708,
  DEVDESK-714), and it checks at the signal that the PID still belongs to the
  process that was listed; `K` is greyed on a row whose identity could not be
  read (DEVDESK-709). It runs **no container** — see `internal/ports` below. Column 0 is the activity column (DEVDESK-150, below).
- **Interfaces tab** (`interfaces_model.go`): this machine's network interfaces
  — name, state, MTU, MAC, RX/TX error counters, addresses — in a `datatable`.
  It runs **no container**; see `internal/netiface` below. It was the Topology
  tab, and §3.44 is why three of its four sections are gone rather than
  translated.
- **Forward tab** (`forward_model.go`, `forward_form.go`): the port
  redirections, `N` to open one, `space` to pause or resume it and `K` to delete
  it. It holds **no listener** — see `internal/forward` below.
- **Trace sub-level of Diagnostics** (`path_model.go`, `path_view.go`,
  `path_run.go`, `path_target.go`, `path_from_route.go`, `path_steps.go`,
  `trace_form.go`, `trace_level.go`; DEVDESK-328, DEVDESK-329, DEVDESK-470,
  DEVDESK-471): the hop-by-hop path to a target, from `internal/nettrace`
  (below). It was the fifth tab; it is now `StateTrace` of the Diagnostics tab
  (`tabCount = 4`), described under "The Trace Path button, `H` and the trace
  sub-level". It runs **no container**; since DEVDESK-330 it can name a running
  Docker Engine container as the source of the column (below), but the probes
  still leave from this machine. Three states: `pathReady` (target known,
  nothing sent), `pathRunning` (hops arriving) and `pathDone` (hops kept on
  screen). The **body is always the hop table** (`PathModel.view()` returns
  `table.View()`, Rule 139), in every state: columns Hop, Step, Address, RTT,
  with a silent hop dim and never red. **The column starts on this machine**
  (DEVDESK-329): three local rows, then the hops. `source` reads `this machine`;
  `interface` reads `<name> (<source address>)`; `gateway` reads the gateway
  address. The local rows have an empty Hop and RTT (they asked nobody; `-`
  means "no answer") and the default text colour. `pathSteps` (`path_steps.go`)
  is the only place that orders the rows; the table item is a `pathStep` (kind
  source, interface, gateway or hop; DEVDESK-330 adds container, network and
  trace, below).
  Nothing is shown before the route is known, so a resolution error leaves 0
  rows. There is **no target field**: the target is the Diagnostics form's (or
  the results' host for `H`), typed once. The sub-level's footer is 3 lines in
  every state (tab row, blank line, info line) and keeps its own
  `FooterMessage`, returned by `activeFooter`, so `WantsBlink()` follows a
  running trace. `Model.Running()` is true while a trace runs, so the router
  does not rebuild the view mid-trace.

  Keys: see "The Trace Path button, `H` and the trace sub-level" below (`enter`
  starts or traces again, `esc` cancels then goes back, `ctrl+o` opens *Trace
  from*, `tab` switches tab, the navigation keys move in the table). `ctrl+o`
  while a trace runs is ignored; where the trace is unsupported it is greyed and
  warns with the platform reason (`sourceChoosable`, the one answer the header
  and the handler both read). While the selector is open it holds every key:
  `up`, `down`, `pgup`, `pgdown`, `home` and `end` move, `enter` chooses, `esc`
  closes, and every other key is swallowed, `tab` and `shift+tab` included
  (`netdiag.Model.Update` hands the key to the Path model before the tab
  switch). A paste is dropped. `Model.InEditMode()` is true while it is open,
  so `q` and `:` reach the view and do not quit or open the palette.

**Ports tab, terminal blurred (DEVDESK-210).** `PortsModel.unfocused` is set by
`shared.TerminalFocusMsg`, which `netdiag.Model.Update` forwards to the ports
poll whichever tab shows (the tick runs on every tab). The tick chain is kept:
`handleTick` re-arms `portsTickCmd` and skips `fetchPortsCmd` while `unfocused`.
On the focus edge (`Resume`) it reads once, with no new tick, so the chain is
not doubled. `unfocused` is separate from `paused`, the user's own pause (shown
in the bar): a user pause is never lifted by a focus, and the resume read is
skipped while `paused`.

**Ports tab, activity column (DEVDESK-150, Rule 125).** Column 0 of the socket
table is `datatable.IndicatorColumn` (untitled, two cells wide), the same
mechanism as `oci/images` (DEVDESK-146). The table has no registry run: `K` is
held in `datatable`'s `busy` map, keyed by PID, and `datatable` writes the
precedence "busy means `indicator.Running`" once, so the column's own state is
always `Idle` and an idle row shows an empty cell (a socket has no rest icon).

- **`K` running = lit dot.** Every socket of the PID shows `●`/`○`, as the
  spinner did before. The blink phase comes from `jobs.FrameMsg`, handled in
  `Model.Update` (`SetBlinkPhase`, nothing rebuilt); `Model.WantsBlink()` is true
  while any row is busy, whatever the active tab. The Diagnostics and Interfaces
  footers show the same shared dot while a run or an interfaces read is in
  flight (DEVDESK-373), driven by the same `FrameMsg`: `WantsBlink()` is also
  true while the active tab's footer is on `Activity`. The `netdiag` package
  holds no `spinner.Model` and no `spinner.TickMsg` handler.
- **The line spinner is gone**: `StatusColumn`, `portsSpinnerTickMsg`,
  `portsSpinnerCmd` and `handleSpinnerTick` are deleted. **State keeps its
  text** (`LISTEN`), dimmed like every busy row. This reverses the letter of
  §3.22 (the State cell used to be replaced by the spinner) and keeps its intent:
  the dim says the value is about to change.
- **The footer names the step**: for the selected busy row, `● Stopping sshd (812)`,
  `● Waiting for sshd (812) to exit` or `● Killing sshd (812)` (the last one is
  also the Windows text) (`PortsModel.rowDetail`, from `components.DescribeRow`),
  before the paused or stale text. At 60 columns, where PID and Process are dropped, this is
  where the process name is readable.
- **Failure stays in the footer only.** A refused or failed kill is an `Error`
  (`killFailureMessage`) and the row returns to rest: no `⊗`, nothing is
  remembered, because no registry kind exists for it. On Windows `K` stays lit
  on a busy row (Rule 130); pressing it again warns `Already killing PID N`. On
  Linux and macOS `K` is greyed on every row while a two-step stop is in
  progress: an exception, see « Two-step stop » under « The socket table ».
- **A periodic refresh never blinks**: `SetItems` leaves `busy` alone, so a
  refresh with nothing busy leaves column 0 empty and `WantsBlink()` false; one
  during a kill keeps the dots and the cursor.
- **A failed network test is not a row operation.** The Diagnostics table keeps
  its result in the Verdict column; no `⊗` was added (open question 4 of the
  activity-indicator stories, option A). The Diagnostics and Interfaces tables
  have no `Indicator`; neither does the trace's hop table (DEVDESK-328): a
  trace is one run, not a per-row operation, and its running state is the
  footer's shared dot. The Path files `path_model.go`, `path_target.go` and
  `path_view.go` (and, since DEVDESK-329, `path_steps.go`; since DEVDESK-330,
  `path_source.go`, `path_container.go`, `path_source_picker.go` and
  `path_source_flow.go`) are in the activity guard's scanned sources for
  `command.ViewNetdiag`; `path_run.go`, which holds the I/O, is not.
- **Files.** `ports_activity.go` (`ActivityTables()` reports `netdiag/ports`,
  `WantsBlink()`); `ports_commands.go` holds the I/O commands (`fetchPortsCmd`,
  `portsTickCmd`, `terminateCmd`, `stopProbeCmd`, `killProcessCmd(kill, target,
  gen)`, `requestPortsRefresh`, whose
  `portsRefreshMsg` makes `Update` read the table once, outside the tick chain),
  `processOps` (`protection`, `confirm`, `terminate`, `probe`, `kill`,
  `windows`) and
  `realProcessOps` (see « Seams and the signal guard » below), and `NewPortsDataMsg`, which the activity
  guard's footer seed uses to hand rows to the view without reading the socket
  table. `ports_commands.go` is not scanned by the guard (its refresh tick is
  data, not a row animation).
- **Widths (Rule 116).** Required columns give way rightmost first. Thresholds,
  as terminal width, with the column added (they were 99, 87, 78 and 50 before):

  | Columns shown | From width |
  |---|---|
  | all | 103 |
  | without Process | 91 |
  | without PID and Process | 82 |
  | activity, Proto, State, Local Address | 54 |

  At 60 columns the last set shows and `RenderedWidth()` is 58.

**Timing (§3.66).** `netcheck.Check` carries a `Duration time.Duration`
alongside `Summary`/`Facts` — zero means untimed. `stage_connect.go` and
`stage_tls.go` fill it from the dial and the handshake respectively;
`stage_http.go` fills it from an `httptrace.ClientTrace` attached in
`env.go`'s `Head`, which also gives `HTTPResult` a DNS/connect/TLS/TTFB
breakdown surfaced as `Facts` on the `HTTP` row. Nothing keeps a history
across runs — that stays an open question.

That breakdown also drives a waterfall: `Check.Phases` is a set of named,
non-overlapping spans that sum exactly to `Duration` (nil on checks with
nothing to break down — TCP, TLS). `stage_http.go`'s `httpPhases` derives
**Wait** and **Content** rather than charting TTFB directly, because TTFB is
measured from the start of the request and already contains DNS+connect+TLS —
charting it as a fifth independent share would double-count that overlap.
`internal/ui/netdiag/view.go`'s `phaseLines` renders one proportional bar per
phase, reusing the load-gauge machinery (`theme.GaugeFill`, `GaugeTrack`,
`GaugeFillWidth`, `GaugeTrackStyle`) with a new neutral fill colour, `theme.TimingFillStyle` —
a phase taking most of the time is a fact to read, not a severity to spot.

**Opened prefilled, from elsewhere (§3.66).** `netdiag.NewWithTarget` builds a
view with the form filled in but not yet run; `netdiag.OpenRequestMsg` is
what a producer elsewhere sends to ask the router for one (mirroring
`uiviewer.OpenRequestMsg`), and `netdiag.BackToOriginMsg` is how `esc` gets
back out, via `Model.OriginView` — empty when netdiag was opened directly
from the command line, in which case `esc` behaves exactly as it always has.
`status`'s `H` (`keymap.Diagnose`, `internal/ui/status/diagnostics.go`) is
the first producer: it resolves the selected monitor's target and
auto-runs the pipeline when the port is certain (http/https/ssl), or lands on
the prefilled form when it is a guess (icmp/dns).

`H` has a **second producer, inside netdiag** (DEVDESK-328, DEVDESK-471,
`path_from_route.go`): on the Diagnostics results, with the **Local route** row
(`netcheck.CheckRoute`) selected, it opens the trace sub-level (`StateTrace`) on
the results' host, in its **ready** state (`pathReady`: target known, nothing
sent). It **never launches**: nothing is resolved or sent until `enter` in the
sub-level. Only the host is carried over, not the port (the route check does not
depend on one). `pathFromRoute()` is the one availability read by the header
(the entry `H Trace the path` is greyed, Rule 130) and by the handler, which
refuses with the same reason at the footer. The reasons come in this order: the
platform cannot trace (`traceable()`), the selected row is not the route row
(`Select the Local route row to trace its path`), the target is one the trace
refuses (`parsePathTarget`, for example an IPv6 literal). If a trace is already
running, `H` changes nothing and warns `reasonPathBusy`.

**The Trace Path button, `H` and the trace sub-level (DEVDESK-470,
DEVDESK-471; `trace_form.go`, `trace_level.go`).** Both entry points call
`Model.openTrace(origin, target)`: `traceOrigin` records where `esc` leads back
to (`StateInput` for the button, `StateResults` for `H`), the state becomes
`StateTrace` on the Diagnostics tab, and `PathModel.ready(target)` puts the
shared `m.pathModel` in `pathReady` (source reset to this machine, previous run
forgotten). The button then launches at once (`PathModel.startFromForm`: `ready`,
then `launch`; the Cmd does the I/O, `Update` mutates, Rule 110); `H` does not. A
trace already running is refused with `reasonPathBusy` at the footer, and the
button stays lit (Rule 130). The body is `pathModel.view()`, the table alone
(Rule 139); the title reads `Network › Path › <host>` (host cut with an
ellipsis), the header is `pathModel.headerInfo()`, and the footer is 3 lines in
every state: the Diagnostics filter bar is not drawn in the sub-level
(`GetFooterHeight`, `RenderFooter`, Rule 124). The footer message is the Path
model's (`activeFooter`). Hops keep landing while another tab is on screen, and
coming back finds the sub-level.

**The route is read once (DEVDESK-473, D155; `path_known_route.go`).**
Diagnostics keeps its route lookups on the `Local route` check (`Check.Routes`);
the trace's interface and gateway rows answer from them instead of asking the
routing table a second time. `Model.openTrace` is the only writer of
`PathModel.known`: it assigns `knownRoutesOf(m.resultsHost, m.results)`, a copy
of the lookups (never shared with `m.results`), on every opening, empty when
the form opens the trace (no results). `Model.resultsHost` is the host of the
run behind the results: `StartRun` sets it (so `ctrl+r` too), `resetToForm`
clears it with the results. `PathModel.launch` builds the run from
`pm.runEngine(tg)`, the engine whose `route` function is the
`pathKnownRoutes.routeFirst` wrapper; the `Cmd` receives only values and never
reads `pm` (Rule 110). A reading is reused only when the snapshot's host equals
the target's host, the traced address (compared after `Unmap`) is one the route
stage looked up, the lookup did not end on `context.Canceled` or
`context.DeadlineExceeded` (it says nothing about the table), and the source is
this machine (a container source reads its own route on every launch). The
lookup's own error is returned untouched, so `no route` and `undetermined`
come from the same `nettrace.IsNoRoute` on both sides. *Trace again* and a
return to the results then `H` reuse the same reading; `ctrl+r`, a new run,
and a trip back to the form (`esc`, `N`) read again, because the results the
reading belonged to are gone. The `Local route` row itself is unchanged.

*Which address (D155 point 5, refining D144).* The trace still resolves the name
itself (`LookupNetIP(ctx, "ip4", host)`) and traces an address that resolution
returned now, never the Diagnostics one and never one from the DNS Server field.
When the source is this machine, `runEngine` also wraps `resolve`:
`pathKnownRoutes.prefer` moves the first IPv4 address of that answer which the
snapshot holds a *successful* reading of (same host test, `Unmap`, and a lookup
with `Err == nil`) to the front, the other addresses keeping their order, and
`resolvePathTarget` takes `addrs[0]` as before. A lookup that ended in an error
(`no route`, `undetermined`, a cancellation, a deadline, any other) is never
preferred (user decision, confirmed 2026-11-16). So on a round-robin name the
trace takes an address Diagnostics already read with success, whatever order the
resolver gave this time. With no results, for another host, from a container, or
with no preferable address in common, the answer is untouched and the first IPv4
address is traced (that one costs one more reading, unless it is the only
address: its failed reading is then reused as it is). IPv6 addresses are never
preferred. The reordering runs inside the `Cmd` on a copy of the snapshot taken
at launch (Rule 110).

`PathModel` has **no target field** (DEVDESK-471): `pathReady` is its zero value,
a fresh model is ready and inert, and `ready(target)` is the only way a target
gets in. Letters, a paste and `esc` do nothing in it; `enter` with no target
launches nothing.

Keys (`handleKeyTrace`: `esc` is the sub-level's, the rest goes to
`PathModel.handleKey`, which ignores letters; while a trace runs only the
table's navigation passes): `enter` launches from the ready state and launches
again after a finished or canceled trace (*Trace again*, replacing D152 point
2); `ctrl+o` opens *Trace from* (declared exception, surface `netdiag/trace`);
choosing a source goes back to `pathReady` with the target kept and sends
nothing; `esc` cancels a running trace, then goes back to `traceOrigin` with
fields, focus, filter and results untouched (no `resetToForm`); navigation moves
the table. An open selector holds every key, `tab` included. `tab` and
`shift+tab` otherwise still switch tabs, so the sub-level survives a visit to
another tab. `traceShortcuts()` is the one constructor of the sub-level's
shortcuts: ready and done share one key set (Rule 130), the selector and a
running trace replace the list. `InEditMode()` is true in the sub-level only
while the selector is open.

The Path tab is gone (DEVDESK-471, P2-02): `tabPath` no longer exists and the
tab bar has four labels.

**Files.** `trace_form.go` holds what the form does: `traceable()` (the one
availability, read by the button, the header and the handler, Rule 130),
`traceFromForm()` (the `enter` handler), the target validation and
`traceUnusedNotice`. `trace_level.go` holds the sub-level: `startFromForm`,
`ready`, `openTrace`, `inTrace`, `handleKeyTrace`, `escFromTrace` (with
`restoreFormFocus`), `traceTitle` and `traceShortcuts`.

**Order of refusals in `traceFromForm`.** An unsupported platform
(`launchable()`, macOS, Windows, WSL1) comes first: the button is greyed and
the reason is in the footer. Then a trace already running (`reasonPathBusy`).
Then the target.

**Target validation (Rule 140).** The target goes through `parsePathTarget`,
the trace's own parser, so an empty, invalid, `host:port` or IPv6 value gets
the trace's sentence (first letter capitalized). The
failure is carried twice: the Target field is underlined
(`traceMarks`, `InvalidMarks.Sync`, from `Update()` only) and the error sits
in the footer at level Error, derived from `traceValidation.Status()` in
`statusLine()`. Nothing is invalid before the first refused `enter` on the
button; after that, every edit of the Target revalidates (`revalidateTrace`).
`enter` on a text field still runs the diagnostics and does not validate
the trace target.

**Port and DNS Server.** The trace probes the path to a host, so both fields
are ignored. When either holds a non-blank value, `traceUnusedNotice` posts an
Info notice on the Path model's footer (`Port is not used by the path trace`,
`DNS Server is not used by the path trace`, or both). It is posted after
`startFromForm`, because launching clears that footer. The values stay in the
form.

**One model, one place.** The sub-level has no model of its own: it drives
`m.pathModel`, the only trace model now that the Path tab is gone. Starting from
the form or from `H` overwrites its target (`ready`) and forgets its previous
run; going back to the form or the results does not clear it.

**What is configurable, and what is not.** `internal/netcheck` held its timeouts
as constants with a comment saying they would become settings when somebody
asked; `network:` is what they became.

| Setting | Replaces |
|---|---|
| `check_timeout` | five per-stage constants — 5 s for DNS and the dial, 8 s for TLS and HTTP, 4 s for the ping |
| `ping_count` | `pingCount` |
| `cert_expiry_warn_days` | `expiryWarnWindow` |
| `ports_refresh_interval` | the 2 s tick |

**One timeout replaces five**, and the default is the old *maximum* so nothing
that answers today starts failing. The 5 / 5 / 8 / 8 / 4 split was never argued
anywhere — five rows for one idea, and each of the five a separate guess. The
cost is stated rather than discovered: an unreachable host now spends 8 s on DNS
instead of 5, which the staged progress line makes legible rather than a hang.

`traceroute_max_hops` was a sixth setting and went with the trace it bounded
(§3.47). It stays removed: the trace is back in the process (DEVDESK-327, see
[The path trace](#the-path-trace--internalnettrace)), but `nettrace.Options.MaxHops`
is not wired to any setting, and the trace (DEVDESK-328) still runs with
the engine's defaults (`nettrace.Options{}`).

Two things stay hardcoded, each for a reason:

- **`MinVersion: VersionTLS10`** — the handshake is *probing*, not securing.
  Reporting an old version is the point; a setting could only make the tool
  blind to what it exists to find.
- **`InsecureSkipVerify`** — the TLS stage verifies the chain itself so it can
  say *which* part failed. A setting here would collapse four checks into one
  error string.

`netcheck.Settings` travels **beside** `Env`, not on it: `Env` is the seam to
the network, while `PingCount` and `ExpiryWarnWindow` are read by stages and
never by a network call — putting a preference behind the seam would make every
fake answer for one. `Settings.Normalized()` fills in anything non-positive at
every entry point, so a hand-edited `0` becomes the default rather than a dial
with no deadline. The view calls `checkSettings()` per run rather than capturing
at construction, so a run already in flight and the config cannot disagree
halfway down the pipeline.

Every probe here answers from the DevDesk process. The route trace was the one
exception and §3.47 removed it: `--network host` on Docker Desktop is the VM's
namespace, so it traced a path from somewhere else. It is back, in the process,
since DEVDESK-327 (`internal/nettrace`, below). Nothing shells out.

**The `route` stage answers the question no other check can** (§3.44): when a
VPN captures the default route, a host that is plainly reachable elsewhere is
unreachable here, and every other row reports the *symptom* — no ICMP reply, no
TCP connect — while none reports the cause. `Env.Route(ctx, ip)` returns a
`RouteHop{Interface, Source, Gateway}`, and the summary names the **interface**
because that is what the user recognises: "Traffic leaves through ProtonVPN"
answers the question, `10.2.0.1` needs a second lookup to mean anything.

`github.com/libp2p/go-netroute` is what makes it one implementation instead of
three — `GetBestRoute2` on Windows, an `RTM_GETROUTE` netlink query on Linux, the
routing socket on the BSDs — with no privilege anywhere and ~2 ms per lookup,
measured. It depends only on `x/net` and `x/sys`, both already in the graph.

Four decisions, each with a test:

- **It gates nothing**, for `reach`'s reason: a machine whose routing table
  cannot be read still has a perfectly answerable question about the port.
- **A lookup that fails is `Unknown`, never `Fail`.** "Could not determine the
  route" is not "there is no route", and rendering the first as the second is
  D20. The platform error is also **localised** — Windows returns
  `ERROR_NETWORK_UNREACHABLE` in the machine's own language — so it goes in a
  fact and never in the summary (Rule 129).
- **One family without a route is a `Warn` with its count.** That is the IPv6
  case, and it is worth reporting: a client that prefers IPv6 hangs before
  falling back.
- **It takes its addresses from the `resolve` check's facts**, capped at
  `maxRoutedAddresses`, rather than resolving again — on a round-robin name the
  two could diverge and the route would describe an address no other check in the
  run ever touched. `TestTheRouteStageReadsWhatTheResolveStageWrote` pins the
  coupling, which is otherwise the kind that breaks in silence.

**The direct path and the Proxy row (DEVDESK-582).** The Diagnostics measure the
direct path from this machine, never the one through a proxy (decision Q1). The
HEAD (`systemEnv.Head`) builds its own `http.Transport` with no `Proxy` field, so
it goes direct. `Head` and `Handshake` are the two entries listed as permanent
exceptions of the proxy guard (below). Nothing is ever sent to a proxy.

What the checks do not measure, the Proxy row reports. `StageProxy` sits between
ICMP echo and TCP connect (check `proxy`, titled `Proxy`). It has no dependency, so
the row is present even when DNS fails, which is the usual case behind a mandatory
proxy. It reads the environment through `Env.ProxyFor`, which is
`netproxy.Describe`, and asks about both schemes, because the HEAD's scheme depends
on a handshake that runs later: `https://host:port` (`HTTPS_PROXY`) and
`http://host:port` (`HTTP_PROXY`). The facts `HTTPS` and `HTTP` give each answer. The
two answers merge by priority: Invalid, then Proxied, then Excluded, then Loopback,
then None.

| Merged answer | Verdict | Reason | Summary |
|---|---|---|---|
| no variable for either scheme | N/A | `proxy-none` | `None` |
| a proxy for at least one scheme | INFO | `proxy-configured` | `Configured for this target: http://proxy.corp:3128`, with `(https only)` or `(http only)`, or both proxies when they differ |
| `NO_PROXY` names the target | N/A | `proxy-excluded` | `Excluded by NO_PROXY` |
| a loopback target | N/A | `proxy-loopback` | `Not used for a loopback target` |
| a value that is not a usable proxy URL | WARN | `proxy-invalid` | `Invalid HTTPS_PROXY value` (the variable's name, in the case it was read in) |
| both values invalid | WARN | `proxy-invalid` | `Invalid HTTPS_PROXY and HTTP_PROXY values` (`invalidSummary`) |

The `None` row is always there, by choice: a stage must produce the same checks
whatever it finds (`TestEveryStageProducesWhatItDeclares`), the package tells "not
looked" from "found nothing", and the MCP output keeps a stable list.

`INFO` is a new verdict: an observation with no objective to meet. `Summarize`
and the overall verdict ignore it, as they ignore N/A. The `p` filter (problems
only) hides it. N/A rows are greyed.

When the direct dial of the TCP connect fails and at least one scheme is Proxied,
the conclusion changes (Reason `direct-failed-proxy-configured`): `Port 443 does
not accept direct connections; a proxy is configured for this target`, with the
fact `Proxy`. A closed direct path is expected behind a mandatory proxy, and "does
not accept connections" would mislead. `runConnect` reads `Check.Proxy` of the
Proxy row, never its Summary.

**DNS behind a mandatory proxy (DEVDESK-596).** When the direct resolution of a
named target fails (the DNS row is Fail) and the Proxy row says a proxy applies to
at least one scheme, the DNS row reads `<host> does not resolve directly; a proxy is
configured for this target`. The verdict stays Fail and the Reason is
`direct-resolve-failed-proxy-configured`. The facts are `Resolver`, `Proxy` (the
masked `scheme://host:port` of `netproxy.Display`) and `Error`, in that order. "Does
not resolve" and "resolves to no address" share the Summary; the `Error` fact tells
them apart. Without an applicable proxy (no variable, `NO_PROXY`, loopback, invalid
value) the row is unchanged. When the resolver itself misbehaves (`server
misbehaving`), the Reason, the Summary and the guidance stay as they were, because
the resolver is at fault; only a `Proxy` fact is added after `Resolver`.

The Proxy stage keeps its position (resolve, route, icmp, proxy, tcp, tls, http), so
`Steps()`, the row order and the MCP output do not change. Instead, after the Proxy
stage, the `stage.conclude`/`amends` hook calls `concludeResolveBehindProxy`, which
rewrites the DNS row in place. It reads `Check.Proxy`, the field the TCP conclusion
reads, never the environment and never a Summary. The `prior` slice of `RunStep` is
never modified: the rewrite builds a new slice of facts. While a run is in progress,
the DNS row briefly shows the generic conclusion between the end of the DNS phase
and the end of the Proxy stage; the gap is instantaneous. Over MCP, the `resolve`
check carries the new Reason in the existing `reason` field, with no new field.

The Explain guidance says what it means and what to do. Means: every check below
needs an address, so none of them ran; clients that go through the configured proxy
usually hand it the name and let it resolve it, so behind a mandatory proxy a name
this machine cannot resolve is expected and says nothing about whether the target
exists or is up. Do: check from a client that uses the proxy (for example an HTTPS
monitor in the Status view), or add the target to `NO_PROXY` if it must be reached
directly, then check the spelling or try another resolver.

A proxy leaves `netproxy` only as `netproxy.Display`: `scheme://host:port`, never
userinfo, path, query or IPv6 zone. An invalid value is named by its variable and
never quoted. `Check.Proxy` carries that string to the connect stage and to the
DNS conclusion (above), is never shown on its own and is never serialized, so
`net_check` over MCP does not carry that field. The masked proxy
(`scheme://host:port`) still goes out through the facts `HTTPS`, `HTTP` and
`Proxy`, in the UI and over MCP.

Limits:

- **Validity is stricter than Go's.** Go ignores a value it cannot parse, so
  `FromEnvironment` cannot tell it from "no proxy". `netproxy` also refuses a value
  with an explicit scheme that fails to parse, an empty or non-printable host, and
  a mis-encoded credential (an unescaped `/`, `?` or `#` before the `@`). Go
  accepts some of these: `http://[::1` is read as `http://http:80`. The row says
  Invalid where Go would silently go on.
- **Excluded and Loopback are deduced.** `Describe` does not recompute
  `NO_PROXY`. A nil answer with a valid variable means Loopback if the host is
  `localhost` or a loopback address, otherwise Excluded. The subprocess test of
  `netproxy` pins this against `http.ProxyFromEnvironment`.
- **Cache against variable.** `http.ProxyFromEnvironment` reads the environment
  once per process and caches it. `Describe` reads the variable's name and
  validity directly. A variable changed after the first lookup can therefore show
  a name the cached answer does not use.
- **CGI.** When `REQUEST_METHOD` is set, Go refuses `HTTP_PROXY` (`httpproxy`: `refusing to use HTTP_PROXY value in CGI environment`), so the row says
  `Invalid HTTP_PROXY value`.

## The proxy entry point — `internal/netproxy` (DEVDESK-581)

### Why it exists

The HTTPS monitor followed `HTTPS_PROXY` and `NO_PROXY` while the SSL monitor
dialed the target directly with `tls.DialWithDialer`: behind a corporate proxy
the HTTP check was green and the SSL check red for the same host. The fix is not
a patch on the SSL check but one place that decides the path of a connection, and
a test that keeps the rest of the module from deciding it elsewhere.

### The entry point

- `netproxy.Selector` (`func(*url.URL) (*url.URL, error)`) says which proxy
  carries a connection to a URL; `nil, nil` is direct. A nil `Selector` behaves
  like `FromEnvironment`. `Selector.ForRequest` adapts it to
  `http.Transport.Proxy`.
- `netproxy.FromEnvironment` is `http.ProxyFromEnvironment` on the URL: the same
  function, and so the same cache, as the HTTPS monitor.
- `netproxy.NewTransport(sel)` is `http.DefaultTransport.Clone()` with `Proxy`
  set from `sel`. It is the only way to build a transport that needs a custom
  selection (`oci.newBlobTransport` wraps its selector to learn which proxy
  addresses it may dial).
- `netproxy.Dialer.DialTLS(ctx, hostport, cfg)` opens a TLS connection the way
  an https request to `hostport` would travel: direct, or by `CONNECT` through
  an `http://` or `https://` proxy (Basic credentials from the proxy URL), then
  the handshake with `cfg`. One deadline, the context's, covers proxy dial,
  `CONNECT` and handshake. The zero `Dialer` follows the environment; its
  fields (`Proxy`, `DialContext`, `ProxyTLSConfig`) are what tests swap.
- `netproxy.Describe(u)` says what the proxy variables mean for one URL, as a
  `Decision`: a `Kind` (`DecisionNone`, the zero value, `DecisionProxied`,
  `DecisionExcluded`, `DecisionLoopback`, `DecisionInvalid`), the `Variable` read
  (`HTTPS_PROXY` or `https_proxy`) and, when proxied, `Proxy`. The answer comes from
  `FromEnvironment`; only the variable's name and validity are read apart. A
  `Decision` carries no credential. The Proxy row of the Diagnostics uses it (see
  above).
- `netproxy.Display(proxy)` renders a proxy URL as `scheme://host:port` and nothing
  else: no userinfo, path, query, fragment or IPv6 zone. It is the only way a proxy
  leaves the package for the screen.
- A failure of the proxy step is a `*netproxy.Error` with a `Step`:
  `StepSelect` (invalid proxy setting), `StepScheme` (socks5), `StepDial`,
  `StepProxyTLS`, `StepConnect` (refused, unreadable or unanswered), plus the
  proxy's `host:port` (never the userinfo) and the `CONNECT` status code. Its
  text is built from the step and the code, never from the proxy's own words. A
  failure of the target itself (handshake, certificate) comes back as it would
  without a proxy.

### The SSL monitor

`status.FetchCertificateChainContext` goes through `Dialer.DialTLS`
(`certDialer`, swapped by tests), so the SSL check, the certificate viewer and
the HTTPS monitor take the same path. When the failure is a `*netproxy.Error`,
`SSLChecker.Check` sets `ComponentStatus.ErrorStage = status.StageProxy` next to
`Error`; the field is internal (the MCP output only changes by the text of
`error`). On the Certificates tab the selected DOWN row's `Error` is shown in the
footer at level Error (`Model.certificateFailure`, derived on every render, no
timer, Rule 128), so a refused `CONNECT` is readable instead of cut off in a
column. The Monitors tab is unchanged. A timed message and the form's validation
status keep priority over it.

### The guard — `internal/netproxy/guard_test.go`

`TestNoHTTPClientBypassesTheProxyEntryPoint` parses every production `.go` file
of the module (go/ast, no type information; test files, `.worktrees`, `.git`,
`vendor`, `testdata`, `docs-site`, `node_modules` and `internal/netproxy` itself
are skipped) and fails on a place that leaves the entry point. The local name of
`net/http` and `crypto/tls` is resolved, aliases included.

| Rule | Reported |
|---|---|
| R1 | an `http.Transport` built by hand: literal, `&literal`, `new(http.Transport)` or `var x http.Transport` |
| R2 | any reference to `http.DefaultTransport` (cloning it keeps its `Proxy` and invites the next line to replace it) |
| R3 | an assignment to a `.Proxy` field, or a `Proxy:` key in a composite literal, in a file that imports `net/http` |
| R4 | a raw TLS dial: `tls.Dial`, `tls.DialWithDialer`, `tls.Client`, `tls.Dialer{}`, `&tls.Dialer{}`, `new(tls.Dialer)` or `var d tls.Dialer` |
| dot-import | a `. "net/http"` or `. "crypto/tls"` import, which hides every selector above; it cannot be excepted |

A client with no `Transport` is compliant: it uses `http.DefaultTransport` and so
the proxy environment. Reports read `file:line: rule (enclosing func)`.

Known exceptions live in `proxyExceptions`, matched by file, enclosing function
and rule, never by line.
`TestEveryProxyExceptionStillMatches` fails on a stale entry, and
`TestNetworkDocListsEveryProxyException` fails when this section does not name
the file and the function of an entry.

| File | Func | Rules | Reason | Until |
|---|---|---|---|---|
| `internal/nexusbench/nexusapi.go` | `newClient` | R2, R3 | the bench is reached directly (`Proxy = nil`): a proxy would receive the bench credentials | permanent |
| `internal/nexusbench/probe.go` | `NewProber` | R2, R3 | the probe goes without a proxy, like the bench client | permanent |
| `internal/netcheck/env.go` | `systemEnv.Head` | R1 | the Diagnostics measure the direct path (decision Q1, DEVDESK-582); the Proxy row says when a proxy applies | permanent |
| `internal/netcheck/env.go` | `systemEnv.Handshake` | R4 | the Diagnostics measure the direct path (decision Q1, DEVDESK-582); the TLS step runs on the same direct path as the TCP step | permanent |
| `internal/forward/proxy.go` | `routeTransport` | R1 | a named route stays direct, like the TCP forwards to the same target (decision Q1) | permanent |

Adding an exception means adding it to `proxyExceptions` and to this table in
the same commit, with a reason and an end.

### What the guard does not see

- Clients built inside third-party dependencies: the GitLab client-go, go-github
  and the Docker SDK build their own transports. They are not covered.
- A composite literal whose type is elided (`[]http.Transport{{...}}`), a local
  variable that shadows `http` or `tls`, reflection, and a wrapper type that
  hides the transport.
- An `http.Transport` held by value and not built from a literal: a struct
  field, or a parameter or result typed `http.Transport`. The guard inspects
  literals, `new`, `var` and `.Proxy` assignments only.
- Raw `net.Dial*` calls are out of scope: TCP forwards, DNS, the UDP of
  `nettrace` and netcheck's `DialTCP` are not HTTP paths.

### Go behaviors to keep in mind

Which variables are read, and how:

- `HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY`, each in two casings. The upper
  case name wins over the lower case one, and an empty value counts as not
  defined (`firstNonEmpty`, like Go's `getEnvAny`).
- `ALL_PROXY` and `FTP_PROXY` are not read, by Go or by DevDesk.
- The environment is read once per process (`http.ProxyFromEnvironment` caches
  it; `ContainerForward` does the same with `sync.OnceValue`). A context switch
  reads nothing again: the user restarts DevDesk after changing a variable. So
  `t.Setenv` has no effect after the first call. A test that needs a given
  environment runs the code in a subprocess, or injects a `Selector` (or, for the
  containers, swaps the `proxyForward` seam of `internal/scan`).
- `NO_PROXY` takes a host name (it also covers its subdomains), `.suffix`
  (subdomains only), an IP address, a CIDR, `host:port`, and `*` (everything).
- `localhost` (exactly) and loopback addresses are never proxied by
  `http.ProxyFromEnvironment`, whatever the variables say; `*.localhost` names
  are proxied.
- CGI: when `REQUEST_METHOD` is set Go refuses `HTTP_PROXY` (see the Proxy row
  above). The container forward does not look at it: a value is forwarded as is.
- A proxy that limits `CONNECT` to port 443 answers 403 for another port (LDAPS,
  SMTPS); the footer now says so.
- socks5 proxies: the HTTPS monitor accepts them (`net/http` does), the SSL
  check refuses them with `StepScheme` and no new dependency. The gap is
  deliberate and known.

Who follows these variables after DEVDESK-581 and DEVDESK-582:

- The HTTP/HTTPS monitor, the SSL monitor and the certificate viewer: the entry
  point above (DEVDESK-581).
- The Diagnostics: direct on purpose (decision Q1). The Proxy row says which
  proxy would apply; no check is sent to a proxy (DEVDESK-582).
- The scanner containers: see the next section (DEVDESK-583).
- Image pulls done by the engine: the Docker or Podman daemon pulls, with its own
  proxy configuration (the service environment, Docker Desktop settings, the
  Podman machine). DevDesk does not configure it and does not pass its variables
  to it. DevDesk's own registry calls (`internal/oci`) go through the entry point.
- `git` (clone, sync) is a child process: it inherits the environment and reads
  it with libcurl's rules, not Go's. libcurl ignores an upper case `HTTP_PROXY`
  for `http://` URLs (only `http_proxy` counts), reads `all_proxy`, and its
  support for CIDR in `NO_PROXY` depends on the curl version. `http.proxy` in the
  git configuration wins. DevDesk does nothing to reconcile the two.

## Container scanners and the proxy (DEVDESK-583)

A scanner that runs from an image (trivy, gitleaks, cosign, plumber,
kubeconform, its manifest rendering, and the version probe of a tool image) has
its own network stack and does not see this process's environment. DevDesk
forwards the proxy itself.

- **One constructor.** `scan.containerRun` (`internal/scan/container_run.go`) is
  the only place that builds `<engine> run --rm`. Its `cmd()` writes
  `run --rm <Flags> -e NAME ... <Image> <Argv>`. Every launcher (`trivy_args.go`,
  `gitleaks.go`, `cosign.go`, `plumber.go`, `kubeconform.go`,
  `kubeconform_render.go`, `tool_source.go`) goes through it.
- **Names in argv, values in the environment.** `netproxy.ContainerForward()`
  returns a `Forward`, a snapshot of `HTTP_PROXY`, `HTTPS_PROXY` and `NO_PROXY`
  taken once. For each of the three, the first non-empty value of the upper then
  lower case name is given to both casings. `cmd()` adds `-e NAME` (no value) for
  each name, in the order of `ForwardedVariables`, and sets the values on the
  child process through `Forward.Environ(os.Environ())` (the six names are
  removed from the base first). The engine copies the client's value, so a
  credential in a proxy URL is in no argv: not in the process list, not in the
  command `:jobs` shows, not in a log. The value is not validated.
- **No rewriting.** A proxy on `127.0.0.1`, `localhost` or `::1` is forwarded as
  is. DevDesk does not rewrite it to `host.docker.internal`, does not add
  `--network host` and does not fall back to a direct connection.
- **A `Forward` never prints a value.** `String` and `GoString` show the display
  form (`netproxy.Display`); the values sit behind a function so `fmt` finds
  nothing to print, with any verb.
- **The renderers get no proxy and no network** (DEVDESK-767, D180). A
  `containerRun{Offline: true}` writes `run --rm <Flags> --network none <Image>
  <Argv>`: no `-e NAME`, an empty `Forward`, and `Env` nil unless the caller has
  variables of its own. helm always runs this way and kustomize too, unless
  `scan.tools.kustomize.allow_remote_bases` is on; kubeconform does not (it
  downloads schemas). The renderers run natively with `offlineEnviron`
  instead (a dead proxy and a `GIT_ALLOW_PROTOCOL` that names no transport), and a
  native kustomize is started only after the gate of DEVDESK-771 has read the
  overlay, because Go never proxies the loopback. No new `"run"` literal:
  `Offline` is a branch of the same `cmd()`, so `TestContainerGuard*` is unchanged.
  The loopback proxy warning (`scan.ContainerProxyWarning`) is unchanged and still
  names kubeconform and the other containers. See `scanning.md`.
- **The trust model.** The proxy and its credentials go to every scanner image,
  including one the user set through `tool.Image` (see `scanning.md`).
- **Stderr is redacted.** `cliRunner.Run` redacts each stderr line before it goes
  to `progressFn`, and `waitErr` redacts the diagnostic kept in the `exitError`,
  with `Forward.Redactor` / `Forward.Redact`. A raw proxy value that carries
  credentials becomes its display form (or `[redacted]` when it cannot be parsed),
  and the userinfo and the password disappear, as written, as Go escapes them and
  decoded. A value with no credentials is left alone. The limits are in
  `scanning.md`.
- **The loopback warning.** When the HTTPS or HTTP value names this machine's
  loopback (`Forward.Loopback`, first the HTTPS pair; `localhost` exactly and
  loopback IPs, not `*.localhost`) and a containerised tool is about to run,
  the footer says `<VARIABLE> is <scheme://host:port>, on this machine's loopback:
  scanner containers cannot reach it` (`scan.ContainerProxyWarning`; the variable
  is named as read, the value in display form). The router posts it when a scan
  or a verified pull starts, and the Remediation tab when it scans candidates
  (see `app-shell.md`). It is a warning: the scan still starts.
  - False positive: if the engine's default network is `host` (Podman
    `containers.conf` with `netns = "host"`), the proxy is reachable and the
    warning is wrong. DevDesk does not read the engine configuration.
  - Over-reporting: a scan (`KindScan`) takes every tool the enabled categories
    require, while a scan of images runs only trivy. The warning can name a
    container that the run will not use.
- **Podman on a remote machine** (a Podman machine on macOS or Windows): to be
  verified. Documentation says `-e NAME` without a value reads the client's
  environment, which is what the design relies on; it has not been checked by hand
  here.

### The container guard

`internal/netproxy/container_guard_test.go` keeps a launch from skipping the
forward. Like the HTTP guard it parses the production sources of the module
(go/ast, no types; test files, `.worktrees`, `.git`, `vendor`, `testdata`,
`docs-site` and `node_modules` skipped, `internal/netproxy` read) and reports:

| Rule | Reported |
|---|---|
| C1 | a `[]string` literal whose first element is the literal `"run"` |
| C2 | a call with the literal `"run"` as argument 0 or 1 and at least one argument after it |

The one allowed site is `containerRun.cmd` in `internal/scan/container_run.go`,
hard-coded in the test. Everything else needs an entry in `containerExceptions`,
matched by file, function and rule, and named in this section
(`TestNetworkDocListsEveryContainerException`);
`TestContainerGuardEveryExceptionStillMatches` fails on a stale one.

| File | Func | Rule | Reason | Until |
|---|---|---|---|---|
| `internal/docker/launch.go` | `buildLaunchArgs` | C1 | the user's own container, configured in the launch form: not a scanner | permanent |
| `internal/docker/launch.go` | `VerifyEntrypoint` | C2 | runs only `command -v`, nothing to forward | permanent |

What the guard does not see:

- a `"run"` that is not a literal: built at run time, or a named constant;
- `append(args, "run")`: the literal is the last argument, with nothing after it;
- the `container run` spelling: in `[]string{"container", "run", ...}` the first
  element is not `"run"` (C1), and in `exec.Command(bin, "container", "run", img)`
  the `"run"` is argument 2 (C2 looks at 0 and 1 only);
- a `"run"` at argument index 2 or later;
- C1 looks at the first element only: `[]string{"--rm", "run"}` is not reported;
- a launch that is not a `run` (`docker create`, `exec`), and clients built in
  third-party code.

## The path trace — `internal/nettrace`

The hop-by-hop path to an IPv4 target (DEVDESK-327, S-03a). It came back after
§3.47 deleted the container trace, and it fixes what failed there: it runs **in
the DevDesk process**, so the hops are this machine's. No container, no external
binary (the package never starts a child process; a source guard test enforces
it), no privilege. The spikes measured it unprivileged (uid 1000, no capability)
on native Linux and on WSL2 in NAT and mirrored modes
(`docs/product/network-map/{feasibility,spike-linux,spike-wsl}.md`).

**Linux and native Windows.** `Supported()` returns `nil` on Linux and, on
Windows, when the `iphlpapi.dll` echo entry points resolve (`trace_windows.go`);
it returns `ErrUnsupported` elsewhere (`trace_other.go`, build tag
`!linux && !windows`), and `Trace` then returns `ErrUnsupported` and sends
nothing. `golang.org/x/sys` is a direct dependency: `x/sys/unix` for the Linux
socket calls, `x/sys/windows` for `iphlpapi.dll` (Windows engine).

**One connected UDP socket per TTL (Linux).** Each socket is connected to the target
(port 33435), gets `IP_TTL` set to its hop number and `IP_RECVERR` enabled, and
sends one datagram. The kernel queues the ICMP answer on the socket's error
queue; `await` reads it with `recvmsg(MSG_ERRQUEUE)` through `RawConn.Read`
(the netpoller, no sleep loop) and `decodeExtendedErr` decodes it. A connected
socket with an ephemeral source port means an ICMP message forged off the path
can skew what is shown, not make the trace read something else.

**Two phases** (`engine.go`). `run` first opens all N sockets, in the caller's
goroutine; only then does one goroutine per TTL send and wait, in parallel. So
"not supported" or "no route" is found before any packet leaves: a refused
socket option (`ENOPROTOOPT`, `EOPNOTSUPP`, and `EPERM`/`EACCES` from the system
prober) maps to `ErrUnsupported`, a `connect` failure with `ENETUNREACH` or
`EHOSTUNREACH` to `ErrNoRoute`, and nothing is emitted. All probes then leave at
once, not spread out (D143).

**The Windows engine** (DEVDESK-486, IPv4 only; IPv6 is a separate story). It
plugs into the same two-phase engine through `echoProber` (`echo_conn.go`) and
needs no administrator right.

- **`IcmpSendEcho2`, synchronous, through `iphlpapi.dll`.** `trace_windows.go`
  loads `iphlpapi.dll` and calls `IcmpCreateFile`, `IcmpSendEcho2` and
  `IcmpCloseHandle`. Each TTL gets its own echo handle (`echoConn`, single-use:
  one send, one await). The call is synchronous and cannot be interrupted, so
  `await` runs it in a goroutine, one per TTL, and abandons it when the context
  ends; the goroutine closes the handle itself when it returns, never under the
  running call.
- **Preflight.** Before any handle is created, `udpPreflight` connects (without
  sending) a UDP socket to the target. A missing route fails there with the raw
  Winsock error and becomes `ErrNoRoute`, instead of looking like silence.
- **Classification is by code, never by text** (`echo_status.go`). The reply
  carries an `IP_STATUS` (`GetLastError` when there is no reply): `IP_SUCCESS`
  is `HopTarget`, `IP_TTL_EXPIRED_TRANSIT` is `HopRouter`, the destination
  net/host/protocol/port unreachable statuses are `HopUnreachable`,
  `IP_REQ_TIMED_OUT` is a silent hop, and everything else is `HopError` or an
  `*EchoStatusError` (`ICMP status <n>`, ASCII only, never a localized system
  message). A zero code with no reply is a failure, never a reached target.
- **Source guard extended to `x/sys/windows`** (`source_guard_test.go`). A
  file may import it only to load the one DLL: `NewLazySystemDLL` takes the
  literal `"iphlpapi.dll"`, and `NewProc` takes a literal from the allowlist
  `{IcmpCreateFile, Icmp6CreateFile, IcmpSendEcho2, Icmp6SendEcho2,
  IcmpCloseHandle}` (case-sensitive). Both must be called directly, never used
  as a value; writing the `Name`, `System` or `Dll` field of a DLL handle, naming
  `LazyDLL`/`DLL`/`LazyProc`/`Proc`, and the process, service and
  `LoadLibrary`/`GetProcAddress` symbols are refused.
- **`IsNoRoute` knows the Windows errnos.** Besides `ErrNoRoute`,
  `syscall.ENETUNREACH` and `syscall.EHOSTUNREACH`, it recognizes
  `WSAENETUNREACH`, `WSAEHOSTUNREACH`, `ERROR_NETWORK_UNREACHABLE` and
  `ERROR_HOST_UNREACHABLE` (`noroute_windows.go`, the Winsock codes and the IP
  Helper ones go-netroute can return). The error text never counts.
- **Known limit: the probes are ICMP echoes.** A target (or a firewall in front
  of it) that filters echo requests stays silent: the trace ends on the hop
  limit or the deadline with no `HopTarget`, where the Linux UDP probe would
  have been answered with Port Unreachable. A UDP or TCP fallback is tracked in
  D154 (`docs/backlog.md`).

**Streaming: `TraceEach`** (DEVDESK-328).
`TraceEach(ctx, target, opts, onHop func(Hop)) (Result, error)` is `Trace` that
also hands each hop to `onHop` as it resolves; `Trace` is `TraceEach` with a
nil callback, and the `Result` is the same. The callback is an argument rather
than an `Options` field. Contract:

- It is called from the goroutine that called `TraceEach`, **one call at a
  time**, so a plain slice needs no lock.
- Hops arrive **in answer order, not TTL order**.
- **Never for an unresolved hop** (`HopUnresolved`), **never after `TraceEach`
  has returned**, and never for a target refused before any probe (a
  validation or open error returns with no call).
- It must not block.
- **A hop above the terminal one may be reported** while the terminal is still
  unknown. `Result.Hops` is cut at the terminal hop as before, so the caller
  reconciles: the trace shows `visiblePathHops` while streaming (sorted by
  TTL, unresolved dropped, everything after the first target or unreachable
  hop cut) and replaces its hops with `Result.Hops` at the end.

**The result.** `Trace(ctx, target, Options) (Result, error)`. The target is
unmapped first (`::ffff:a.b.c.d` becomes IPv4); any other IPv6 address gets
`ErrNotIPv4`, and `0.0.0.0`, multicast and `255.255.255.255` get
`ErrNotUnicast`. Loopback and private targets are allowed: the caller decides at
its own boundary. Read `Result` only when the error is nil, because the zero
`Result` and the zero `Outcome` (`OutcomeReached`) mean "no trace".

| `Outcome` | Meaning |
|---|---|
| `OutcomeReached` | The target answered (Port Unreachable from the target itself, `HopTarget`). |
| `OutcomeUnreachable` | A router reported the target unreachable (`HopUnreachable`). It ends the trace, like a reached target. |
| `OutcomeMaxHops` | `MaxHops` (at most 30) exhausted without an answer from the target. |
| `OutcomeDeadline` | The whole-trace deadline (at most 30 s) expired. |
| `OutcomeCancelled` | The caller's context was canceled. |

`Hops` has a **stable shape**: entries in TTL order, `Hops[i].TTL == i+1`. It is
cut at the terminal hop (target reached or refused), or holds one entry per TTL
up to `MaxHops`. When the deadline or a cancellation ends the trace first it
holds exactly `MaxHops` entries, and every probe that did not resolve is a
`HopUnresolved`, even above a terminal hop already known. `HopKind` is
`silent`, `router`, `target`, `unreachable`, `unresolved` or `error`.

**A silent hop is not a failure.** Many routers do not answer expired probes; a
`HopSilent` in the middle of the path says nothing about the hops after it, and
a later TTL can still answer or reach the target. The probes are independent, so
the trace never stops at a silent hop.

**Errors.** Match the sentinels `ErrUnsupported`, `ErrNotIPv4`, `ErrNotUnicast`
and `ErrNoRoute` with `errors.Is`. The errors `Trace` returns and `Hop.Err` carry
the raw platform text (errno messages, in the machine's language): show a fixed
label, never that text, in a summary.

**Known limits**, measured and not fixed:

- **The number of hops that answer varies from one run to the next**, and so
  does the TTL at which the target answers (6 to 10 hops with an address to
  `1.1.1.1` in the native Linux spike; also observed on WSL2 NAT and on native
  Linux, sometimes with silent TTLs before the target). The cause is an
  **unverified hypothesis**: ICMP rate limiting by routers. A timing defect of
  the prober is not ruled out. The lowest TTL that reached the target is not the
  real hop count.
- **A multi-homed target** that answers Port Unreachable from another of its
  addresses is classified `HopUnreachable`, not reached (`classify`).
- **The duration is about `ProbeTimeout` (3 s by default)** as soon as one hop
  before the target is silent, because the trace completes only when every TTL
  up to the terminal one is resolved.
- **`Options.MaxHops` is not tied to the configuration** (`traceroute_max_hops`
  stays removed).
- **The probes are UDP to the fixed port 33435** (`defaultProbePort`, not
  exposed). The trace refuses a target that carries a port. See "The trace
  sub-level" below.

**`Supported()` and the trace.** The trace calls `Supported()` as a launch
guard (`PathModel.launchable()`): where it returns an error, the Trace Path button, `H`, `enter` and `ctrl+o`
are greyed out in the header and refused with a footer warning; nothing is
resolved or sent (Rule 130).

The guard is `pathSupport(goos, kind, traceErr)` in
`internal/ui/netdiag/path_platform.go` (DEVDESK-334). The trace is available
on native Linux, native Windows and WSL2. macOS, WSL1 and any other OS are
greyed out. It names the reason, as a `pathPlatformError` that unwraps to
`nettrace.ErrUnsupported`:

| Platform | Reason |
|---|---|
| Native Windows | none, the trace is available (since DEVDESK-486) |
| macOS | `The path trace needs Linux — not available on macOS` |
| Other OS (not linux, windows or darwin) | `The path trace is not available on <goos>` |
| WSL1 | `The path trace needs a Linux kernel — WSL1 has none (use WSL2)` |
| Anything else unsupported (`nettrace.Supported()` fails) | `The path trace is not supported on this platform` |

The footer shows the reason as a permanent Warning on the form (the Trace Path
button), not only after `enter`. The decision is made once per process
(`systemPathSupport`, a `sync.OnceValue`), so `launchable()` does no I/O per
frame, and nothing is sent on the network. A WSL2 whose networking mode is
unknown never greys the tab out. No WSL2 mode is switched off: the spike
verdict (`docs/product/network-map/spike-wsl.md`) is that WSL2 is supported, so
no switch is coded (YAGNI).

## The trace sub-level — streaming, cancellation, resolution

How `internal/ui/netdiag` drives `nettrace` (DEVDESK-328).

**Streaming.** `launch` (called from `Update`) creates the run's context, a
buffered `chan tea.Msg` and the launch Cmd: a batch of the producer
(`producePath`, which does the I/O and touches no model, Rule 110) and
`waitForPathMsg(ch)`. The producer sends three kinds of message, each tagged
with the run's generation: `pathResolvedMsg` (the address), `pathHopMsg` (one
per `onHop` call) and `pathDoneMsg` (the `Result` or an error), then closes the
channel. `Update` re-arms `waitForPathMsg` after every resolved or hop message;
the done message is the last and is not re-armed. The three messages reach the
Path sub-model whichever tab is on screen. The resolved message also carries the
route (below).

- **Capacity 32** (`pathChanCap = nettrace.MaxHopsLimit + 2`): the resolved
  message, at most one hop per TTL (30) and the done message. The producer
  therefore never blocks on a reader. Sends of the resolved and hop messages
  are non-blocking. **The done message is never lost**: if the channel is
  full, `sendDone` drops the oldest queued message to make room.
- **Generation.** `PathModel.gen` is bumped on each launch and on a cancel. A
  message whose `gen` differs is dropped and not re-armed, so the late messages
  of a canceled run change nothing.
- **Cancellation.** The model holds the run's `context.CancelFunc`; it is called
  **from `Update` only** (`releaseRun`, on `esc` while running, on the next
  launch, and when the done message arrives, which releases the context). `esc`
  answers at once: the state becomes `pathDone`, the outcome `Canceled`, the
  hops already seen stay on screen. The engine stops on its context; the view
  stops reading and nothing is drained, so no goroutine is left blocked on the
  channel.

**Resolution is the view's job, not `nettrace`'s**, which stays address-only. A
literal IPv4 is used as is. A name is resolved with
`net.DefaultResolver.LookupNetIP(ctx, "ip4", host)` under its own timeout, the
context's `network.check_timeout` (`netcheck.Settings.Normalized().CheckTimeout`,
read when the view is built; the tab falls back to 5 s
(`defaultPathResolveTimeout`) otherwise). The **first IPv4 address** is traced (unless Diagnostics already read one of the
addresses returned: "Which address", below), and the header's `Target` field shows it after
the name. A literal IPv6 target
(brackets or several colons) is refused before launch with the footer error
`Only IPv4 targets are supported by the path trace`.

**The route rows (DEVDESK-329).** After resolution and before the trace,
`producePath` asks `pathEngine.route` for the traced address. The system
engine uses `netcheck.SystemEnv(netcheck.DefaultSettings()).Route`, which sends
nothing and needs no privilege. The answer (`RouteHop`: interface, source,
gateway) rides on `pathResolvedMsg` (`route`, `routeErr`), so there is no extra
message and the capacity stays 32. `newPathRoute` turns it into a `pathRoute`
(addresses unmapped to IPv4). The lookup asks the kernel the same question as
`ip route get <addr>` (go-netroute, `RTM_GETROUTE`, policy rules applied), and
the probe socket is unmarked and not bound to a device, so a VPN that captures
the default route shows as the interface row, and the probes take that route.

- **Interface row**: `eth0 (192.168.1.21)`; without a source address `eth0`;
  without a name `unnamed (192.168.1.21)`; `no route` when the lookup proved
  there is none; `undetermined` when the lookup failed for another reason.
- **Gateway row**: the address; `directly connected` when the route has none
  (or an unspecified one); `-` after `no route`; `undetermined` when the lookup
  failed. The texts `undetermined`, `directly connected` and `-` are dim. The
  row stays even when the gateway is also hop 1.
- **`no route` is not `undetermined`.** `newPathRoute` sets `pathRoute.NoRoute`
  only when `nettrace.IsNoRoute(err)` is true: `ErrNoRoute`, or `ENETUNREACH` /
  `EHOSTUNREACH` anywhere in the error chain (`errors.Is`; the error text never
  counts). `IsNoRoute` (`internal/nettrace/noroute.go`) is the single place that
  classifies a no-route failure. Any other failure, including a routing table
  that could not be read, leaves `NoRoute` false and the rows read
  `undetermined`: that says nothing either way.
- **A failed lookup is not fatal.** The trace still runs; the rows read
  `no route` or `undetermined`. The raw error is logged (`ERROR [netdiag/path]
  route <host>: …`) and never shown.
- **The source is never a host name.** It is the fixed text `this machine`, not
  `os.Hostname()`: under WSL2 the distro's host name defaults to the Windows
  computer name, which would label the source as another machine. Under WSL2
  the row says so instead (see "The WSL2 labels" below), still without a host
  name.
- **Counter.** The footer's `Tracing <host> — N hops answered` counts hop rows
  only (`hopCount`), not the local rows. A run that ends on a no-route error
  keeps the local rows; any other error done clears every row (see "Errors
  become fixed labels" below).

**The container source (DEVDESK-330).** The column can start in a running
container of a Docker Engine instead of on this machine. Files: `path_source.go`
(the source and its list, pure), `path_container.go` (the rows' content, pure),
`path_source_picker.go` (the selector, a value type with no I/O),
`path_source_flow.go` (opening, choosing, drawing), `path_run.go` (the reads).

- **The source is a value of the tab**, `pathSource` (`ID`, `Name`, `Image`;
  the zero value is `this machine`). It is read by the launch, not by the trace
  engine: **the probes always leave from this machine**. Nothing runs inside the
  container.
- **The selector** is a framed list drawn over the table with `overlay.Compose`
  (palette variant of Rule 112; the body stays the table, Rule 139), titled
  `Trace from` in the top border, box width `clamp(width-4, 20, 60)`, centred,
  starting on the table's second line. It opens at once with `this machine` and
  the footer's loading dot (`Reading running containers...`, Rule 128), then
  fills from a `pathSourcesMsg` tagged with `sourcesGen`; an answer for an older
  request, or one that arrives after the selector closed, is dropped. The list
  is read again at every opening (no cache). The cursor starts on the source in
  use, marked `(current)`. Entries are `this machine`, then the containers whose
  state is `running` (paused and restarting are left out), sorted by name, shown
  as `<name> · <image>`. A row whose ID fails `IsContainerID` or whose name is
  empty after `engineText` is skipped.
- **Listing** (`listPathSources`, in a Cmd under the resolve timeout) goes in
  this order: engine name (`engine.Current().Name`, no invocation), then
  `info` for the OS name, then `ps`. The result is `this machine` alone, with a
  neutral footer note, when: the engine is podman (`podman is not supported as a
  container source`, nothing invoked); `info` or `ps` fails, or no engine is
  present (`No Docker Engine reachable — only this machine is offered`; the raw
  error goes to the log as `ERROR [netdiag/path] sources: …`); the OS name
  starts with `Docker Desktop` (`Docker Desktop is not supported as a container
  source yet`, `ps` is not called); or no container runs (`No running container
  — only this machine is offered`). Docker Desktop is refused because its
  containers live in a VM: drawing their network before `this machine` would
  be false.
- **Choosing.** `enter` on the source already in use only closes the selector.
  Any other choice clears the column (rows, route, container, address, outcome,
  error), returns the model to `pathReady` with the target kept and nothing
  sent, and shows a `Source` header field with the container name (absent for
  this machine). The column never mixes two sources. `H` from Diagnostics
  (`PathModel.ready`) resets the source to this machine: the route row it comes from is
  this machine's.
- **The container is read in the producer**, after the route lookup and before
  the trace (`inspectPathSource`, under the resolve timeout, killed by the run's
  context), and rides on `pathResolvedMsg` (`source`, `ctr`, `ctrErr`): no new
  message, capacity still 32. A trace from this machine never calls it. A
  failure is not fatal: the rows read `undetermined`, the trace runs, the raw
  error is logged (`ERROR [netdiag/path] inspect <id>: …`) and never shown.
  Reading at launch, not at selection, means "trace again" reads it again and a
  container that stopped in between is shown as not running.
- **Three more row kinds** (`bridge`, since DEVDESK-331, and `engine`, since DEVDESK-332, below), ordered by `pathSteps` only (it takes the container
  as a first argument; `nil` gives the DEVDESK-329 column unchanged): `container`
  and `network` before `source`, `trace` after `gateway` and before the hops.
  Hop and RTT are empty for all three; `trace`, and `network` when it reads
  `undetermined…` or `none…`, are dim.

  | Row | Address text |
  |---|---|
  | `container` | `web-1 (172.18.0.5)`; no address known: `web-1`; stopped at launch: `web-1 (not running)` |
  | `network` | `app_net via 172.18.0.1`; host mode: `host (shares this machine's stack)`; `none`: `none (no network)`; `container:<x>`: `shares another container's network`; undecidable: `undetermined` or `undetermined (N networks)`; inspection failed or container not running: `undetermined` |
  | `trace` | `from this machine, not from the container`; host mode: `from this machine (shared with the container)`; with an `engine` row: `from this machine (host or WSL VM)` |
  | `engine` | `internal path not visible (location unknown)` or `undetermined (bridge not verified)`; see below |

- **The default-route network is never guessed** (`defaultRouteNetwork`). The
  candidates are the endpoints with an IPv4 gateway (an internal network has
  none, so it cannot hold the default route). One candidate: it. Several: the
  one whose `GwPriority` is strictly the highest, provided the engine reported a
  priority on every candidate. A tie, or a missing priority (API before 1.48,
  Docker 28), gives `undetermined (N networks)`, with N the number of endpoints.
  moby documents a tie-break by network name, but its direction was not
  measured, and docs.docker.com says the default gateway "may change whenever a
  container's network connections change". The address on the row is the
  container's IPv4 on that network, and `via` is that network's gateway.
- **Host mode** (`HostConfig.NetworkMode == host`) draws no bridge: the network
  row says the container shares this machine's stack, and the trace row says
  the probes leave from this machine, shared with the container. `none` and
  `container:<id>` get their own texts and are never resolved further.
- **`engineText`.** Every string an engine returned (names, images, network
  names) passes once through `engineText` at conversion: invalid UTF-8 dropped,
  escape sequences stripped (`ansi.Strip`), control (Cc) and format (Cf, so
  bidi overrides and zero-width) runes dropped, whitespace collapsed, at most
  128 runes. The cells are therefore display-safe, and Rule 122 holds (plain
  text in `Cell`, colour in `Style`). It duplicates `imagesearch`'s
  sanitizer; a shared helper is a later refactor.
- **Criterion 7: the engine reads start nothing.** Three guards pin it.
  `TestPathSourceReadsStartNothing` (`internal/docker`) records the arguments of
  the four reads on a stub runner: the first is `ps`, `info`, `container` or
  `network` (then `inspect`), and no argument is a state-changing verb.
  `TestPathEngineReadsStartNothing` (`internal/ui/netdiag`) records the real
  wiring (`systemPathEngine`) through `listPathSources` and the producer and
  asserts the same. `TestPathTabCallsOnlyReadOnlyEngineFunctions` parses every
  non-test file of the package and fails on any `docker.<X>` outside
  `Container`, `ContainerNetworking`, `EndpointNetwork`,
  `ListContainersContext`, `EngineOperatingSystem`, `InspectContainerNetworking`,
  `InspectNetworkBridge`, `NetworkBridge` and `IsContainerID`, on any `engine.<X>` other than `Current`, and on a string
  literal equal to a verb such as `start` or `exec`.
- **Limits.** A remote engine (`DOCKER_HOST=tcp://…`, an ssh context) or an
  engine in a VM other than Docker Desktop is **not detected** as such: its
  containers are listed and drawn before `this machine`, which they never pass
  through. Since DEVDESK-332 the missing bridge is signalled by the `engine`
  row (below); the false positive on `docker0` of a remote engine remains. The
  `trace` row limits the damage. Network drivers (macvlan, ipvlan) are not
  distinguished, because the container inspection does not carry them. The
  container ID is the short one `docker ps` prints, validated by
  `IsContainerID`. Kubernetes, overlay and named VMs are out of scope.

**The bridge row (DEVDESK-331).** Between `network` and `source`, the column
shows the host bridge the container's network is attached to, **only when that
bridge exists on this machine**. Files: `path_bridge.go` (`readPathBridge`,
`hostBridge`), `path_run.go` (the reads), `path_steps.go` (`stepBridge`).

- **`InspectNetworkBridge(ctx, networkID)`** runs `network inspect --format
  <networkBridgeFormat> <id>`: only the driver, the
  `com.docker.network.bridge.name` option and the ID are read. The ID must be 64
  hex digits, one argv element, never a name. The result is a `NetworkBridge`
  (`Driver`, `Name`). `Name` is empty unless the driver is `bridge`; it is the
  option when set, else `br-` + the first 12 digits of the ID. The error carries
  no command output.
- **`pathEngine`** gets two more fields, `network` (`docker.InspectNetworkBridge`)
  and `interfaces` (`netiface.List`), so tests inject both.
- **`readPathBridge`** runs in the producer, after `inspectPathSource`, under the
  resolve timeout, and its result rides on `pathResolvedMsg.bridge`: no new
  message, capacity still 32. It makes **no call** for a trace from this machine,
  host, `none`, `container:<x>`, a container stopped at launch, an ambiguous
  default-route network (`routeEndpoint`, shared with `newPathContainer`), a
  failed inspection or an endpoint with no network ID.
- **`hostBridge`** accepts the interface only if its name equals the expected one
  exactly (case sensitive) **and** it carries the network's gateway among its
  IPv4 addresses. The OS is never consulted. Malformed CIDR entries are ignored.
  The name goes through `engineText`.
- **Failures are logged, never shown** (`ERROR [netdiag/path] bridge network|
  interfaces: …`, not logged when the run was canceled): the row is simply
  absent. `pathSteps` adds `stepBridge` only when `pathContainer.Bridge` is set
  (`withBridge`, for a container attached to a network). Address text is the
  interface name; Hop and RTT are empty.
- **Limits.** Absence of the row no longer means "not found": since
  DEVDESK-332 a missing or unverified bridge is told by the `engine` row below,
  and no row at all is left for host, `none`, `container:<x>`, a stopped
  container, an ambiguous network, a failed inspection or a network with no
  IPv4 gateway (for example `inhibit_ipv4`: `defaultRouteNetwork` keeps no
  candidate, so nothing is read). Rootless Docker gives an `engine` row only
  when the endpoint has an IPv4 gateway and no host interface has the
  bridge's name (`bridgeAbsent`, "not visible"). User-defined networks (`br-<id>`) are protected by their unique
  name. A **false positive** remains for the default `docker0` on a remote
  engine: if the remote engine and a local Docker both use `docker0` with
  `172.17.0.1/16`, the row appears though the container is not behind this
  machine's bridge. This extends the remote-engine limit above.

**The engine row (DEVDESK-332).** When the bridge of the container's network is
not verified on this machine, the column shows an `engine` row between `network`
and `source`, instead of a `bridge` row. It says the internal path is not seen
from here; it never says where the engine runs. Files: `path_bridge.go`
(`classifyBridge`, `readPathBridge`), `path_container.go` (`withBridgeRead`,
`engineView`), `path_steps.go` (`stepEngine`), `path_view.go`.

- **`pathBridge`** replaces the bridge name on `pathResolvedMsg.bridge`: a
  `State` and, when found, a display-safe `Name`. Four states:

  | State | Produced when |
  |---|---|
  | `bridgeNotRead` (zero value) | no read was made: this machine, host, `none`, `container:<x>`, stopped at launch, ambiguous default-route network, failed container inspection |
  | `bridgeFound` | an interface named exactly as the network's bridge exists **and** carries its gateway (`hostBridge`); the name passes `engineText` |
  | `bridgeAbsent` | the expected name is known, the gateway is valid and **no** interface has that name |
  | `bridgeUnverified` | anything else: no network ID on the endpoint, `network inspect` failed, the driver is not `bridge` (empty name), `netiface.List` failed, empty name or invalid gateway, an interface of that name exists but does not carry the gateway, or the name is empty after `engineText` |

- **`classifyBridge(want, gw, ifaces)`** gives the last three states from the
  engine's answer and this machine's interfaces. `readPathBridge` calls it and
  maps every failure to `bridgeUnverified` (logged as before, never shown).
- **`withBridgeRead`** (`pathContainer`, replaces `withBridge` in
  `handleResolved`) copies the outcome into the container: `found` sets
  `Bridge`, `absent` sets `Engine = engineHidden`, `unverified` sets
  `Engine = engineUnverified`. Only a container attached to a network
  (`attachNetwork`) is touched.
- **`pathSteps`** adds `stepBridge` when `Bridge` is set, else `stepEngine` when
  `Engine` is set: **never both**. Step cell `engine`; Hop and RTT are empty.
  The Address (`engineText()`) is dim, like `trace`, and carries **no bridge name
  and no IP**:

  | `Engine` | Address text |
  |---|---|
  | `engineHidden` | `internal path not visible (location unknown)` |
  | `engineUnverified` | `undetermined (bridge not verified)` |

  With an engine row the `trace` row reads `from this machine (host or WSL VM)`.
- **Decision: the engine's nature is never inferred.** Whether the engine runs
  in a VM, on another host or rootless is not read from the OS name, the
  `DOCKER_HOST` or an address range: the column says what this machine can and
  cannot see, nothing more. `engine VM` is to be written only when the engine
  itself declares it is a VM. That is not done: Docker Desktop stays excluded
  from `ctrl+o` (DEVDESK-330) and S-07 (WSL2) is not a prerequisite.

**The WSL2 labels (DEVDESK-333).** Under WSL2 the machine DevDesk runs on is a
VM, and its first hop is the Windows host's virtual switch, not a router. The
column says so only on proof. Files: `internal/wslenv` (detection),
`internal/ui/netdiag/path_wsl.go` (`withWSL`, `windowsHostGateway`,
`wslSourceText`), `path_run.go` (the read), `path_steps.go`.

- **Detection (`wslenv.Detect`).** `Gather` collects `Facts`, `Classify` turns
  them into `Info` (`WSL2`, `Mode`, `NATInterface`, `NATGateway`); a zero `Info`
  means not WSL2. Off Linux nothing is read. WSL2 is proven when
  `/proc/sys/kernel/osrelease` contains `WSL2` (case-insensitive) and the
  facts look like WSL (`WSL_DISTRO_NAME` set, or `microsoft` in osrelease).
  Native Linux gets no mention at all.
- **Generation (`wslenv.DetectKind`, DEVDESK-334).** `ClassifyKind(Facts)`
  returns `KindNone`, `KindWSL1` or `KindWSL2`, reading only `WSL_DISTRO_NAME`
  and the osrelease (`GatherKernel`: no process, no interface listing, nothing
  off Linux). WSL1 is an osrelease containing `Microsoft`, case-sensitive; it is
  checked before the case-insensitive WSL test. The trace uses it to refuse
  WSL1 (see "`Supported()` and the trace"). `ClassifyKind` can say WSL2 for an
  old 4.19 kernel where `Classify().WSL2` is false; known, minor.
- **Mode.** `wslinfo --networking-mode`, run only under WSL, from
  `/usr/bin/wslinfo` then `/usr/sbin/wslinfo` (absolute paths, never the search
  path), with the constant argument, an empty environment, a 2 s deadline and
  the output bounded to 64 bytes. The answer is cross-checked with the
  `loopback0` interface, which exists under mirrored networking only: `nat`
  with `loopback0`, `mirrored` without it, any other answer, a failed run or a
  missing `wslinfo` (WSL before 2.0.4) gives `unknown`.
- **Source row.** `WSL2 (VM), not the Windows host · NAT mode`, `· mirrored
  mode` or `· mode unknown`. The `trace` rows still say `from this machine`.
- **Windows host label.** The gateway row and the TTL 1 hop get ` — Windows
  host (virtual switch)` only when all hold: WSL2 proven, mode NAT, the default
  route of `/proc/net/route` (`defaultGateway`) has the same gateway and the same
  interface as the traced route, and the hop is TTL 1 at that gateway. Never in
  mirrored or unknown mode.
- **Read in the producer** (`detectPathPlatform`, once per launch, under the
  resolve timeout; `pathEngine.platform`, injectable). The result rides on
  `pathResolvedMsg`: no new message, capacity still 32. `withWSL` labels the
  steps without modifying them. With no answer the column reads as on native
  Linux.
- **Import guard.** `wslenv` starts a process, so it is the declared exception
  on `allowedInternalImports` in `path_engine_source_guard_test.go`. What it may
  run is pinned by its own `internal/wslenv/source_guard_test.go`, which is the
  contract.
- **Limits.** A VPN inside the distro that replaces the default route gets no
  Windows host label (the routes differ). Measured on WSL 2.7.13.0 only. The
  label says what the virtual switch is, not what the Windows host reaches.

**Errors become fixed labels.** `pathErrorLabel` maps the sentinels (matched
with `errors.Is`, wrapped errors included) to US English sentences:
a resolution failure to `Could not resolve <host> — no probe was sent`, no
IPv4 answer to `<host> has no IPv4 address — the path trace is IPv4 only`,
`ErrNotIPv4`, `ErrNotUnicast`, `ErrNoRoute` (`No route to <host> from this
machine — no probe was sent`; matched with `nettrace.IsNoRoute`, which also
accepts a raw `ENETUNREACH` / `EHOSTUNREACH` in the error chain) and `ErrUnsupported` to their own sentence, and
anything else to `The path trace failed — check logs`. The raw error text
(errno, resolver message, in the machine's language) goes to the log only
(`ERROR [netdiag/path] resolve|trace <host>: …`). An error leaves the sub-level in
`pathDone` with the hops cleared and the label shown as a persistent error
(level Error) until the next launch (`enter` traces again).
`handleDoneError` decides what stays on screen: when `nettrace.IsNoRoute(err)`
is true, the column **keeps the local rows** (container rows, source, interface
and gateway, platform labels), with the route set to `pathRoute{NoRoute: true}`
(`no route`, gateway `-`); the hops are cleared. Any other error clears the
route, the container, the platform info and every row, as before. How
a successful trace ended is the header's `Path` field (`pathOutcomeLabel`:
`Reached at hop N`, `Reported unreachable at hop N`,
`No answer from the target within N hops`, `Stopped after 30 s`, `Canceled`).

**When the call fails (DEVDESK-335).** `path_noresponse.go`. When a trace ends
on `OutcomeMaxHops` or `OutcomeDeadline`, `refreshSteps` passes the rows through
`withNoResponse(steps, res)`; it returns a copy and modifies neither input.

- **The tail.** `silentTail` takes the longest final run of hops whose kind is
  `HopSilent` or `HopUnresolved`. `withNoResponse` replaces it with one
  `stepNoResponse` row: Hop `N+` (the first TTL of the run; the TTL alone when
  one hop is silent), Step `silent`, RTT `-`. Address text: `no response beyond
  hop N (hops A to B)` or `(hop B)`, with N the TTL of the last router that
  answered (`Anchor`); `no response from any hop (hops 1 to B)` when none did.
- **The anchor.** The last hop that answered (kind `HopRouter` with a valid
  address) gets `· last to answer` after its address, after the Windows host
  label when there is one.
- **No grouping** for `OutcomeReached`, `OutcomeUnreachable` or
  `OutcomeCancelled`, nor when a `HopTarget` or `HopUnreachable` hop precedes the
  tail: that hop contradicts "no response beyond", so the rows stay as they are.
  A nil result changes nothing.
- **Nothing is concluded.** The rows say what was observed, never why: no cause
  and no "down". A silent hop most often means a router or firewall that does not
  answer these probes (ICMP filtering), which is a hypothesis; the rows do not
  state it.

**The target takes no port (DEVDESK-335).** `parsePathTarget` accepts a
bare host name or IPv4 address. `host:80`, `[::1]:443` and `host:` are refused
with `Path ignores ports: enter a host or IP address only` (`errPathPort`,
footer level Error, nothing resolved, no trace). A literal IPv6 target stays
refused with the IPv6 error. `pathTarget` has no `Port` field. The probes are
UDP to port 33435 (`defaultProbePort`); the trace reaches a host, not a service.
This partly replaces point 3 of D144 (the port accepted and shown, not probed);
see D150 in `docs/backlog.md`. `traceroute_max_hops` is still not a
setting: the trace uses `nettrace.Options{}`.

**Testing.** The engine is tested against a fake `prober`; the Linux prober has
loopback integration tests. A real trace to `1.1.1.1` is opt-in and never runs in
CI: `DEVDESK_NETTRACE_LIVE=1 go test -run TestLiveTraceToCloudflare -v ./internal/nettrace`
(Linux, needs the internet).

## The socket table — `internal/ports`

The machine's TCP and UDP sockets, and the process holding each, read **in this
process**. `List(ctx, resolve)`, `Kill(pid, Identity)`, `Terminate(pid,
Identity)`, `Probe(pid, Identity)`, `Liveness`, `ErrNoCleanStop`,
`ConfirmIdentity(pid, Identity)`, `Identity`, `ProtectionOf(pid)`, `Protection`
(with `Reason()`), `ProtectedError`, `UnverifiableError` and
`IdentityChangedError` are the whole interface (the two `...ForTest` constructors of `Identity` aside).

**It exists because the old one answered for the wrong machine** (D55). The
Ports tab ran `ss -tupan` inside
`docker run --rm --net=host --pid=host --privileged`, and on Docker Desktop
`--net=host` is the namespace of the Linux VM: the tab listed the VM's NFS
daemons with two-digit PIDs while not one of the host's 38 listening sockets
appeared, and `K` sent SIGKILL to a process of the VM under the impression it was
freeing a port on the machine. The sentence explaining this has sat at the top of
`docker.runDiagHost` since §3.33 rapatriated DNS, ICMP, TCP, TLS and HTTP for
exactly the same reason; `RunSS` and `KillProcess` were the two nobody pulled it
for. **Under Linux the defect did not exist**, which is why it went unnoticed:
the tab was right on the platform it was written on.

No new dependency: `gopsutil/v4/net` was already imported by
`internal/metrics/host.go`. Windows reads iphlpapi, Linux `/proc`, macOS and the
BSDs `lsof` — the last is a subprocess, but one the system supplies, and it
replaces a privileged container.

Four decisions, each with a test:

- **The process names come from one enumeration, not one call per socket.** The
  obvious `process.NewProcess(pid).Name()` goes through `OpenProcess` on
  Windows and needs rights over the target: measured here, **98 of 189** sockets
  came back *Access denied*, so the Process column would have been empty for
  every service on the box. `Processes()` reads a Toolhelp32 snapshot instead
  and named **294 of 294** in 11 ms, elevated or not. The bulk call is not an
  optimisation over the per-PID one — it is the difference between a column that
  is filled in and one that is not.
- **A PID of zero carries no PID.** It is the system declining to attribute the
  socket, and `K` keys on that field: left as `"0"` the row would look killable,
  and process 0 is a whole process group on Unix. `Kill` refuses it a second
  time, on the model of §3.23's double guard. The name is a *separate* question:
  a row may know which process holds the socket and not what it is called, and
  it must stay killable — that is the row the user is acting on.
- **Two states are normalised, and neither is cosmetic.** An unconnected
  datagram socket is `NONE` on Linux and nothing at all on Windows, so the same
  socket read differently depending on where DevDesk ran — and the `l`/`e`
  filters with it. `ESTABLISHED` is shortened to `ESTAB` because the State
  column is ten cells wide, so the long spelling renders as `ESTABLISHE`.
- **`n` resolves the host half only.** `ss` without `-n` also turned 22 into
  `ssh`; Go resolves a name to a port and not the other way round, so honouring
  that would mean shipping a copy of `/etc/services` and calling the result the
  system's answer. Saying less beats saying something the system did not.

The reverse-DNS cache is **package-level**, because the lookups run inside a
`Cmd` and a `Cmd` may not touch the model (Rule 110). It caches the failures
too: a machine talking to hosts with no PTR record would otherwise re-ask for
every one of them on every two-second tick — the storm the cache exists to
prevent, arriving through the failures instead of the successes. Wildcard,
loopback and unspecified addresses are never asked at all.

**`Kill` signals with the rights DevDesk has**, which is the visible change:
another user's process, or a service, now comes back refused by the operating
system instead of succeeding against the wrong machine.

**So the refusal has to be legible, and `K` has to say when it cannot even
try** (§3.49). What DevDesk itself decides is answerable before the keypress;
what the operating system decides is not:

- **No PID, no key.** A socket the system declines to attribute carries an empty
  `PID`, and `K` is greyed on that row (Rule 130) — it used to be advertised
  everywhere and warn only once pressed.
- **Protected, no key.** DevDesk itself, the process that started it, PID 1 and
  the Windows system PIDs are greyed before the keypress, with the reason
  (`Protection.Reason()`) in the footer when `K` is pressed. This is not a guess
  about what the OS would accept: DevDesk refuses by its own rule.
- **Unverifiable, no key (DEVDESK-709).** A row whose process identity could not
  be read at listing is greyed with `Cannot verify which process this is`. This
  is not a guess about what the OS would accept either: without an identity,
  DevDesk could not tell whether the PID still belongs to the process the user
  saw, so it has nothing safe to signal. The protected rule is read first, so
  DevDesk's own row says `This is DevDesk itself`, not `Cannot verify`.
- **Whether the OS will accept the signal is the attempt's answer**, never the
  header's, so a row with a PID stays lit even when the kill is certain to be
  refused. `killFailureMessage` classifies the failure with `errors.Is` through
  the `%w` wrapping `Kill` applies: `os.ErrPermission` reads *Refused by the
  system*, `os.ErrProcessDone` reads *no longer running*, anything else keeps
  the generic line. `Failed to kill PID N` made those the same sentence.

**Protected processes (DEVDESK-708).** `ports.ProtectionOf(pid)` says whether
DevDesk refuses to signal a PID, and why (`Protection`: `ProtectedSelf`,
`ProtectedInit`, `ProtectedWindowsSystem`, `ProtectedParent`; `Unprotected` is
the zero value). The rule, `protectionFor`, is pure and evaluated in this
order, so a PID that qualifies twice gets the first reason:

1. DevDesk's own PID: `This is DevDesk itself` (also when DevDesk is PID 1 in a
   container);
2. PID 1: `PID 1 is the system's init process`;
3. PID 0 or 4, when `goos` is `windows`: `Windows system process`;
4. DevDesk's **direct** parent, only if it was created no later than DevDesk:
   `This process started DevDesk`.

Rules of the parent check, each a decision (D174):

- **Direct parent only.** Ancestors above it (the terminal, the multiplexer,
  the login session) are not protected. A warning for them may come later; for
  now the user is trusted past the first level.
- **Born first.** `parentStartedFirst` compares the creation times of the parent
  and of DevDesk through `procOS.createTime`. A parent younger than DevDesk is a
  process that reused the PID of the real one, so it is not protected.
- **Unreadable or tied means protected.** If either time cannot be read, or the
  two are equal, the parent is protected: when in doubt, do not signal.
- **Lazy read.** The creation time costs a system call (`OpenProcess` on
  Windows) and `ProtectionOf` runs on every render of the header, so it is read
  only when the PID asked about is the parent's.
- **Parent 0 is ignored.** `os.Getppid()` returns 0 when the parent lives
  outside DevDesk's PID namespace (a container, a sandbox); there is nothing to
  compare, so the parent rule never matches.
- **A dead parent can be replaced by a sub-reaper** (`systemd --user`, an init
  shim) born before DevDesk. It is then shown as `This process started
  DevDesk`: too much protection, never too little.

`Kill` and `ProtectionOf` share one parser, `parsePID`: a PID outside int32 or
not in canonical decimal form (`+5`, `007`, spaces) is refused as `not a
process id` before any protection is looked at, and is `Unprotected` for
`ProtectionOf`. The kernel keeps 32 bits (`pid_t`, `DWORD`), so `"4294967295"`
would otherwise become `kill(-1)` and `"4294967297"` PID 1.

`Kill` checks `protectionOf` before the `n <= 0` check, so that `Kill("0")` on
Windows names `Windows system process`; on Unix, 0 and negative numbers stay
`not a process id`. A refusal is a `*ProtectedError` (`PID N not stopped:
<reason>`) and no signal is sent. It does not wrap `os.ErrPermission`: it is
DevDesk's own refusal, not the system's. The header and `Kill` read the same
function, so the greyed key and the refusal cannot disagree (Rule 130).

**Process identity (DEVDESK-709).** A PID names a process only while that
process lives, and the system may give it to another one. `ports.Identity`
(`identity.go`) lets DevDesk tell the two apart: the creation stamp of the
process, in the operating system's own unit. It is **opaque** to the interface,
which only asks `Verifiable()`; `internal/ports` compares it (`Conflicts`: both
known and different). The zero value is "unknown", so a caller that forgets the
identity gets a refusal, not a signal. Three kinds exist: unknown, known, and
*deferred* (Windows only, below).

| OS | Where it is read | Raw value |
|---|---|---|
| Linux | `/proc/<pid>/stat`, field 22 (`starttime`), counted from the last `)` because the command name may contain spaces and parentheses (`parseStat`, which also reads the state, field 3; a `Z` state is then confirmed by the `Threads:` line of `/proc/<pid>/status`) | ticks since boot |
| Windows | `GetProcessTimes`, `CreationTime` (`identity_windows.go`) | FILETIME as one 64-bit value (100 ns since 1601) |
| macOS | `sysctl kern.proc.pid`, `P_starttime` (`identity_darwin.go`) | seconds and microseconds as one value (`timevalStamp`) |
| other | nothing: the read returns `errors.ErrUnsupported` | every row unknown, `K` greyed |

- **Read once per distinct PID, at listing.** `List` calls `attachIdentities`
  after the socket snapshot. It reads each distinct PID once, in ascending
  order, and gives the value to every row of that PID. A PID that cannot be
  read leaves its rows unknown. `Socket.Identity` is not displayed.
  `ProtectionOf` and its `createTime` (milliseconds, from gopsutil) are
  untouched: the parent rule and the identity are two readings of two sources.
- **Windows: a budget, then a deferred read.** A creation time on Windows is a
  wall-clock time, so it can be compared with an instant. `List` takes `listedAt`
  *before* the snapshot, and a row counts as listed only if the process was
  created at least `windowsBirthSlack` (100 ms) before it (`judgeBirth`:
  `bornBefore`, `bornAround`, `bornAfter`); a process born later, or too close to
  tell, leaves the row unknown. Reading is capped at `windowsIdentityBudget`
  (100 ms) per refresh: when it runs out, the remaining PIDs get a *deferred*
  identity that carries `listedAt`, and `K` stays lit on them. The read happens
  when the confirmation opens (`ConfirmIdentity`, called by `confirmKill` in
  `Update`, so it is a short read at the press of `K`, never at render), with the
  same `judgeBirth` test: born before the listing gives a known identity, born
  after gives `IdentityChangedError`, too close to tell gives
  `UnverifiableError`. A deferred identity never reaches `Kill`: it is refused
  there as unverifiable. Linux and macOS have no budget; everything is read at
  listing.
- **Verified on the process held, then signalled.** `realOS.kill(pid, want, sig)`
  (`sig` is `signalTerm` or `signalKill`, DEVDESK-711; anything else is refused
  with `no signal named`) opens the process with `os.FindProcess` (a pidfd on Linux 5.4 and later, a
  handle on Windows), reads the identity through that held process
  (`heldIdentity`, per OS), compares it with `want` (`verdictAtSignal`, a pure
  function) and only then calls `p.Signal(syscall.SIGTERM)` or `p.Kill()`, which
  signal through the same pidfd or handle. SIGTERM and SIGKILL go through the
  same checks. The PID cannot change hands between the comparison and the signal.
  A failed read never sends. The per-OS files contain no signalling call, so
  `TestOnlyRealOSSendsASignal` still holds.
- **Errors.** `UnverifiableError` (`Cannot verify which process PID N is — not
  stopped`) and `IdentityChangedError` (`PID N is no longer the process that was
  listed — not stopped`) are DevDesk's own refusals, like `ProtectedError`: they
  wrap no OS sentinel, so the screen never reads them as `Refused by the
  system`. `UnverifiableError.Cause` is for the log; `Unwrap` does not return it.
  An OS refusal (`os.ErrPermission`, for instance a Windows handle that cannot be
  opened) or a gone process (`os.ErrProcessDone`) stays readable through
  `errors.Is`, and `killFailureMessage` words it as before.
- **Order in `Kill` and `Terminate`.** Both call `signalTarget`: parse,
  protection, `n <= 0`, then the identity: a non-known identity is an
  `UnverifiableError` and nothing is sent.

**Reaching the screen.** `PortsModel.refusal(pid, process, err)` is the one
classifier of every refused or failed stop: the press of `K` (the confirmation's
read), `Yes` (the table compared with the target) and the kill result all call
it. `errors.As` runs first for the three typed refusals; anything else goes
through `killFailureMessage` (`errors.Is`).

| Error | Footer | Level |
|---|---|---|
| `ProtectedError` | its own text | Warn |
| `IdentityChangedError` | `<name> (PID N) is no longer the process that was listed — table refreshed` (`PID N is …` when the name is empty), plus `requestPortsRefresh()` | Warn |
| `UnverifiableError` | its own text; the cause goes to the log | Warn |
| anything else | `killFailureMessage`: `Refused by the system — DevDesk cannot signal PID N`, `PID N is no longer running`, or the generic line | Error |

After an identity change the table is read once more, even when the tab is
paused (as `n` does), because the footer says it was refreshed; the pause stays
displayed.

**What is guaranteed, and what is not.**

- *Guaranteed*: on Linux with a pidfd, and on Windows within the assumptions
  below, no signal reaches a process other than the one whose identity was
  captured (at listing, or when the confirmation opened for a deferred row).
- *Residual windows, accepted and documented*:
  - **macOS, and Linux without a pidfd.** No handle on the process exists, so
    the comparison and the signal are two steps on a PID. `os.FindProcess` falls
    back to the bare PID, silently, on a kernel without the pidfd calls or where
    seccomp refuses them. The window is short, but it is there.
  - **Listing race (Linux, macOS).** The identity is read after the socket
    snapshot. If the holder exits and its PID is reused between the two, the row
    carries the newcomer's identity. That takes a full turn of `pid_max` within a
    few milliseconds. These stamps are not wall-clock times, so nothing compares
    them with an instant.
  - **Windows clock.** Anchoring to the listing instant assumes that
    `CreationTime` and `time.Now` read the same clock, with a granularity under
    100 ms (not verified). A wall clock set backwards
    between the listing and the read can make a younger process look older. A
    coarser granularity only produces cautious refusals (`bornAround`).
  - **Windows rows over the budget** are not greyed ahead of time when the system
    would refuse them: the refusal shows at the press of `K`. Reading PIDs in a
    stable order keeps the cut on the same PIDs from one refresh to the next.
  - **macOS `EIO`.** For a missing PID the kernel answers `sysctl kern.proc.pid`
    with no data, which `x/sys` reports as `EIO`. `readIdentity` reads `EIO`,
    `ESRCH`, `ENOENT` and a record with another `P_pid` as "process done"
    (not run on macOS).
- *Not run*: `identity_windows.go` and `identity_darwin.go` are compiled for
  their OS and read, never executed; no Windows or macOS machine was available.
  That `OpenProcess` with `PROCESS_QUERY_LIMITED_INFORMATION` and the handle of
  `os.FindProcess` both allow `GetProcessTimes` is the documented behaviour of
  the API, not checked here.
- *Cost on Windows: not measured* (as of 2026-11-23). `windowsIdentityBudget` is
  the story's starting value, not a measured threshold. To measure it, on a
  Windows machine **without elevation**, with a busy socket table (the story's
  case is 189 sockets):
  `DEVDESK_MEASURE_IDENTITY=1 go test -run TestMeasureIdentityCost -v ./internal/ports`
  (PowerShell: set `$env:DEVDESK_MEASURE_IDENTITY=1` first). It reads the table
  and every distinct PID five times with no budget, and logs the sockets and PIDs
  counted, the failures by kind (permission, gone, other), the min, median and
  max per round, and the cost of `List`. It never signals. Write the result in a
  dated note under `docs/measurements/` and adjust the constant if it
  contradicts the budget (D175). The task is DEVDESK-727.

**The target is captured (DEVDESK-714).** `confirmKill` copies the selected
row's PID and process name into `pendingKill` (a `killTarget`) in `Update`,
and builds the text of the confirmation from it. `handleConfirmYes` reads
`pendingKill`, never the table: the refresh keeps running during the
confirmation and the table's cursor stays on an index, so the selected row can
be another process by the time the user answers. Before the bug, `Yes` stopped
that other process. `handleConfirmNo` and `handleConfirmYes` both clear
`pendingKill` and the modal; a `Yes` with no pending target does nothing.
The target also carries its `Identity`: the one the row was listed with, or the
one `ops.confirm` returned when the confirmation opened (a refusal there shows
at once and opens no modal). `handleConfirmYes` then, in this order:

1. `targetPresence`, which compares the target with the last table received
   without a system call and has three outcomes. `present`: a row of the same
   PID has the same name and no conflicting identity. `replaced`: a row of the
   same PID has a known identity that differs from the target's; nothing is
   sent, and `refusal` says the PID is no longer the process that was listed and
   reads the table again. `absent`: no such row; nothing is sent and the footer
   warns `<name> (PID N) is no longer listed — not stopped`. The process may be
   gone, or its PID may belong to another process now; a refusal costs the user
   one keypress, a wrong signal costs a process. A deferred or unknown identity
   never conflicts here (`Conflicts` needs both known); the signal verifies it.
2. `ops.protection` on the captured PID: a target that became protected since
   the confirmation opened is refused with a warning naming the reason.
3. On Windows, the busy mark and `killProcessCmd(ops.kill, target, 0)`, with
   `kill` and the target copied as values (Rule 110). On Linux and macOS,
   `startStop(target)`, the first step of the two-step stop below.

A `ProtectedError`, an `IdentityChangedError` or an `UnverifiableError` that
comes back from `Kill` (the in-depth refusal) shows as a footer **warning**, not
an error: a precondition was not met, nothing failed (Rule 128).

**Two-step stop (DEVDESK-711, S-02b).** On Linux and macOS `K` asks for a clean
stop first and forces only after a second confirmation. `internal/ports` holds
stateless primitives; the state machine lives in `internal/ui/netdiag`
(`ports_stop.go`), because the wait must not block and `esc` must be able to end
it (Rule 110).

- **Primitives (`internal/ports`).** `Terminate(pid, Identity)` sends SIGTERM and
  returns at once; it waits for nothing. `Kill` is unchanged (SIGKILL,
  `TerminateProcess` on Windows). Both call `signalTarget` and then the single
  `procOS.kill(pid, want, sig)`, so protection, identity and the held process
  are the same for both signals. `Terminate` wraps its error `stopping process
  N: %w`; `refusal` reads it with `errors.As` and `errors.Is`, so the prefix
  never reaches the screen. On Windows `Terminate` answers `ErrNoCleanStop`,
  after the protection check; the UI never calls it there.
- **The probe sends nothing.** `Probe(pid, Identity)` reads the state and the
  identity stamp in one read (`readStatus`: `/proc/<pid>/stat` on Linux,
  `sysctl kern.proc.pid` on macOS) and decides in `livenessOf`:
  `ProcessExited` when no process holds the PID **or when another process does**
  (the stamp differs from the listed one: the listed process is gone),
  `ProcessZombie`, else `ProcessRunning`. On Linux `isLinuxZombie` decides: state
  `X` (dead) is a zombie; state `Z` is one **only if `Threads:` in
  `/proc/<pid>/status` is 1**. A process whose main thread has exited (state `Z`
  on the leader) while other threads still run is alive and stays `running`.
  `readStatus` reads `/proc/<pid>/status` (`readThreads`, `parseThreads`) only
  when the state is `Z`; a status file that cannot be read or parsed leaves the
  thread count unknown, and an unknown count reads as alive. A status file that
  is gone reads as exited. On macOS it is `P_stat == 5` (local constant
  `darwinSZOMB`, since `x/sys` exports none), unchanged.
  `Liveness.Stopped()` is true for the first two. A read that fails for another
  reason returns `ProcessRunning` with the error: an unreadable probe is never
  taken for a stop. On Windows and other platforms `Probe` returns
  `errors.ErrUnsupported`. The probe is not a signal, so
  `TestOnlyRealOSSendsASignal` does not apply to it.
- **The machine (`PortsModel.stop`, a `*pendingStop`; nil means no stop).**

  ```
  K, first Yes        SIGTERM result, no error          still running at the deadline
  signalling  ---------------------------> waiting ------------------------------> askingForce
     | error: no wait                         | exited or zombie: "stopped"             | Yes
     v                                        v                                          v
   (end)                                    (end)                                    forcing --> (end)
  ```

  `signalling`: `terminateCmd` is in flight, the row shows `Stopping nginx (812)`.
  `waiting`: the row shows `Waiting for nginx (812) to exit` and `stopProbeCmd`
  (a `tea.Tick` of `stopProbeEvery`, 250 ms) probes once per tick. `askingForce`:
  the row's busy mark is lifted and the `Force Kill` modal is open. `forcing`:
  `killProcessCmd` is in flight, the row shows `Killing nginx (812)`.
- **The clock.** `stopGrace` is 5 s, fixed (D176). The deadline is the instant
  `terminateCmd` returned plus `stopGrace` (`portsTermResultMsg.at`); each probe
  carries its tick instant (`portsStopProbeMsg.at`), and the model compares the
  two. No clock lives in the model, so tests choose the instants. A probe at or
  after the deadline that still reads running opens the second confirmation: at
  most about 21 probes per stop. An unreadable probe is logged
  (`ERROR [netdiag/ports] probe pid=N`) and counts as running. A zombie is
  counted as stopped (logged at `INFO`).
- **Generation.** Each stop takes `nextStopGen()`, a package-level atomic counter,
  so two netdiag views built one after the other never share one. Every message
  of a stop carries it. A message whose generation differs from `pm.stop.gen`,
  that arrives with no stop, or that arrives in a phase that does not expect it,
  is ignored: a tick that outlives its stop (after `esc`) touches neither the
  footer nor a stop started since.
- **Refused SIGTERM.** `portsTermResultMsg.err` ends the stop on the spot, with
  no wait, through `refusal` (the same classifier as before: `Refused by the
  system — DevDesk cannot signal PID N`, `PID N is no longer running`, the
  identity and protection warnings).
- **The second confirmation.** Opened by the deadline, never by a key, so it can
  open while the user is typing in the filter (`/`). `askForce` therefore does
  three things: it calls `FilterBar.EndSearch()` (the filter leaves edit mode and
  its text stays applied, as after `enter`), it opens the modal with
  `components.NewConfirmModal(...).WithoutYesShortcut()`, and it sets the Warn
  footer `nginx (812) did not exit within 5 s`. On this modal `y` and `Y` do
  nothing: a letter of a search the user was typing must not answer Yes to a
  SIGKILL. The focus starts on No; Yes takes `←` or `→` to move to the button,
  then `enter`. `n`, `N` and `esc` answer No. Every other `ConfirmModal` keeps
  its `y`/`Y` shortcut (opt-in, the zero value is unchanged). No answer
  forces anything. `No` and `esc` end the stop with the Info `nginx (812) left
  running — no SIGKILL sent`. `Yes` (`handleForceYes`) acts on the target
  captured at the first confirmation: a table that lists the PID under another
  identity (`replaced`) is refused with the identity-changed warning and no
  system call; a row **absent** from the table does not stop it (a process that
  is stopping often closes its sockets before it exits, which is the case this
  step is for), the identity is verified at the signal; the protection is
  recomputed. Then `kill` runs with the captured identity, the same check as
  SIGTERM.
- **The SIGKILL result is always announced.** `portsKillResultMsg.gen` is the
  generation of the stop it closes (0 for a Windows kill). A result of the
  current stop clears the busy mark and the stop; a stale one only gets its
  sentence. `os.ErrProcessDone` on a two-step result reads as Info
  `nginx (812) stopped` (the process left between the deadline and the `Yes`) and
  reads the table again; anything else goes through `refusal`; success is Info
  `Process 812 terminated`.
- **`esc` while waiting.** `cancelWait` ends the stop and says Info `Stopped
  waiting for nginx (812) — the stop request was already sent`. It signals
  nothing and does not take the SIGTERM back. Outside `waiting` it does
  nothing: during `signalling` and `forcing` there is nothing to cancel, and the
  modal of `askingForce` takes `esc` itself (as `No`).
- **One stop at a time.** `killable()` greys `K` on every row while `pm.stop` is
  set, after the reasons that belong to the row (protection, identity):
  `Another stop is already in progress`, or `This process is already being
  stopped` on the row of the stop. **This is an exception to Rule 130** (an
  operation in progress is not a cause of grey), imposed by the criterion that
  only one stop waits; it lasts at most the 5 s plus a modal, and pressing `K`
  puts the reason in the footer (D176). On Windows `K` stays lit on a busy row
  as before. `esc` is a control, greyed outside the wait (`Stop waiting`), and
  absent on Windows, so the set of keys on a platform does not change from one
  state to the next.
- **During the wait** the table keeps refreshing (its own tick chain; `space`
  stays the user's choice), every key answers, and the chain of probes is not
  suspended when the terminal loses focus (it is bounded by 5 s). A refresh does
  not touch `stop`. `Model.Running()` is true while a stop is pending, so the
  router does not rebuild a netdiag view opened from another view (`OriginView`)
  in the middle of one.
- **Windows is unchanged.** `pm.stop` is never set there: `handleConfirmYes`
  follows the S-02a path (`MarkBusy("Killing …")`, `killProcessCmd(kill, t, 0)`),
  selected by `processOps.windows`. No wait, no second confirmation, no new text.
- **Off screen (`netdiag.BackgroundMsg`).** `portsTermResultMsg`,
  `portsStopProbeMsg` and `portsKillResultMsg` implement `BackgroundMsg`
  (`netdiagBackground()`); no other type does
  (`TestOnlyNetdiagWorkIsBackgroundMessage`). `internal/app` routes them with
  `routeNetdiagWork`: to the active view if `:net` is on screen, otherwise to
  the netdiag view the router holds, through `routeToView`, without changing the
  current view and without creating the view if there is none. Without it the
  chain would stop when the user leaves `:net`, `K` would stay greyed and the
  row busy. This also fixes a kill result lost the same way before DEVDESK-711.
  See `app-shell.md`, « Cross-View Communication ».

**Wording per platform (DEVDESK-710, DEVDESK-711).** Windows has no signal to
send: the process is ended at once through `TerminateProcess`, and the
confirmation promises nothing clean. `processOps.windows` selects the texts. The
key is `K` everywhere.

| | Linux, macOS | Windows |
|---|---|---|
| `K` label in the header | `Stop process` | `Kill process` |
| Title | `Stop Process` | `Kill Process` |
| Body | `Ask <name> (PID <pid>) to stop?` + `Sends SIGTERM and waits up to 5 s. You can force it afterwards.` | `Terminate <name> (PID <pid>)?` + `Windows has no clean stop for this process: it is ended at once and cannot save anything.` |
| Row and footer | `Stopping <name> (<pid>)`, then `Waiting for <name> (<pid>) to exit` | `Killing <name> (<pid>)` |
| End | `<name> (<pid>) stopped` (it left within 5 s), `Process <pid> terminated` after a forced kill | `Process <pid> terminated` |
| Second confirmation | title `Force Kill`; `<name> (PID <pid>) did not exit within 5 s.` + `Force kill (SIGKILL)? It gets no chance to shut down cleanly.` | none |
| Help for `K` | `Ask the selected process to stop (SIGTERM), after confirmation; if it still runs 5 s later, offer SIGKILL (Ports tab)` | `End the selected process at once, after confirmation (Ports tab)` |
| `esc` | header `Stop waiting`, help `esc (Ports)` | none |

In the two Linux/macOS modals the process name is cut to 32 cells
(`stopNameWidth`), because `ConfirmModal` does not wrap and the lines must fit 80
columns. A name with no value reads `PID <pid>`. The Windows label stays free of
the word « Stop » (`TestWindowsLabelsPromiseNoCleanStop`): it must not suggest a
clean stop. A graceful close (`WM_CLOSE`) is a later story (S-04), not this one.

**Seams and the signal guard (DEVDESK-708, DEVDESK-709, DEVDESK-711).**
`internal/ports/procos.go` holds the seams the package reads about the host:
`selfPID`, `parentPID`, `goos`, `now` (the listing instant and the Windows
budget) and `procOS` (an `osProcs` with `createTime`, `identity`, `probe(pid)`
and `kill(pid, want, sig)`; production value `realOS{}`). Per OS,
`identity_linux.go`, `identity_windows.go`, `identity_darwin.go` and
`identity_other.go` hold `readStatus` (`readIdentity` is its start stamp),
`heldIdentity` and `isNoSuchProcess`; `readStatus` returns
`errors.ErrUnsupported` on Windows and other platforms. In `netdiag`,
`processOps` (`protection`, `confirm`, `terminate`, `probe`, `kill`, `windows`)
gathers what the tab asks of the system, wired by `realProcessOps()` to
`ports.ProtectionOf`, `ports.ConfirmIdentity`, `ports.Terminate`, `ports.Probe`,
`ports.Kill` and `runtime.GOOS == "windows"`; tests replace `PortsModel.ops`, so
no netdiag test can reach `ports.Kill`, `ports.Terminate` or `ports.Probe`. A test
builds an identity with `ports.IdentityForTest(raw)` or
`ports.DeferredIdentityForTest(listedAt)`; both panic outside a test binary.

`realOS.kill` is the **only** place in the package that sends a signal (SIGTERM
and SIGKILL both, chosen by its `sig` argument), and its
first statement is `refuseRealSignalUnderTest`: in a test binary
(`testing.Testing()`), a real signal **panics** unless the test called
`allowRealSignals(t)`. It panics rather than returning an error because an
error looks like an ordinary failed kill and the test could still pass. Two
tests hold the guard:

- `TestARealSignalInATestBinaryPanics` and
  `TestKillWithoutASubstitutedProcOSHitsTheGuard` run the panic.
- `TestOnlyRealOSSendsASignal` parses the non-test files of `internal/ports`
  and fails on a signalling call (`os.FindProcess`, `syscall.Kill`,
  `unix.Kill`, `windows.TerminateProcess`, a `.Kill(` or `.Signal(` method)
  outside `(realOS) kill`, or if the guard is not its first statement. It does
  not see an aliased import, another package or an external binary.

These tests send a real signal, each to a child the test started itself:
`TestKillEndsARealChildProcess` and `TestKillSparesARealChildWhoseIdentityDiffers`
(the child must survive: the identity differs, so nothing is sent), and, since
DEVDESK-711 (`internal/ports/stop_real_test.go`, Linux), `TestTerminateEndsARealChildProcess`,
`TestTerminateSparesARealChildWhoseIdentityDiffers`,
`TestTheTwoStepStopOnARealChildThatIgnoresSIGTERM` (a child that ignores SIGTERM
survives it and dies of the SIGKILL) and `TestKillAfterTerminateSparesAReusedPID`.
`TestProbeSeesARealZombie` signals nothing: it reads the `/proc` state of a child
that exited and was not yet waited for.
`TestKillOfAnExitedChildSendsNothing` calls `Kill` on a child that already
exited. They re-run the test binary as a child (`TestHelperProcessSleeps`), call
`allowRealSignals(t)` and wait for it. Never call
`allowRealSignals` in a test aimed at anything else. The seams are globals:
no `t.Parallel` in the package.

**Known limits.** S-01b (DEVDESK-709) closed the PID-reuse window on Linux with
a pidfd and on Windows with a handle. What stays, said plainly (the guarantees
and the cost measurement are under « Process identity »):

1. **macOS, and Linux without a pidfd** (kernel older than 5.4, or seccomp
   filtering `pidfd_open`). `os.FindProcess` silently falls back to the bare
   PID, so the comparison and the signal are two steps on a PID. The window is a
   few microseconds, and using it would need a whole turn of `pid_max` at an
   identical tick: impractical, not impossible.
2. **Identity read after the snapshot (Linux, macOS).** `attachIdentities` runs
   after the socket snapshot; a PID that changes hands in those few
   milliseconds carries the newcomer's identity.
3. **Windows clock set backwards.** `judgeBirth` compares a creation time with
   the listing instant (`windowsBirthSlack`, 100 ms). If the clock goes back
   between the listing and the birth of a process that took the PID, the
   newcomer can look older than the listing. It applies to both read paths; it
   matters mostly for a deferred row, where the gap lasts until the
   confirmation opens.
4. **`HOST_PROC`.** gopsutil pairs sockets and PIDs under that prefix, but
   `readIdentity` reads `/proc` as a literal path and the pidfd acts in
   DevDesk's own PID namespace. If `HOST_PROC` points at another namespace's
   `/proc`, the check agrees with itself and cannot see the mismatch. This
   predates S-01b.
5. **Windows inherited sockets.** A socket inherited by a child can keep the
   `OwningPid` of a parent that exited, with that PID reused before the listing.
   The identity is read from the PID as listed, so it matches the newcomer and
   the row is not caught. This predates S-01b.
6. **Windows and macOS code never ran.** `identity_windows.go` and
   `identity_darwin.go` are compiled and vetted, not executed. Unconfirmed
   assumptions: an unknown PID fails `OpenProcess` (and `os.FindProcess`) with
   `ERROR_INVALID_PARAMETER` on Windows; a missing PID answers `EIO`, or a record
   whose `P_pid` differs, on macOS.
7. **The two-step stop never ran on macOS** (DEVDESK-711). `readStatus` in
   `identity_darwin.go` is compiled and vetted, not executed. Not verified:
   that `sysctl kern.proc.pid` returns a zombie with `P_stat == SZOMB` (5, a
   constant copied from `<sys/proc.h>`, not from `x/sys`) and its `P_starttime`.
   If the kernel does not return it, the read means « gone »: the zombie reads as
   exited, and the stop counts as done either way. SIP may also refuse a SIGTERM
   (`EPERM`), which shows as `Refused by the system`.
8. **The second confirmation can wait out of sight.** The deadline can open the
   `Force Kill` modal while the user is on another tab or another view, since the
   messages reach netdiag off screen. Nothing is forced without the answer, but
   nothing says it is waiting; it shows when the user returns to the Ports tab,
   and `K` stays greyed until then (`Another stop is already in progress`).
9. **Rebuilding the view loses the wait.** `Running()` protects only a netdiag
   view opened from another view (`OriginView`). If the router rebuilds the view
   during a stop (a context switch, for instance), the wait and the second
   confirmation are lost without a word. The SIGTERM already sent is not taken
   back.
10. **PID reuse during the 5 s.** Between two probes the PID can change hands.
    `Probe` compares the identity stamp, so the listed process reads as exited
    and the footer says it stopped, which is true of the listed process. The
    SIGKILL re-verifies the identity at the signal, with the same residual window
    as any signal (limit 1). The identity is the one captured at the first
    confirmation, not re-read at the second.
11. **A row absent from the table can still be forced.** The second `Yes` does not
    require the row to be listed (a stopping process often closes its sockets
    first), so it relies on the identity check at the signal alone.

Also: the process name comes from the system and is not sanitised (DEVDESK-720).
S-02b adds it to four places: the first confirmation, the second confirmation,
the row's busy label and the footer lines (including the `did not exit` Warn);
the cut to 32 cells in the modals bounds the width, not the content.
The Linux zombie rule depends on `Threads:` being readable: if it is not, a
zombie reads as running and the second confirmation opens at the deadline
(the safe side). `targetPresence` trusts the last table received: a
paused or stale table is trusted as it stands, and the signal is what verifies.
`createTime` (milliseconds) still feeds the parent rule and `Identity` is a
second source; unifying them would change S-01a and is not done. When
`parentPID()` is 0 (PID namespace), the parent is not protected.

The platform error never reaches the screen — measured, PID 4 on this machine
answers `OpenProcess: Accès refusé.`, in the machine's own language, which is
the route stage's rule (§3.44). Also measured, and written down rather than
discovered: **the "already gone" branch does not fire on Windows**, where a
nonexistent PID fails `OpenProcess` with `ERROR_INVALID_PARAMETER` and maps to
neither sentinel. Mapping that code would be a guess, and the row disappears on
the next two-second refresh anyway.

**Nothing in the application runs `--network host` any more** (§3.47). §3.33
brought DNS, ICMP, TCP, TLS and HTTP into the process, §3.43 the socket table,
§3.44 the interfaces, and §3.47 removed the route trace — the last one. That is
the checkable form of "DevDesk never claims to answer for a machine that is not
yours", and it closes **D57**.

**The route trace was deleted rather than rewritten** (it came back in the
process with DEVDESK-327, see [The path trace](#the-path-trace--internalnettrace);
what follows is the history of §3.47), and the reasons are worth
keeping because they are the shape of the decision, not of this feature:

- it answered for the **wrong machine** — `--network host` traced from the Docker
  Desktop VM, so the hops were the VM's;
- what people came for has a **better answer**: `netcheck`'s Local route check
  names the interface and source address from *this* machine, in 2 ms, which is
  the split-tunnel question;
- rewriting it was **three unmeasured unknowns** — reading ICMP `TIME_EXCEEDED`
  without a raw socket on three platforms, a per-packet TTL `pro-bing` does not
  expose, and a TCP mode needing the ICMP error of an outgoing connection.

`H` went back into `keymap.Free()` beside `J`, `Q` and `Z`: a letter an action
has just released is redeclared free, or it stays reserved for something that no
longer exists.

The OCI connectivity test — the last container DevDesk started from a
network-inspect overlay, and with it `network.connectivity_image` and its
three-link migration chain (`docker.network_tool_image` →
`network.tool_image` → `network.connectivity_image`) — has been removed. The
network-inspect overlay itself (`:oci` → Networks → `enter`) stays; only the
`c` key it used to offer, and everything behind it, is gone.

## The port forwarder — `internal/forward`

A local TCP port redirected to a `host:port`, **in this process** and with no
privilege: a `net.Listen` and two `io.Copy`. `Registry.Open(port, target)`,
`Toggle(id)`, `Close(id)`, `CloseAll()`, `List()`, `Restore(entries)` and
`Entries()` are the whole interface; the messages a view exchanges with the router live in the same package so a view
never imports `internal/app`.

**The router owns the registry** (`shared.State.Forwards`, built once in
`newWithSize`). `reinitializeViews` drops every view on a config save and on a
context switch, so a listener held by the Forward tab would stay bound with
nothing left to close it. The tab asks (`forward.Open` / `Close` / `Refresh`) and
is told (`forward.ChangedMsg`, broadcast to every held view, on screen or not).
Forwards **survive a context switch** — one belongs to no context.

### Persistence (§3.75)

The **listener** cannot outlive the process, but the **intent** can: every
forward is written to `~/.devdesk/forwards.yaml` and reopened at the next launch.
It is a file of its own rather than a key of `config.yaml`, because the config
is per context and is reloaded on every switch, and a forward belongs to no
context.

| Piece | Where | Does |
|---|---|---|
| `Store` (`store.go`) | `internal/forward` | `Load` / `Save` the `[]Entry` (`local_port`, `target`, `paused`), `version: 1` |
| `Registry.Entries()` | `internal/forward` | describes what is *wanted*, in creation order, live or not |
| `Registry.Restore()` | `internal/forward` | reopens the entries at startup, binds in parallel |
| `restoreForwardsCmd` / `saveForwardsCmd` | `internal/app/forward.go` | the router's two Cmds; the list is copied in `Update` (Rule 110) |

A forward has a `State`:

| State | Meaning | In the file |
|---|---|---|
| `StateLive` | listener bound | an entry |
| `StatePaused` | the user stopped it; port released, row kept | `paused: true` |
| `StateUnbound` | wanted, not bound — port taken or target silent at the last attempt; `LastErr` says which | an entry (retried at launch) |

- **A refusal at creation deletes nothing; a failure at launch keeps the entry.**
  `Open` still refuses a typo, opens no row and writes nothing — the form's
  business. `Restore` answers a service that is not up yet, or a port some other
  process holds today, and forgetting the route for that would make the file
  lose things by being started at the wrong moment. The row reads unbound and
  `space` tries again.
- **`space` is Pause/Resume** (Rule 111's *Control* key), not a new capital. A
  resume is a bind like any other: probe, then listen. A failure is returned and
  recorded, so the row and the footer say the same thing.
- **`K` deletes**: the row and its entry, after a confirmation that says it will
  not come back.
- **Order is the creation order** (`entry.seq`), not the last bind and not the ID
  as text — `"10"` sorts before `"2"`, and a restored file creates its rows in
  the same instant. A row does not move when it is paused and resumed.
- **`serve` is handed its listener.** A pause clears `entry.ln`; an accept loop
  that read the field would be the goroutine dereferencing it.
- **The write is atomic** (temporary file, `Sync`, `Rename`) and serialised by a
  mutex: two `Cmd`s can be in flight, and the older list must not land last.
- **An unreadable file is refused, not treated as empty.** `Load` returns
  `ErrUnreadable` naming the file and touches nothing; the first save afterwards
  moves it to `forwards.yaml.unreadable` instead of overwriting it.
- **A test never writes the developer's home.** `newWithSize` sets no store;
  `New` calls `useForwardStore`, the way it calls `useSecrets`.

**Two instances share the file.** `~/.devdesk/` is shared, so two DevDesks write
`forwards.yaml` and the last write wins; the second one's reopening then finds
the first one's ports taken and reads unbound rather than failing. There is no
file lock — the atomic write keeps the file whole, not merged.

### Named routes (§3.74)

A forward with a **name** is an HTTP route instead of a TCP redirection. One
`http.Server` on `127.0.0.1:network.proxy_port` serves every route, and the
request's `Host` header picks the target: `http://api.localhost:8080` and
`http://app.localhost:8080` differ by name, not by port. No DNS entry, no
privilege — `*.localhost` is reserved for the loopback (RFC 6761), and the
platforms measured resolve it unaided.

| Piece | Where | Does |
|---|---|---|
| `proxy.go` | `internal/forward` | `OpenRoute`, `SetProxyPort`, the proxy's lifecycle, `serveHTTP` |
| `Entry.Name` | `store.go` | a route is an entry with a name and no `local_port`; the type is *deduced from the name*, so no state has a route without one |
| `network.proxy_port` | `internal/config` | the port, 8080 by default, per context (see below) |
| the form's `Type` field | `forward_form.go` | `TCP` / `HTTP`, a cycle field (Rule 132); only the fields that apply are shown |

- **The proxy binds with the first served route and closes with the last.** Its
  lifecycle is serialised by `proxyMu`; lock order is `proxyMu` then `mu`, and the
  request handler takes `mu` only, so serving never waits on a bind.
- **`Host` is matched without its port, trailing dot or case.** An unknown host,
  or bare `localhost`, gets a 404 that lists what *is* served. A target that does
  not answer gets a 502 naming it, and the route's `LastErr` records why.
- **The inbound `Host` is forwarded to the backend**, not replaced by the
  target's: an application behind a named route often keys on it. `WebSocket`
  upgrades pass (`httputil.ReverseProxy` handles them), which is what hot reload
  rides on.
- **A name must be a valid host name ending in `.localhost`**, unique whatever
  the state of the route that owns it — a paused route keeps its name, or
  resuming it would find it gone. Stored in lower case. No suffix completion in
  the form: what is typed is what is stored, and a name that does not end in
  `.localhost` is refused there before it reaches the registry.
- **A route has no port of its own.** `List` fills `LocalPort` with the proxy's,
  which is the number that goes in the URL. A TCP forward asking for that port is
  refused (`ErrProxyPortTaken`), and the proxy's own bind failing is
  `ErrProxyPortInUse` — not `ErrPortInUse`, whose sentence says to pick another
  port in a field a route does not have.
- **The proxy's `Server` and its listener are closed separately.** `Server.Close`
  reaches the listener only once `Serve` has registered it, on a goroutine; the
  registry closes the listener itself so that the port is free when the stop
  returns and a pause-then-resume can rebind at once.
- **The port follows the configuration.** It is per context, and a forward
  belongs to none, so a switch that changes it moves the proxy: `syncProxyPortCmd`
  runs on a context switch and on a saved configuration. It records the wanted
  port in `App.wantedProxyPort` in `Update` and the `Cmd` applies the value as it
  is *when it runs* — two moves in flight leave the proxy on the latest port
  whatever order they finish in. If the new port cannot be bound, every live
  route becomes unbound with the reason and nothing leaves the file.
- **Restore probes the targets, then binds the proxy once**, and only if a route
  passed: a file whose routes are all silent binds nothing.
- **Restore holds the file to the rules of `OpenRoute`.** A name is normalized
  (lower case) and validated, and a second entry for a name is refused — the
  file can be edited by hand, and a name that was never normalized would read
  live and never match a request. A refused entry is kept, unbound, with the
  reason, and is not rewritten. Resuming a paused route also refuses a name a
  live route already answers to.
- **A new proxy port retries the routes a bind failure left unbound.** Without
  it, correcting a taken `network.proxy_port` would report success and leave
  every route unbound with an error naming the port just replaced.
- **A resume marks the route live under `proxyMu`**, the lock a pause takes to
  decide the proxy is idle. Released in between, a pause of the last live route
  could close the proxy under a route that then read live with no listener. A
  test holds a resume in that window through an unexported hook and fails when
  the lock is released early.
- **A client that goes away is not the route failing.** A cancelled request does
  not set `LastErr`.

**A known race, left as it is.** `bindTCP` checks the proxy's port under `mu`,
while the proxy binds under `proxyMu`, and neither holds a lock across the other.
A TCP forward asked for the proxy's port at the very moment the first route binds
it can therefore lose the race and be refused with a generic port-in-use
sentence instead of the one that names `network.proxy_port`. Nothing is lost and
the window is a single `net.Listen`; closing it would mean holding `proxyMu`
across every TCP bind, which is worse than the message.

**Limits, stated rather than discovered.** HTTP only — `https://app.localhost`
needs a certificate the browser trusts, which is one more elevation. The port
stays in the URL (80 is privileged). A name outside `.localhost` does not
resolve. A client with its own resolver is not covered. The proxy adds no CORS
header: it makes nothing reachable that was not already on the loopback.

**Not yet measured:** browsers on Windows and macOS, and the `::1`-then-IPv4
fallback (the proxy binds IPv4 only). See §3.74.

Four decisions, each with a test:

- **Loopback only.** A forward binds `127.0.0.1`. `0.0.0.0` would put on the LAN a
  service its owner kept off it. The test asserts the listener's own address: a
  second bind cannot prove it, since `0.0.0.0` and `127.0.0.1` collide on a port
  in both directions.
- **Below 1024 is refused before the syscall**, and before the target is probed.
  There is no unprivileged way around it on Unix, and `permission denied` reads
  like something a retry would fix.
- **The target is dialled once before the port opens.** Otherwise the bind
  succeeds, the row reads healthy, and the failure only surfaces at the first
  client. Refusals are sentinels (`ErrPrivilegedPort`, `ErrPortInUse`,
  `ErrTargetUnreachable`), and `app.forwardRefusal` maps each to a sentence —
  the wording never comes from the operating system.
- **The registry has a mutex; `jobs.Registry` deliberately does not.** `jobs` is
  only written from `Update`. Here the accept and connection goroutines write the
  counters `Update` reads. The lock is never held across I/O, and a test closes a
  forward while a connection is live.

**A container's own address is not a target on Docker Desktop.** Measured on
Windows: `172.17.0.3:80` for an unpublished `nginx` gives `i/o timeout`. The
address is real inside the Linux VM and routes nowhere outside it — the same
mechanism as D55 for the Ports tab. It works on native Linux. The probe is what
turns that into a named refusal instead of a healthy-looking dead row, and it is
why the container pre-fill was not built (§3.1).

A forward records its local port and its target, and nothing about where the
target came from. It briefly carried a `Label` for a container name, with a
`Source` column to show it; both were removed because the pre-fill that would
have filled them was not built, so the column only ever read `-`.

## The interfaces — `internal/netiface`

This machine's network interfaces, read **in this process**. `List(ctx)` is the
whole interface.

**It exists because the old one answered for the wrong machine** (D57), and it
is `internal/ports`' story exactly one screen over. The Topology tab ran
`ip addr`, `ip -s link`, `ip route`, `ip neigh` and `iptables` in containers
started with `--network host`, which on Docker Desktop is the Linux VM's
namespace: the tab showed `eth0 10.254.254.3`, `docker0` and the `br-*` while
the machine had `Ethernet 2`, ProtonVPN, Tailscale and **two competing default
routes**. Not one interface and not one route in common. Under Linux the defect
did not exist, which is why it went unnoticed.

No new dependency: `net.Interfaces()` is the standard library and
`gopsutil/v4/net` was already imported. Measured here: 10 interfaces in 4,6 ms,
10 counter rows in 2,6 ms, and **0 of 10 interfaces without a matching counter
row** — the names agree character for character, so there is no correspondence
table to keep.

**The addresses are split by family, one column each** (§3.49). The split
happens in `List`, where each address is still a `net.IP` and the family is a
fact — `To4()` answers for an IPv4-mapped address as well as for a plain one,
which is right, it *is* an IPv4 address. A view splitting `AddressList()` again
would be parsing text this package produced, and would have to decide what an
unparseable entry means: a question that only exists once the type has been
thrown away. Hence `IPv4 []string` and `IPv6 []string` rather than one
`Addresses`.

Two consequences in the view:

- **Both columns follow their content and both carry a flex weight.** The flex
  is what levels them against each other when there is a shortfall — a table
  reclaims from the widest content column first — and with the flex on IPv6
  alone it absorbed the whole shortfall and rendered at **zero width, header
  included, from about 100 columns down**.
- **The cliff below 88 columns is gone** (§3.45). It was written down here
  rather than fixed, because `MinWidth` was an ask and giving the solver a floor
  would have changed every table in the application; §3.45 changed every table
  in the application. What gives way now is MTU, then MAC, then the two
  counters — the columns that declare `Optional` — and each is removed whole, so
  it hands back its padding as well as its width. At 80 columns the addresses
  get 18 and 29 cells instead of nothing.

Three decisions, each with a test:

- **`RxErrors` and `TxErrors` are `*uint64`.** The counters come from a second
  source that fails on its own, and a zero written because nobody looked is
  indistinguishable from an interface that has dropped nothing. `nil` renders as
  `-`, never `0` — D58 at the scale of a column, and `SecretVerdict`'s `*bool`
  under another name.
- **A counter failure does not fail the listing.** `List` returns an error only
  when the interfaces themselves could not be read: a list without its error
  counts is still the answer to "which adapters does this machine have".
- **An MTU the platform does not report is withheld.** Windows returns `-1` for
  its loopback pseudo-interface where `ip` returns 65536; `HasMTU` is what keeps
  a cell from printing a number the machine never meant.

**Three sections were removed rather than translated**, and §3.44 measures why
for each: the ARP cache loses IPv6 and four of its six states outside netlink,
the firewall's `{Name, Policy, Rules}` is an iptables shape that Windows profiles
and `pf` do not fit, and the routing *table* became a routing *question* — see
the `route` stage below.

**The tab is named after what it shows.** At one section "Topology" described
nothing, and deleting it would have meant rehousing the interfaces in Diagnostics
or Ports, where neither has room. One consequence has a test: the table has a
search box where the old tab had no input at all, so `InEditMode()` had to stop
returning a hardcoded `false` — otherwise a `:` typed into the query opens the
command line (Rule 111).

