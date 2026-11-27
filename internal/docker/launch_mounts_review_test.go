package docker

import (
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"testing"
)

// OC-20 review: what the first version let through. Each table is read with the
// private environment of launch_mounts_test.go (newMountEnv: HOME, XDG_RUNTIME_DIR,
// DOCKER_HOST, CONTAINER_HOST and the working directory).

// Review 1: Docker lowercases the keys of a --mount and the value of its type,
// so TYPE=BIND is a bind. Podman is read the same way (conservative).
func TestSensitiveMountsReadsTheMountFormInAnyCase(t *testing.T) {
	tests := []struct {
		name  string
		mount string
	}{
		{"type value in capitals", "type=BIND,source=/etc,target=/x"},
		{"type value capitalized", "type=Bind,source=/etc,target=/x"},
		{"every key in capitals", "TYPE=bind,SOURCE=/etc,TARGET=/x"},
		{"src capitalized", "type=bind,Src=/etc,dst=/x,READONLY"},
		{"mixed", "Type=BIND,sOuRcE=/etc,target=/x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newMountEnv(t)
			got := SensitiveMounts(ContainerLaunchOptions{Image: "img", Mounts: []string{tt.mount}})
			checkMounts(t, got, []wantMount{{"/etc", MountSystemConfig, false}}, e)
		})
	}
}

// Review 2: ~/.ssh and ~/.devdesk are often links (stow, dotfiles managers): the
// target of the link is as sensitive as the link, whichever way it is typed.
func TestSensitiveMountsSeesAThroughASensitiveDirectoryThatIsALink(t *testing.T) {
	for _, tt := range []struct {
		dir      string
		category MountCategory
		inside   string
	}{
		{".ssh", MountSSH, "keys"},
		{".devdesk", MountDevDesk, "config.yaml"},
	} {
		t.Run(tt.dir, func(t *testing.T) {
			e := newMountEnv(t)
			real := filepath.Join(e.tmp, "dotfiles", tt.dir[1:])
			if err := os.MkdirAll(real, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(e.home, tt.dir)); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(real, filepath.Join(e.home, tt.dir)); err != nil {
				t.Fatal(err)
			}
			for _, entry := range []string{
				real, real + "/" + tt.inside, "{home}/" + tt.dir, "~/" + tt.dir, "{home}/" + tt.dir + "/" + tt.inside,
			} {
				got := SensitiveMounts(ContainerLaunchOptions{Image: "img", Volumes: []string{e.expand(entry) + ":/x"}})
				if len(got) != 1 || got[0].Category != tt.category {
					t.Errorf("%q: got %+v, want one %q mount", entry, got, tt.category)
				}
			}
		})
	}
}

// Review 3: the daemon cleans the path first, the kernel follows the link first.
// Either reading may be the one that is sensitive; both are read.
func TestSensitiveMountsReadsABothWaysAfterALink(t *testing.T) {
	e := newMountEnv(t)
	// {home}/projects/lnk leads to {tmp}. Kernel: {home}/projects/lnk/../../.ssh is
	// <parent of {tmp}>/../.ssh, nothing. Textual: {home}/.ssh.
	if err := os.Symlink(e.tmp, filepath.Join(e.home, "projects", "lnk")); err != nil {
		t.Fatal(err)
	}
	path := e.home + "/projects/lnk/../../.ssh"

	got := SensitiveMounts(ContainerLaunchOptions{
		Image:   "img",
		Volumes: []string{path + ":/x"},
		Mounts:  []string{"type=bind,source=" + path + ",target=/y"},
	})

	checkMounts(t, got, []wantMount{
		{"{home}/.ssh", MountSSH, false}, {"{home}/.ssh", MountSSH, false},
	}, e)
}

// Review 4: the Docker CLI makes any ".x" source absolute, so ".ssh" is a path.
func TestSensitiveMountsReadsADotPrefixedRelativeSource(t *testing.T) {
	tests := []struct {
		name   string
		volume string
		mount  string
		want   []wantMount
	}{
		{".ssh", ".ssh:/x", "type=bind,source=.ssh,target=/x", []wantMount{{"{home}/.ssh", MountSSH, false}}},
		{".devdesk", ".devdesk:/x", "type=bind,src=.devdesk,target=/x", []wantMount{{"{home}/.devdesk", MountDevDesk, false}}},
		{".config is nothing", ".config:/x", "type=bind,source=.config,target=/x", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newMountEnv(t)
			t.Chdir(e.home)
			var want []wantMount
			for range 2 { // the -v entry and the --mount value
				want = append(want, tt.want...)
			}
			got := SensitiveMounts(ContainerLaunchOptions{Image: "img", Volumes: []string{tt.volume}, Mounts: []string{tt.mount}})
			checkMounts(t, got, want, e)
		})
	}
}

