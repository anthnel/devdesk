package docker

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// OC-20 (DEVDESK-818): the launch refuses to be silent about a bind mount that
// hands the container something sensitive of the host. The classification is a
// pure function, kept apart from the form, so that one rule serves the modal
// and anything else that launches (a future MCP tool, a re-run).
//
// Expected production API, pinned by these tests (new file launch_mounts.go):
//
//	type MountCategory string
//	const (
//	    MountRoot         MountCategory = "root"          // "/"
//	    MountSystemConfig MountCategory = "system-config" // "/etc" and what is under it
//	    MountHome         MountCategory = "home"          // $HOME itself
//	    MountSSH          MountCategory = "ssh"           // ~/.ssh and what is under it
//	    MountDevDesk      MountCategory = "devdesk"       // ~/.devdesk and what is under it
//	    MountEngineSocket MountCategory = "engine-socket"
//	)
//
//	type SensitiveMount struct {
//	    Spec         string        // the whole entry as typed: "~/.ssh:/root/.ssh:ro"
//	    Source       string        // its host side as typed: "~/.ssh"
//	    Path         string        // the host path after expansion, normalisation and symlink resolution
//	    Category     MountCategory
//	    Reason       string        // one sentence, English US, fixed per category, never the typed text
//	    EngineSocket bool          // true exactly for MountEngineSocket
//	}
//
//	func SensitiveMounts(opts ContainerLaunchOptions) []SensitiveMount
//	    reads opts.Volumes (the -v form) and opts.Mounts (the --mount form, a new
//	    field: the value of each --mount, "type=bind,source=...,target=..."),
//	    volumes first, in the order given. It returns one element per sensitive
//	    bind mount and nil when there is none. It reads HOME (USERPROFILE is set by
//	    the tests too), XDG_RUNTIME_DIR, DOCKER_HOST and the working directory at
//	    the time of the call, and the file system for symlinks; it never starts
//	    anything and never fails (an unreadable path is compared as written, after
//	    normalisation).
//
// Decisions the tests rely on:
//
//   - Only a path is a bind source: it starts with "/", "." or "~" (so ".ssh" is
//     a path, as the Docker CLI reads it). "myvolume:/data" is a named volume, "/data" alone is an
//     anonymous volume (the one path is the container's), and neither is checked.
//   - A leading "~" is expanded to HOME even though the engine would not do it: the
//     hint a user typed is read the way they meant it (conservative).
//   - A relative source is taken from the working directory of DevDesk.
//   - The path is resolved before it is compared, and ".." after a link is
//     followed means the parent of the link target, as the kernel does: the
//     implementation must not Clean() the raw path first (see
//     TestSensitiveMountsResolvesLinksBeforeNormalising).
//   - Comparison is by whole path components: "/etcetera" and "$HOME/.sshx" are
//     not "/etc" and "~/.ssh".
//   - "$HOME/projects/app" is under HOME and is not sensitive: only HOME itself is.
//   - Linux paths only. Windows and macOS are not covered here.

// mountEnv is a private HOME, working directory, XDG_RUNTIME_DIR and scratch
// directory, all resolved (t.TempDir may itself sit under a link).
type mountEnv struct{ home, cwd, xdg, tmp string }

