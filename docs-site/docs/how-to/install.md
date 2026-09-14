# Install DevDesk

## Requirements

- Go 1.25.5+
- A terminal with [Nerd Font](https://www.nerdfonts.com/) support — DevDesk
  uses icons throughout the UI; without one, boxes or missing glyphs appear
  instead of icons.
- Optional, and only if you want the feature: [Trivy](https://trivy.dev/)
  (CVE/misconfiguration scanning), [Gitleaks](https://github.com/gitleaks/gitleaks)
  (secret detection), Docker **or** Podman (container management, image
  scanning — see [Container engine](../explanation/network.md#container-engine-docker-or-podman)
  for how DevDesk picks between them). DevDesk degrades gracefully when any
  of these is absent — the views that need them say so rather than failing.

## Build from source

```bash
git clone https://github.com/anthnel/devdesk.git
cd devdesk

mise run build      # → bin/dk
./bin/dk
```

No `mise`? Plain Go works too:

```bash
go build -o bin/dk .
```

## Install to your `$GOPATH/bin`

```bash
mise run install    # → dk, on your PATH if $GOPATH/bin is
```

## Verify the build

```bash
./bin/dk
```

then, inside the app, `:about` (or `:version`) — it reports the exact
version, commit and build date that were stamped into the binary at build
time.

## Create your first context

`dk setup` walks you through creating a context — see
[Run the setup wizard](run-the-setup-wizard.md). Running `dk` with no
context yet also works; it starts with the defaults from
[Configure a context](configure-a-context.md).

!!! tip "Windows"
    Tasks that use POSIX shell syntax declare `shell = "bash -c"` in
    `mise.toml`, so they need Git Bash — it ships with
    [Git for Windows](https://gitforwindows.org/).