// Review 6: a path that cannot be read (not "does not exist") cannot be proven
// harmless, and /proc/<pid>/root is the host root of another process.
func TestSensitiveMountsFlagsWhatItCannotVerify(t *testing.T) {
	t.Run("a directory that denies the search", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root reads everything")
		}
		e := newMountEnv(t)
		locked := filepath.Join(e.tmp, "locked")
		if err := os.MkdirAll(filepath.Join(locked, "inner"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

		got := SensitiveMounts(ContainerLaunchOptions{Image: "img", Volumes: []string{locked + "/inner:/x"}})

		checkMounts(t, got, []wantMount{{locked + "/inner", MountUnverified, false}}, e)
	})
	t.Run("a missing path is not one", func(t *testing.T) {
		e := newMountEnv(t)
		got := SensitiveMounts(ContainerLaunchOptions{Image: "img", Volumes: []string{e.tmp + "/nowhere/deeper:/x"}})
		checkMounts(t, got, nil, e)
	})
	t.Run("a file read as a directory is not one", func(t *testing.T) {
		e := newMountEnv(t)
		file := filepath.Join(e.tmp, "plain")
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		got := SensitiveMounts(ContainerLaunchOptions{Image: "img", Volumes: []string{file + "/below:/x"}})
		checkMounts(t, got, nil, e)
	})
	for _, p := range []string{"/proc/self/root", "/proc/self/cwd", "/proc/thread-self/root", "/proc/1/root", "/proc/1/root/etc", "/proc/1/cwd/x", "/proc/12345/root"} {
		t.Run(p, func(t *testing.T) {
			e := newMountEnv(t)
			if _, err := os.Lstat("/proc/self"); err != nil {
				t.Skip("no /proc")
			}
			got := SensitiveMounts(ContainerLaunchOptions{Image: "img", Volumes: []string{p + ":/x"}, Mounts: []string{"type=bind,source=" + p + ",target=/y"}})
			if len(got) != 2 || got[0].Category != MountRoot || got[1].Category != MountRoot {
				t.Errorf("%s: got %+v, want two root mounts", p, got)
			}
			_ = e
		})
	}
}

// Review 7: a named volume can be a bind in disguise (the local driver with
// device=<path>), and Podman has a glob type.
func TestSensitiveMountsReadsAVolumeDeviceAndAGlob(t *testing.T) {
	tests := []struct {
		name  string
		mount string
		want  []wantMount
	}{
		{"volume-opt device", "type=volume,source=v,target=/t,volume-opt=type=none,volume-opt=o=bind,volume-opt=device=/etc",
			[]wantMount{{"/etc", MountSystemConfig, false}}},
		{"volume-opt in capitals", "TYPE=VOLUME,target=/t,VOLUME-OPT=DEVICE=/etc/ssl",
			[]wantMount{{"/etc/ssl", MountSystemConfig, false}}},
		{"a device that is not a path", "type=volume,source=v,target=/t,volume-opt=device=tmpfs", nil},
		{"another option", "type=volume,source=v,target=/t,volume-opt=o=bind", nil},
		{"a harmless device", "type=volume,source=v,target=/t,volume-opt=device={home}/projects", nil},
		{"glob", "type=glob,source=/etc/ss*,target=/t", []wantMount{{"/etc/ss*", MountSystemConfig, false}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newMountEnv(t)
			got := SensitiveMounts(ContainerLaunchOptions{Image: "img", Mounts: e.expandAll([]string{tt.mount})})
			checkMounts(t, got, tt.want, e)
		})
	}
}