func newMountEnv(t *testing.T) mountEnv {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("these cases name Linux host paths (/etc, /var/run/docker.sock)")
	}
	resolve := func(p string) string {
		r, err := filepath.EvalSymlinks(p)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	e := mountEnv{
		home: resolve(t.TempDir()), cwd: resolve(t.TempDir()),
		xdg: resolve(t.TempDir()), tmp: resolve(t.TempDir()),
	}
	for _, d := range []string{".ssh", ".devdesk", "projects"} {
		if err := os.MkdirAll(filepath.Join(e.home, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", e.home)
	t.Setenv("USERPROFILE", e.home)
	t.Setenv("XDG_RUNTIME_DIR", e.xdg)
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("CONTAINER_HOST", "")
	t.Chdir(e.cwd)
	return e
}

// expand fills the placeholders of a case.
func (e mountEnv) expand(s string) string {
	return strings.NewReplacer("{home}", e.home, "{cwd}", e.cwd, "{xdg}", e.xdg, "{tmp}", e.tmp).Replace(s)
}

func (e mountEnv) expandAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = e.expand(s)
	}
	return out
}

type wantMount struct {
	path     string
	category MountCategory
	socket   bool
}

func checkMounts(t *testing.T, got []SensitiveMount, want []wantMount, e mountEnv) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("SensitiveMounts() = %+v, want %d sensitive mount(s) %+v", got, len(want), want)
	}
	for i, w := range want {
		g := got[i]
		// The expected path is itself resolved when it exists: on a host where
		// /var/run is a link to /run, the docker socket resolves to /run/docker.sock.
		wantPath := e.expand(w.path)
		if r, err := filepath.EvalSymlinks(wantPath); err == nil {
			wantPath = r
		}
		if g.Path != wantPath {
			t.Errorf("mount %d: Path = %q, want %q", i, g.Path, wantPath)
		}
		if g.Category != w.category {
			t.Errorf("mount %d: Category = %q, want %q", i, g.Category, w.category)
		}
		if g.EngineSocket != w.socket {
			t.Errorf("mount %d: EngineSocket = %v, want %v", i, g.EngineSocket, w.socket)
		}
		if g.Reason == "" || g.Spec == "" || g.Source == "" {
			t.Errorf("mount %d: Reason, Spec and Source must all be filled, got %+v", i, g)
		}
	}
}

// Criteria 1, 3, 4, 5, the -v form: what is sensitive, what is not, and the
// boundaries between them.
func TestSensitiveMountsClassifiesTheVolumeForm(t *testing.T) {
	tests := []struct {
		name    string
		volumes []string
		want    []wantMount
	}{
		// Criterion 1: the listed paths, equal.
		{"the root", []string{"/:/host"}, []wantMount{{"/", MountRoot, false}}},
		{"the root, read-only", []string{"/:/host:ro"}, []wantMount{{"/", MountRoot, false}}},
		{"/etc", []string{"/etc:/host-etc:ro"}, []wantMount{{"/etc", MountSystemConfig, false}}},
		{"HOME itself", []string{"{home}:/h"}, []wantMount{{"{home}", MountHome, false}}},
		{"~/.ssh", []string{"{home}/.ssh:/root/.ssh:ro"}, []wantMount{{"{home}/.ssh", MountSSH, false}}},
		{"~/.devdesk", []string{"{home}/.devdesk:/d"}, []wantMount{{"{home}/.devdesk", MountDevDesk, false}}},
		// Criterion 1: under /etc, ~/.ssh and ~/.devdesk.
		{"under /etc", []string{"/etc/ssl/certs:/certs"}, []wantMount{{"/etc/ssl/certs", MountSystemConfig, false}}},
		{"under ~/.ssh", []string{"{home}/.ssh/keys:/k"}, []wantMount{{"{home}/.ssh/keys", MountSSH, false}}},
		{"a file under ~/.devdesk", []string{"{home}/.devdesk/config.yaml:/c"}, []wantMount{{"{home}/.devdesk/config.yaml", MountDevDesk, false}}},
		// A leading ~ is read as HOME.
		{"~ alone", []string{"~:/h"}, []wantMount{{"{home}", MountHome, false}}},
		{"~/.ssh", []string{"~/.ssh:/root/.ssh"}, []wantMount{{"{home}/.ssh", MountSSH, false}}},

		// Criterion 3: normalised before the comparison.
		{"dot-dot into /etc", []string{"/tmp/../etc:/x"}, []wantMount{{"/etc", MountSystemConfig, false}}},
		// /usr, not /tmp: the private HOME of these tests is under /tmp, and an ancestor of a sensitive path is sensitive (review 8).
		{"dot-dot out of /etc", []string{"/etc/../usr:/x"}, nil},
		{"dot-dot into ~/.ssh", []string{"{home}/projects/../.ssh:/x"}, []wantMount{{"{home}/.ssh", MountSSH, false}}},
		{"tilde and dot-dot, nothing sensitive", []string{"~/../x:/x"}, nil},
		{"a doubled slash and a dot", []string{"//etc/./ssl:/x"}, []wantMount{{"/etc/ssl", MountSystemConfig, false}}},
		{"a trailing slash", []string{"/etc/:/x"}, []wantMount{{"/etc", MountSystemConfig, false}}},

		// Relative sources are taken from the working directory (the cwd is neither
		// HOME nor under it here).
		{"./data, no link with anything", []string{"./data:/data"}, nil},

		// Criterion 4: nothing to say.
		{"under HOME is not HOME", []string{"{home}/projects/app:/app"}, nil},
		{"a named volume", []string{"myvolume:/app/data"}, nil},
		{"a named volume with options", []string{"pgdata:/var/lib/postgresql/data:rw"}, nil},
		{"an anonymous volume is a container path", []string{"/etc"}, nil},
		{"an anonymous volume of another path", []string{"/var/lib/data"}, nil},
		{"a blank entry", []string{"", "  "}, nil},
		{"no entry", nil, nil},

		// Whole components only.
		{"/etcetera is not /etc", []string{"/etcetera:/x"}, nil},
		{"/etc-backup is not /etc", []string{"/etc-backup:/x"}, nil},
		{"~/.sshx is not ~/.ssh", []string{"{home}/.sshx:/x"}, nil},
		{"~/.devdesk-old is not ~/.devdesk", []string{"{home}/.devdesk-old:/x"}, nil},
		{"HOME's sibling is not HOME", []string{"{home}-other:/x"}, nil},

		// Several entries: each sensitive one is reported, in order, the rest skipped.
		{"three entries, two sensitive", []string{"/etc:/a", "data:/b", "{home}/.ssh:/c"}, []wantMount{
			{"/etc", MountSystemConfig, false}, {"{home}/.ssh", MountSSH, false},
		}},
		{"/etc and a path under it are two mounts", []string{"/etc:/a", "/etc/ssl:/b"}, []wantMount{
			{"/etc", MountSystemConfig, false}, {"/etc/ssl", MountSystemConfig, false},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newMountEnv(t)
			got := SensitiveMounts(ContainerLaunchOptions{Image: "img", Volumes: e.expandAll(tt.volumes)})
			checkMounts(t, got, tt.want, e)
		})
	}
}

// A relative source that is HOME or a sensitive child of it, from a working
// directory that is HOME: the dot forms are paths too.
func TestSensitiveMountsReadsRelativeSourcesFromTheWorkingDirectory(t *testing.T) {
	tests := []struct {
		name    string
		volume  string
		want    []wantMount
		cwdHome bool
	}{
		{"dot is HOME when the cwd is HOME", ".:/x", []wantMount{{"{home}", MountHome, false}}, true},
		{"./.ssh when the cwd is HOME", "./.ssh:/x", []wantMount{{"{home}/.ssh", MountSSH, false}}, true},
		{"./projects when the cwd is HOME", "./projects:/x", nil, true},
		{"dot is not sensitive elsewhere", ".:/x", nil, false},
		{"dot-dot of a child of HOME is HOME", "..:/x", []wantMount{{"{home}", MountHome, false}}, false}, // cwd set below
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newMountEnv(t)
			switch {
			case tt.cwdHome:
				t.Chdir(e.home)
			case strings.HasPrefix(tt.volume, ".."):
				t.Chdir(filepath.Join(e.home, "projects"))
			}
			got := SensitiveMounts(ContainerLaunchOptions{Image: "img", Volumes: []string{tt.volume}})
			checkMounts(t, got, tt.want, e)
		})
	}
}