// Review 8: the source that contains a sensitive path is sensitive, with the
// category of what it contains; one that is not on the way to one is not.
func TestSensitiveMountsFlagsTheAncestorsOfASensitivePath(t *testing.T) {
	tests := []struct {
		name    string
		volumes []string
		want    []wantMount
	}{
		{"/run holds the sockets", []string{"/run:/x"}, []wantMount{{"/run", MountEngineSocket, true}}},
		{"/var/run holds docker.sock", []string{"/var/run:/x"}, []wantMount{{"/var/run", MountEngineSocket, true}}},
		{"/var holds /var/run/docker.sock", []string{"/var:/x"}, []wantMount{{"/var", MountEngineSocket, true}}},
		{"the parent of HOME", []string{"{home}/..:/x"}, []wantMount{{"{home}/..", MountHome, false}}},
		{"two entries, one modal", []string{"/run:/a", "/etc:/b"}, []wantMount{
			{"/run", MountEngineSocket, true}, {"/etc", MountSystemConfig, false},
		}},
		{"under HOME but not on the way", []string{"{home}/projects:/x", "{home}/projects/app:/y"}, nil},
		{"a sibling of a sensitive path", []string{"/etcetera:/x", "{home}/.sshx:/y"}, nil},
		{"/usr leads to nothing sensitive", []string{"/usr:/x"}, nil},
		{"the working directory", []string{"./data:/x", ".:/y"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newMountEnv(t)
			// A HOME alone in its parent: the parent of the shared scratch directories
			// also holds the engine socket of XDG_RUNTIME_DIR.
			lone := filepath.Join(e.tmp, "lone", "home")
			if err := os.MkdirAll(lone, 0o700); err != nil {
				t.Fatal(err)
			}
			e.home = lone
			t.Setenv("HOME", lone)
			t.Setenv("USERPROFILE", lone)
			got := SensitiveMounts(ContainerLaunchOptions{Image: "img", Volumes: e.expandAll(tt.volumes)})
			if tt.name == "the parent of HOME" {
				tt.want[0].path = filepath.Dir(lone) // the path is resolved, not the typed ".."
			}
			checkMounts(t, got, tt.want, e)
		})
	}
}

// Review 9: a socket is a socket whatever its name and wherever the link leads,
// and containerd's is an engine's.
func TestSensitiveMountsTreatsAnySocketFileAsTheEngine(t *testing.T) {
	short, err := os.MkdirTemp("", "dd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(short) })
	sock := filepath.Join(short, "s.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("no unix socket here: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if err := os.Symlink(sock, filepath.Join(short, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(short, "docker.sock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(short)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		volume string
		want   []wantMount
	}{
		{"a socket of any name", short + "/s.sock:/x", []wantMount{{resolved + "/s.sock", MountEngineSocket, true}}},
		{"a link to a socket", short + "/link:/x", []wantMount{{resolved + "/s.sock", MountEngineSocket, true}}},
		{"containerd", "/run/containerd/containerd.sock:/x", []wantMount{{"/run/containerd/containerd.sock", MountEngineSocket, true}}},
		{"a plain file named docker.sock", short + "/docker.sock:/x", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newMountEnv(t)
			got := SensitiveMounts(ContainerLaunchOptions{Image: "img", Volumes: []string{tt.volume}})
			checkMounts(t, got, tt.want, e)
		})
	}
}

// Review 10: with HOME unset or relative, the categories still know the home of
// the user; "~" is still not expanded. Without XDG_RUNTIME_DIR the rootless
// sockets are under /run/user/<uid>.
func TestSensitiveMountsFallsBackWhenTheEnvironmentIsIncomplete(t *testing.T) {
	for _, home := range []string{"", "relative/home"} {
		t.Run("HOME is "+strconv.Quote(home), func(t *testing.T) {
			e := newMountEnv(t)
			u, err := user.Current()
			if err != nil || !filepath.IsAbs(u.HomeDir) || u.HomeDir == "/" {
				t.Skip("no usable home in the user database")
			}
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)

			got := SensitiveMounts(ContainerLaunchOptions{Image: "img", Volumes: []string{u.HomeDir + "/.ssh:/a", u.HomeDir + "/.devdesk:/b", "~/.ssh:/c"}})

			if len(got) != 2 || got[0].Category != MountSSH || got[1].Category != MountDevDesk {
				t.Errorf("got %+v, want ~/.ssh and ~/.devdesk of %s, and the tilde left unexpanded", got, u.HomeDir)
			}
			_ = e
		})
	}
	t.Run("no XDG_RUNTIME_DIR", func(t *testing.T) {
		e := newMountEnv(t)
		t.Setenv("XDG_RUNTIME_DIR", "")
		run := "/run/user/" + strconv.Itoa(os.Getuid())

		got := SensitiveMounts(ContainerLaunchOptions{Image: "img", Volumes: []string{run + "/docker.sock:/a", run + "/podman/podman.sock:/b"}})

		checkMounts(t, got, []wantMount{
			{run + "/docker.sock", MountEngineSocket, true}, {run + "/podman/podman.sock", MountEngineSocket, true},
		}, e)
	})
}

// CONTAINER_HOST, like DOCKER_HOST, can name the engine's socket.
func TestSensitiveMountsFlagsTheSocketOfContainerHost(t *testing.T) {
	e := newMountEnv(t)
	t.Setenv("CONTAINER_HOST", "unix://"+e.tmp+"/podman-remote.sock")

	got := SensitiveMounts(ContainerLaunchOptions{Image: "img", Volumes: []string{e.tmp + "/podman-remote.sock:/x"}})

	checkMounts(t, got, []wantMount{{e.tmp + "/podman-remote.sock", MountEngineSocket, true}}, e)
}