// Criterion 5, the --mount form: source and src, target and dst and destination
// (the container side, never checked), readonly and ro, any order of the keys.
func TestSensitiveMountsClassifiesTheMountForm(t *testing.T) {
	tests := []struct {
		name   string
		mounts []string
		want   []wantMount
	}{
		{"bind /etc", []string{"type=bind,source=/etc,target=/x"}, []wantMount{{"/etc", MountSystemConfig, false}}},
		{"src and dst, readonly", []string{"type=bind,src=/etc,dst=/x,readonly"}, []wantMount{{"/etc", MountSystemConfig, false}}},
		{"src and destination, ro", []string{"type=bind,src=/etc,destination=/x,ro"}, []wantMount{{"/etc", MountSystemConfig, false}}},
		{"readonly=true", []string{"type=bind,source=/etc,target=/x,readonly=true"}, []wantMount{{"/etc", MountSystemConfig, false}}},
		{"keys in another order", []string{"target=/x,source=/,type=bind"}, []wantMount{{"/", MountRoot, false}}},
		{"source first", []string{"source={home}/.ssh,target=/root/.ssh,type=bind,readonly"}, []wantMount{{"{home}/.ssh", MountSSH, false}}},
		{"under /etc", []string{"type=bind,source=/etc/ssl,target=/x"}, []wantMount{{"/etc/ssl", MountSystemConfig, false}}},
		{"HOME", []string{"type=bind,source={home},target=/h"}, []wantMount{{"{home}", MountHome, false}}},
		{"~/.devdesk", []string{"type=bind,source=~/.devdesk,target=/d"}, []wantMount{{"{home}/.devdesk", MountDevDesk, false}}},
		{"normalised", []string{"type=bind,source=/tmp/../etc,target=/x"}, []wantMount{{"/etc", MountSystemConfig, false}}},
		{"two entries", []string{"type=bind,source=/etc,target=/a", "type=bind,source=/,target=/b"}, []wantMount{
			{"/etc", MountSystemConfig, false}, {"/", MountRoot, false},
		}},

		// Not a host path, or not a bind.
		{"a harmless bind", []string{"type=bind,source={home}/projects/app,target=/app"}, nil},
		{"a relative harmless bind", []string{"type=bind,source=./data,target=/data"}, nil},
		{"the target is the container's: /etc there is fine", []string{"type=bind,source={home}/projects,target=/etc"}, nil},
		{"a named volume", []string{"type=volume,source=pgdata,target=/var/lib/postgresql"}, nil},
		{"a named volume mounted over /etc", []string{"type=volume,source=pgdata,target=/etc"}, nil},
		{"tmpfs", []string{"type=tmpfs,target=/etc"}, nil},
		{"tmpfs with options", []string{"type=tmpfs,destination=/run,tmpfs-size=64m"}, nil},
		{"a bind with no source", []string{"type=bind,target=/x"}, nil},
		{"only a type", []string{"type=bind"}, nil},
		{"a blank value", []string{"", "  "}, nil},
		{"no value", nil, nil},
		{"garbage", []string{"source", ",,,", "=", "type=bind,source"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newMountEnv(t)
			got := SensitiveMounts(ContainerLaunchOptions{Image: "img", Mounts: e.expandAll(tt.mounts)})
			checkMounts(t, got, tt.want, e)
		})
	}
}

// Both forms in one request: volumes first, then mounts, each in the order given.
func TestSensitiveMountsReadsBothFormsOfOneRequest(t *testing.T) {
	e := newMountEnv(t)
	got := SensitiveMounts(ContainerLaunchOptions{
		Image:   "img",
		Volumes: []string{"/etc:/a", "data:/b"},
		Mounts:  []string{"type=bind,source=/,target=/c", "type=volume,source=v,target=/d"},
	})
	checkMounts(t, got, []wantMount{
		{"/etc", MountSystemConfig, false},
		{"/", MountRoot, false},
	}, e)
	if got[0].Spec != "/etc:/a" || got[0].Source != "/etc" {
		t.Errorf("-v: Spec = %q, Source = %q, want the entry and its host side as typed", got[0].Spec, got[0].Source)
	}
	if got[1].Spec != "type=bind,source=/,target=/c" || got[1].Source != "/" {
		t.Errorf("--mount: Spec = %q, Source = %q, want the value and its source as typed", got[1].Spec, got[1].Source)
	}
}

// Criterion 2: the engine sockets, whatever the form.
func TestSensitiveMountsFlagsTheEngineSockets(t *testing.T) {
	tests := []struct {
		name       string
		dockerHost string
		volumes    []string
		mounts     []string
		want       []wantMount
	}{
		{"docker.sock under /var/run", "", []string{"/var/run/docker.sock:/var/run/docker.sock"}, nil,
			[]wantMount{{"/var/run/docker.sock", MountEngineSocket, true}}},
		{"docker.sock under /run", "", []string{"/run/docker.sock:/var/run/docker.sock"}, nil,
			[]wantMount{{"/run/docker.sock", MountEngineSocket, true}}},
		{"docker.sock read-only is still the engine", "", []string{"/var/run/docker.sock:/var/run/docker.sock:ro"}, nil,
			[]wantMount{{"/var/run/docker.sock", MountEngineSocket, true}}},
		{"the rootful podman socket", "", []string{"/run/podman/podman.sock:/run/podman/podman.sock"}, nil,
			[]wantMount{{"/run/podman/podman.sock", MountEngineSocket, true}}},
		{"the rootless podman socket of XDG_RUNTIME_DIR", "", []string{"{xdg}/podman/podman.sock:/x"}, nil,
			[]wantMount{{"{xdg}/podman/podman.sock", MountEngineSocket, true}}},
		{"the rootless docker socket of XDG_RUNTIME_DIR", "", []string{"{xdg}/docker.sock:/x"}, nil,
			[]wantMount{{"{xdg}/docker.sock", MountEngineSocket, true}}},
		{"the socket of DOCKER_HOST", "unix://{tmp}/engine.sock", []string{"{tmp}/engine.sock:/x"}, nil,
			[]wantMount{{"{tmp}/engine.sock", MountEngineSocket, true}}},
		{"DOCKER_HOST with a unix:// path that has no file yet", "unix://{tmp}/not-there.sock", []string{"{tmp}/not-there.sock:/x"}, nil,
			[]wantMount{{"{tmp}/not-there.sock", MountEngineSocket, true}}},
		{"the same sockets through --mount", "", nil, []string{"type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock"},
			[]wantMount{{"/var/run/docker.sock", MountEngineSocket, true}}},
		{"the podman socket through --mount, src and dst", "", nil, []string{"type=bind,src=/run/podman/podman.sock,dst=/x,readonly"},
			[]wantMount{{"/run/podman/podman.sock", MountEngineSocket, true}}},
		{"DOCKER_HOST through --mount", "unix://{tmp}/engine.sock", nil, []string{"type=bind,source={tmp}/engine.sock,target=/x"},
			[]wantMount{{"{tmp}/engine.sock", MountEngineSocket, true}}},

		// A socket that is not the engine's.
		{"a socket path when DOCKER_HOST is unset", "", []string{"{tmp}/engine.sock:/x"}, nil, nil},
		{"DOCKER_HOST over tcp names no file", "tcp://10.0.0.5:2375", []string{"{tmp}/engine.sock:/x"}, nil, nil},
		{"DOCKER_HOST over ssh names no file", "ssh://user@host", []string{"{tmp}/engine.sock:/x"}, nil, nil},
		{"the ssh agent is a key, not the engine", "", []string{"{home}/.ssh/agent.sock:/x"}, nil,
			[]wantMount{{"{home}/.ssh/agent.sock", MountSSH, false}}},
		{"a file named like the socket elsewhere", "", []string{"{home}/projects/docker.sock:/x"}, nil, nil},

		// A socket and something else in one request.
		{"a socket and /etc", "", []string{"/etc:/e", "/var/run/docker.sock:/var/run/docker.sock"}, nil, []wantMount{
			{"/etc", MountSystemConfig, false}, {"/var/run/docker.sock", MountEngineSocket, true},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newMountEnv(t)
			t.Setenv("DOCKER_HOST", e.expand(tt.dockerHost))
			if err := os.WriteFile(filepath.Join(e.tmp, "engine.sock"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			got := SensitiveMounts(ContainerLaunchOptions{
				Image: "img", Volumes: e.expandAll(tt.volumes), Mounts: e.expandAll(tt.mounts),
			})
			checkMounts(t, got, tt.want, e)
		})
	}
}

// Criterion 3: real links, in a real directory, with HOME pointed at a private
// directory. The classification is of what the engine would end up mounting.
func TestSensitiveMountsResolvesSymbolicLinks(t *testing.T) {
	link := func(t *testing.T, target, name string) string {
		t.Helper()
		if err := os.Symlink(target, name); err != nil {
			t.Fatal(err)
		}
		return name
	}
	tests := []struct {
		name   string
		setup  func(t *testing.T, e mountEnv) string // returns the -v entry
		want   []wantMount
		sockOf string // DOCKER_HOST path, when the case is about the engine socket
	}{
		{"a link to ~/.ssh", func(t *testing.T, e mountEnv) string {
			return link(t, filepath.Join(e.home, ".ssh"), filepath.Join(e.tmp, "to-ssh")) + ":/x"
		}, []wantMount{{"{home}/.ssh", MountSSH, false}}, ""},
		{"a link to HOME", func(t *testing.T, e mountEnv) string {
			return link(t, e.home, filepath.Join(e.tmp, "to-home")) + ":/x"
		}, []wantMount{{"{home}", MountHome, false}}, ""},
		{"a link to a file under ~/.devdesk", func(t *testing.T, e mountEnv) string {
			target := filepath.Join(e.home, ".devdesk", "config.yaml")
			if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			return link(t, target, filepath.Join(e.tmp, "cfg")) + ":/x"
		}, []wantMount{{"{home}/.devdesk/config.yaml", MountDevDesk, false}}, ""},
		{"a chain of links", func(t *testing.T, e mountEnv) string {
			link(t, e.home, filepath.Join(e.tmp, "chain2"))
			return link(t, filepath.Join(e.tmp, "chain2"), filepath.Join(e.tmp, "chain1")) + ":/x"
		}, []wantMount{{"{home}", MountHome, false}}, ""},
		{"a relative link", func(t *testing.T, e mountEnv) string {
			return link(t, "../.ssh", filepath.Join(e.home, "projects", "rel")) + ":/x"
		}, []wantMount{{"{home}/.ssh", MountSSH, false}}, ""},
		{"a link to /etc", func(t *testing.T, e mountEnv) string {
			if r, err := filepath.EvalSymlinks("/etc"); err != nil || r != "/etc" {
				t.Skip("/etc is itself a link on this host")
			}
			return link(t, "/etc", filepath.Join(e.tmp, "to-etc")) + ":/x"
		}, []wantMount{{"/etc", MountSystemConfig, false}}, ""},
		{"a link to a harmless directory", func(t *testing.T, e mountEnv) string {
			return link(t, filepath.Join(e.home, "projects"), filepath.Join(e.tmp, "to-projects")) + ":/x"
		}, nil, ""},
		{"a link to the engine socket of DOCKER_HOST", func(t *testing.T, e mountEnv) string {
			sock := filepath.Join(e.tmp, "real.sock")
			if err := os.WriteFile(sock, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			return link(t, sock, filepath.Join(e.tmp, "sock-link")) + ":/x"
		}, []wantMount{{"{tmp}/real.sock", MountEngineSocket, true}}, "{tmp}/real.sock"},
		{"a loop of links is not sensitive and does not hang", func(t *testing.T, e mountEnv) string {
			return link(t, filepath.Join(e.tmp, "loop"), filepath.Join(e.tmp, "loop")) + ":/x"
		}, nil, ""},
		{"a dangling link is compared as written", func(t *testing.T, e mountEnv) string {
			return link(t, filepath.Join(e.tmp, "nowhere"), filepath.Join(e.tmp, "dangling")) + ":/x"
		}, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newMountEnv(t)
			if tt.sockOf != "" {
				t.Setenv("DOCKER_HOST", "unix://"+e.expand(tt.sockOf))
			}
			entry := tt.setup(t, e)

			got := SensitiveMounts(ContainerLaunchOptions{Image: "img", Volumes: []string{entry}})

			checkMounts(t, got, tt.want, e)
			if len(got) == 1 && got[0].Source != strings.SplitN(entry, ":", 2)[0] {
				t.Errorf("Source = %q, want the link as typed, %q", got[0].Source, strings.SplitN(entry, ":", 2)[0])
			}
		})
	}
}

// Criterion 3, the order of the two steps: the link is followed before the
// ".." is applied (the kernel's reading; the daemon's, which cleans first, is
// read too, see TestSensitiveMountsReadsABothWaysAfterALink). {tmp}/to-projects/../.ssh is {home}/.ssh for the kernel
// (to-projects leads to {home}/projects), and {tmp}/.ssh for a Clean() that runs
// first. Both the form -v and the form --mount.
func TestSensitiveMountsResolvesLinksBeforeNormalising(t *testing.T) {
	e := newMountEnv(t)
	if err := os.Symlink(filepath.Join(e.home, "projects"), filepath.Join(e.tmp, "to-projects")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(e.tmp, "to-projects") + "/../.ssh"

	got := SensitiveMounts(ContainerLaunchOptions{
		Image:   "img",
		Volumes: []string{path + ":/x"},
		Mounts:  []string{"type=bind,source=" + path + ",target=/y"},
	})

	checkMounts(t, got, []wantMount{
		{"{home}/.ssh", MountSSH, false}, {"{home}/.ssh", MountSSH, false},
	}, e)
}

// HOME unset: ~ cannot be expanded and "$HOME" is nothing. The function must not
// treat an empty HOME as "." or "/", must not panic, and must keep flagging the
// paths that need no HOME. What it does with a "~" it cannot expand is left open.
func TestSensitiveMountsWithNoHome(t *testing.T) {
	e := newMountEnv(t)
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	got := SensitiveMounts(ContainerLaunchOptions{
		Image:   "img",
		Volumes: []string{"/etc:/a", "./data:/c", e.cwd + ":/d"},
	})

	checkMounts(t, got, []wantMount{{"/etc", MountSystemConfig, false}}, e)
}

// HOME that is a link: the comparison is on resolved paths on both sides.
func TestSensitiveMountsResolvesHomeToo(t *testing.T) {
	e := newMountEnv(t)
	homeLink := filepath.Join(e.tmp, "home-link")
	if err := os.Symlink(e.home, homeLink); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", homeLink)
	t.Setenv("USERPROFILE", homeLink)

	got := SensitiveMounts(ContainerLaunchOptions{
		Image:   "img",
		Volumes: []string{e.home + "/.ssh:/a", homeLink + "/.ssh:/b", "~/.devdesk:/c", e.home + "/projects:/d"},
	})

	checkMounts(t, got, []wantMount{
		{"{home}/.ssh", MountSSH, false},
		{"{home}/.ssh", MountSSH, false},
		{"{home}/.devdesk", MountDevDesk, false},
	}, e)
}

// Reasons are fixed sentences of the category, never built from what was typed
// (the typed path may hold a secret, and the modal and the log print the reason).
func TestSensitiveMountsGivesEachCategoryItsOwnFixedReason(t *testing.T) {
	e := newMountEnv(t)
	byCategory := map[MountCategory]string{}
	for _, spec := range []struct {
		entry string
		want  MountCategory
	}{
		{"/:/a", MountRoot},
		{"/etc:/a", MountSystemConfig},
		{e.home + ":/a", MountHome},
		{e.home + "/.ssh:/a", MountSSH},
		{e.home + "/.devdesk:/a", MountDevDesk},
		{"/var/run/docker.sock:/a", MountEngineSocket},
	} {
		got := SensitiveMounts(ContainerLaunchOptions{Image: "img", Volumes: []string{spec.entry}})
		if len(got) != 1 || got[0].Category != spec.want {
			t.Fatalf("%q: got %+v, want one %q mount", spec.entry, got, spec.want)
		}
		byCategory[spec.want] = got[0].Reason
	}
	seen := map[string]MountCategory{}
	for cat, reason := range byCategory {
		if other, dup := seen[reason]; dup {
			t.Errorf("categories %q and %q share the reason %q", cat, other, reason)
		}
		seen[reason] = cat
		if strings.Contains(reason, e.home) {
			t.Errorf("the reason of %q holds the typed path: %q", cat, reason)
		}
	}

	// A second call, a different path of the same category: the same sentence.
	again := SensitiveMounts(ContainerLaunchOptions{Image: "img", Volumes: []string{"/etc/ssl:/a"}})
	if len(again) != 1 || again[0].Reason != byCategory[MountSystemConfig] {
		t.Errorf("/etc/ssl reason = %+v, want the one of /etc: %q", again, byCategory[MountSystemConfig])
	}

	reason := strings.ToLower(byCategory[MountEngineSocket])
	if !strings.Contains(reason, "engine") || !strings.Contains(reason, "control") {
		t.Errorf("the engine-socket reason = %q, want it to say the container gets control of the engine", byCategory[MountEngineSocket])
	}
}

// SensitiveMounts is a reader: it starts nothing and creates nothing, so it is
// safe to call on every keystroke of a future live check.
func TestSensitiveMountsCreatesNothing(t *testing.T) {
	e := newMountEnv(t)
	missing := filepath.Join(e.tmp, "does", "not", "exist")

	SensitiveMounts(ContainerLaunchOptions{Image: "img", Volumes: []string{missing + ":/x"}})

	if _, err := os.Lstat(missing); !os.IsNotExist(err) {
		t.Errorf("the path was created or touched: %v", err)
	}
}
