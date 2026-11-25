package scan

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fluxDoc is a document of apiVersion av.
func fluxDoc(av, kind string) string {
	return "apiVersion: " + av + "\nkind: " + kind + "\nmetadata:\n  name: x\n"
}

// fluxOf asks about the overlay `overlay` of a repository made of files.
func fluxOf(t *testing.T, files map[string]string, lim gateLimits) bool {
	t.Helper()
	root := writeRepo(t, files)
	return fluxOverlay(root, filepath.Join(root, "overlay"), lim)
}

// DEVDESK-783: an overlay is a Flux one when its kustomization lists, in
// `resources` or `bases`, a FILE one of whose documents is of an apiVersion whose
// group is exactly *.toolkit.fluxcd.io.
func TestFluxOverlayReadsTheFilesAnOverlayListsAndTheirGroup(t *testing.T) {
	flux := fluxDoc("kustomize.toolkit.fluxcd.io/v1", "Kustomization")
	listing := "resources:\n  - ../flux/a.yaml\n"
	tests := []struct {
		name  string
		files map[string]string
		want  bool
	}{
		// What it is.
		{"a Flux Kustomization in a neighbouring folder", ovl(listing, "flux/a.yaml", flux), true},
		{"the file is below the overlay", ovl("resources:\n  - a.yaml\n", "overlay/a.yaml", flux), true},
		{"a HelmRelease", ovl(listing, "flux/a.yaml", fluxDoc("helm.toolkit.fluxcd.io/v2", "HelmRelease")), true},
		{"a GitRepository", ovl(listing, "flux/a.yaml", fluxDoc("source.toolkit.fluxcd.io/v1", "GitRepository")), true},
		{"a version that is not the latest", ovl(listing, "flux/a.yaml", fluxDoc("kustomize.toolkit.fluxcd.io/v1beta2", "Kustomization")), true},
		{"the group quoted", ovl(listing, "flux/a.yaml", fluxDoc(`"kustomize.toolkit.fluxcd.io/v1"`, "Kustomization")), true},
		{"in bases", ovl("bases:\n  - ../flux/a.yaml\n", "flux/a.yaml", flux), true},
		{"the kustomization is kustomization.yml", map[string]string{
			"overlay/kustomization.yml": listing, "flux/a.yaml": flux}, true},
		{"the second file of the list", ovl("resources:\n  - ../flux/cm.yaml\n  - ../flux/a.yaml\n",
			"flux/cm.yaml", fluxDoc("v1", "ConfigMap"), "flux/a.yaml", flux), true},
		{"the second document of the file", ovl(listing, "flux/a.yaml", fluxDoc("v1", "ConfigMap")+"---\n"+flux), true},
		{"a file that starts with ---", ovl(listing, "flux/a.yaml", "---\n"+flux), true},
		{"a file that starts with comments and ---", ovl(listing, "flux/a.yaml", "# the infra\n---\n# kind\n"+flux), true},
		{"a Flux document after a ConfigMap, --- first", ovl(listing, "flux/a.yaml", "---\n"+fluxDoc("v1", "ConfigMap")+"---\n"+flux), true},
		{"a Flux file next to one that does not exist", ovl("resources:\n  - ../flux/missing.yaml\n  - ../flux/a.yaml\n", "flux/a.yaml", flux), true},

		// What it is not.
		{"a ConfigMap", ovl(listing, "flux/a.yaml", fluxDoc("v1", "ConfigMap")), false},
		{"a Deployment", ovl(listing, "flux/a.yaml", fluxDoc("apps/v1", "Deployment")), false},
		{"a custom resource of another group", ovl(listing, "flux/a.yaml", fluxDoc("example.com/v1", "Widget")), false},
		{"a group that continues after toolkit.fluxcd.io", ovl(listing, "flux/a.yaml",
			fluxDoc("x.toolkit.fluxcd.io.evil.com/v1", "Kustomization")), false},
		{"a group that is Flux's name under another one", ovl(listing, "flux/a.yaml",
			fluxDoc("evil.com/kustomize.toolkit.fluxcd.io/v1", "Kustomization")), false},
		{"a group that only ends like it", ovl(listing, "flux/a.yaml", fluxDoc("faketoolkit.fluxcd.io/v1", "Kustomization")), false},
		{"a group that is another domain of the same project", ovl(listing, "flux/a.yaml",
			fluxDoc("kustomize.toolkit.fluxcd.io.example/v1", "Kustomization")), false},
		{"Flux's name in a place that is not the apiVersion", ovl(listing, "flux/a.yaml",
			"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n  annotations:\n    a: kustomize.toolkit.fluxcd.io/v1\ndata:\n  apiVersion: kustomize.toolkit.fluxcd.io/v1\n"), false},
		{"Flux's apiVersion in a comment only", ovl(listing, "flux/a.yaml",
			"# apiVersion: kustomize.toolkit.fluxcd.io/v1\napiVersion: v1\nkind: ConfigMap # kustomize.toolkit.fluxcd.io/v1\nmetadata:\n  name: x\n"), false},
		{"Flux's apiVersion in a label value", ovl(listing, "flux/a.yaml",
			"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n  labels:\n    apiVersion: kustomize.toolkit.fluxcd.io/v1\n    app: kustomize.toolkit.fluxcd.io/v1\n"), false},
		{"Flux's apiVersion in a block scalar", ovl(listing, "flux/a.yaml",
			"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\ndata:\n  doc: |\n    apiVersion: kustomize.toolkit.fluxcd.io/v1\n"), false},
		{"a file that is not YAML", ovl(listing, "flux/a.yaml", "apiVersion: [kustomize.toolkit.fluxcd.io/v1\n"), false},
		{"an empty file", ovl(listing, "flux/a.yaml", ""), false},
		{"a file that is not there", ovl(listing, "flux/other.yaml", fluxDoc("kustomize.toolkit.fluxcd.io/v1", "Kustomization")), false},
		{"nothing listed", ovl("namePrefix: dev-\n"), false},
		{"a Flux file that is a patch, not a resource", ovl("patches:\n  - path: ../flux/a.yaml\n", "flux/a.yaml", flux), false},
		{"a Flux file that is a component", ovl("components:\n  - ../flux/a.yaml\n", "flux/a.yaml", flux), false},

		// A folder is not followed: Flux's own build is what reads it.
		{"a folder with its own kustomization that lists a Flux file", ovl("resources:\n  - ../flux\n",
			"flux/kustomization.yaml", "resources:\n  - a.yaml\n", "flux/a.yaml", flux), false},
		{"a folder without a kustomization, with a Flux file in it", ovl("resources:\n  - ../flux\n", "flux/a.yaml", flux), false},
		{"a folder listed after a ConfigMap", ovl("resources:\n  - ../flux/cm.yaml\n  - ../dir\n",
			"flux/cm.yaml", fluxDoc("v1", "ConfigMap"), "dir/kustomization.yaml", "resources:\n  - a.yaml\n", "dir/a.yaml", flux), false},

		// A reference that is not a path of the repository.
		{"an address", ovl("resources:\n  - " + q("https://example.com/flux.yaml") + "\n"), false},
		{"the loopback", ovl("resources:\n  - " + q(loopbackURL) + "\n"), false},
		{"a git repository", ovl("resources:\n  - " + q("git@example.com:o/r") + "\n"), false},
		{"a name that starts with github.com", ovl("resources:\n  - github.com/org/repo/flux.yaml\n",
			"github.com/org/repo/flux.yaml", flux), false},
		{"a path with a query", ovl("resources:\n  - "+q("../flux/a.yaml?ref=x")+"\n", "flux/a.yaml", flux), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fluxOf(t, tt.files, defaultGateLimits); got != tt.want {
				t.Errorf("fluxOverlay = %v, want %v", got, tt.want)
			}
		})
	}
}

// The path of the file is the gate's: absolute, outside the root or through a link
// that leaves it, the answer is false whatever the file holds.
func TestFluxOverlayFollowsTheGatesRulesForAPath(t *testing.T) {
	flux := fluxDoc("kustomize.toolkit.fluxcd.io/v1", "Kustomization")

	t.Run("an absolute path, to a file inside the root", func(t *testing.T) {
		root := writeRepo(t, ovl("resources: []\n", "flux/a.yaml", flux))
		writeFile(t, filepath.Join(root, "overlay", "kustomization.yaml"),
			"resources:\n  - "+q(filepath.Join(root, "flux", "a.yaml"))+"\n")

		if fluxOverlay(root, filepath.Join(root, "overlay"), defaultGateLimits) {
			t.Error("an absolute path was followed")
		}
	})
	t.Run("a path above the root", func(t *testing.T) {
		base := t.TempDir()
		root := filepath.Join(base, "repo")
		writeFile(t, filepath.Join(root, "overlay", "kustomization.yaml"), "resources:\n  - ../../outside/a.yaml\n")
		writeFile(t, filepath.Join(base, "outside", "a.yaml"), flux)

		if fluxOverlay(root, filepath.Join(root, "overlay"), defaultGateLimits) {
			t.Error("a file above the root was read")
		}
	})
	t.Run("the same file, below the root, is read", func(t *testing.T) {
		base := t.TempDir()
		root := filepath.Join(base, "repo")
		writeFile(t, filepath.Join(root, "overlay", "kustomization.yaml"), "resources:\n  - ../../repo/outside/a.yaml\n")
		writeFile(t, filepath.Join(root, "outside", "a.yaml"), flux)

		if !fluxOverlay(root, filepath.Join(root, "overlay"), defaultGateLimits) {
			t.Error("a file below the root, reached through a path that goes up and down again, was not read")
		}
	})
	t.Run("a symbolic link to a file outside the root", func(t *testing.T) {
		base := t.TempDir()
		root := filepath.Join(base, "repo")
		writeFile(t, filepath.Join(root, "overlay", "kustomization.yaml"), "resources:\n  - link.yaml\n")
		writeFile(t, filepath.Join(base, "outside", "a.yaml"), flux)
		if err := os.Symlink(filepath.Join(base, "outside", "a.yaml"), filepath.Join(root, "overlay", "link.yaml")); err != nil {
			t.Skipf("no symbolic link here: %v", err)
		}

		if fluxOverlay(root, filepath.Join(root, "overlay"), defaultGateLimits) {
			t.Error("a link out of the root was followed")
		}
	})
	t.Run("a symbolic link to a folder outside the root", func(t *testing.T) {
		base := t.TempDir()
		root := filepath.Join(base, "repo")
		writeFile(t, filepath.Join(root, "overlay", "kustomization.yaml"), "resources:\n  - link/a.yaml\n")
		writeFile(t, filepath.Join(base, "outside", "a.yaml"), flux)
		if err := os.Symlink(filepath.Join(base, "outside"), filepath.Join(root, "overlay", "link")); err != nil {
			t.Skipf("no symbolic link here: %v", err)
		}

		if fluxOverlay(root, filepath.Join(root, "overlay"), defaultGateLimits) {
			t.Error("a link to a folder out of the root was followed")
		}
	})
	t.Run("a symbolic link that stays inside the root", func(t *testing.T) {
		root := writeRepo(t, ovl("resources:\n  - link.yaml\n", "flux/a.yaml", flux))
		if err := os.Symlink(filepath.Join(root, "flux", "a.yaml"), filepath.Join(root, "overlay", "link.yaml")); err != nil {
			t.Skipf("no symbolic link here: %v", err)
		}

		if !fluxOverlay(root, filepath.Join(root, "overlay"), defaultGateLimits) {
			t.Error("a link that stays below the root was not followed")
		}
	})
	t.Run("a root that is reached through a link is the same root", func(t *testing.T) {
		real := writeRepo(t, ovl("resources:\n  - ../flux/a.yaml\n", "flux/a.yaml", flux))
		link := filepath.Join(t.TempDir(), "root")
		if err := os.Symlink(real, link); err != nil {
			t.Skipf("no symbolic link here: %v", err)
		}

		if !fluxOverlay(link, filepath.Join(link, "overlay"), defaultGateLimits) {
			t.Error("the root was not resolved")
		}
	})
	t.Run("an overlay outside the root", func(t *testing.T) {
		root := writeRepo(t, ovl("resources:\n  - ../flux/a.yaml\n", "flux/a.yaml", flux))
		other := filepath.Join(root, "flux")

		if fluxOverlay(other, filepath.Join(root, "overlay"), defaultGateLimits) {
			t.Error("an overlay that is not below the root was read")
		}
	})
	t.Run("an overlay that is not there", func(t *testing.T) {
		root := writeRepo(t, map[string]string{"flux/a.yaml": flux})

		if fluxOverlay(root, filepath.Join(root, "overlay"), defaultGateLimits) {
			t.Error("a missing overlay is a Flux one")
		}
	})
}

// A file is read within the gate's own ceiling: at most FileBytes (a mebibyte), no
// more.
func TestFluxOverlayDoesNotReadAFileLargerThanTheCeiling(t *testing.T) {
	flux := fluxDoc("kustomize.toolkit.fluxcd.io/v1", "Kustomization")
	files := ovl("resources:\n  - ../flux/a.yaml\n", "flux/a.yaml", flux)
	size := int64(len(flux))

	if !fluxOf(t, files, gateLimits{Depth: 16, Visited: 256, FileBytes: size, TotalBytes: 8 << 20}) {
		t.Error("a file of exactly the ceiling was not read")
	}
	if fluxOf(t, files, gateLimits{Depth: 16, Visited: 256, FileBytes: size - 1, TotalBytes: 8 << 20}) {
		t.Error("a file one byte over the ceiling was read")
	}
}

// With the real ceiling: a mebibyte and a byte of comments before the document.
func TestFluxOverlayDoesNotReadAFileOfMoreThanAMebibyte(t *testing.T) {
	flux := fluxDoc("kustomize.toolkit.fluxcd.io/v1", "Kustomization")
	pad := func(n int) string { return "#" + strings.Repeat("x", n-2) + "\n" }
	small := ovl("resources:\n  - ../flux/a.yaml\n", "flux/a.yaml", pad(1<<10)+flux)
	big := ovl("resources:\n  - ../flux/a.yaml\n", "flux/a.yaml", pad(1<<20)+flux)

	if !fluxOf(t, small, defaultGateLimits) {
		t.Error("a small file was not read")
	}
	if fluxOf(t, big, defaultGateLimits) {
		t.Errorf("a file of %d bytes was read, the ceiling is %d", len(big["flux/a.yaml"]), defaultGateLimits.FileBytes)
	}
}

// ── kustomizeArgs ────────────────────────────────────────────────────────────

func TestKustomizeArgsAddsTheFlagAfterBuildAndTheUsersArguments(t *testing.T) {
	scope := targetOnly("/repo")
	wider := renderScope{target: "/repo/deploy", mount: "/repo", rel: "deploy"}
	tests := []struct {
		name string
		got  toolCmd
		want []string
		dir  string
	}{
		{"a binary, with the flag", kustomizeArgs(scope, "kustomizations/dev", ToolSpec{Source: ToolSourceBinary}, true),
			[]string{"build", loadNone, "./kustomizations/dev"}, "/repo"},
		{"a binary, without the flag: what it is today", kustomizeArgs(scope, "kustomizations/dev", ToolSpec{Source: ToolSourceBinary}, false),
			[]string{"build", "./kustomizations/dev"}, "/repo"},
		{"a binary, the user's arguments, with the flag",
			kustomizeArgs(scope, "kustomizations/dev", ToolSpec{Source: ToolSourceBinary, Args: []string{"--enable-helm", "--helm-command", "helm"}}, true),
			[]string{"build", "--enable-helm", "--helm-command", "helm", loadNone, "./kustomizations/dev"}, "/repo"},
		{"a binary, the user's arguments, without the flag",
			kustomizeArgs(scope, "kustomizations/dev", ToolSpec{Source: ToolSourceBinary, Args: []string{"--enable-helm"}}, false),
			[]string{"build", "--enable-helm", "./kustomizations/dev"}, "/repo"},
		{"a target, with the flag", kustomizeArgs(scope, ".", ToolSpec{Source: ToolSourceBinary}, true),
			[]string{"build", loadNone, "."}, "/repo"},
		{"a folder named like the flag, with the flag", kustomizeArgs(scope, loadNone, ToolSpec{Source: ToolSourceBinary}, true),
			[]string{"build", loadNone, "./" + loadNone}, "/repo"},
		{"a folder named like the flag, without it", kustomizeArgs(scope, loadNone, ToolSpec{Source: ToolSourceBinary}, false),
			[]string{"build", "./" + loadNone}, "/repo"},
		{"a binary in a repository, with the flag", kustomizeArgs(wider, "kustomizations/dev", ToolSpec{Source: ToolSourceBinary}, true),
			[]string{"build", loadNone, "./kustomizations/dev"}, "/repo/deploy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !slices.Equal(tt.got.Args, tt.want) {
				t.Errorf("Args = %q, want %q", tt.got.Args, tt.want)
			}
			if tt.got.Dir != tt.dir {
				t.Errorf("Dir = %q, want %q", tt.got.Dir, tt.dir)
			}
		})
	}
}

// From an image the flag is an argument of the image like the others, after the
// image name; `--network none` stays before it, and what comes before the image
// is what it was.
func TestKustomizeArgsForAnImageHasTheFlagAfterTheImageAndTheNetworkStaysCut(t *testing.T) {
	tool := ToolSpec{Source: ToolSourceContainer, Image: "img:1"}
	wider := renderScope{target: "/repo/deploy", mount: "/repo", rel: "deploy"}
	forEachEngine(t, func(t *testing.T, binary string) {
		tests := []struct {
			name  string
			scope renderScope
			tool  ToolSpec
			flag  bool
			want  []string
		}{
			{"with the flag", targetOnly("/repo").cut(), tool, true,
				[]string{"run", "--rm", "-v", "/repo:/scan:ro", "-w", "/scan", "--network", "none", "img:1", "build", loadNone, "./kustomizations/dev"}},
			// The golden: nothing changes for a scan that does not ask for the flag.
			{"without the flag", targetOnly("/repo").cut(), tool, false,
				[]string{"run", "--rm", "-v", "/repo:/scan:ro", "-w", "/scan", "--network", "none", "img:1", "build", "./kustomizations/dev"}},
			{"with the flag, in a repository", wider.cut(), tool, true,
				[]string{"run", "--rm", "-v", "/repo:/scan:ro", "-w", "/scan/deploy", "--network", "none", "img:1", "build", loadNone, "./kustomizations/dev"}},
			{"with the flag, the network allowed", targetOnly("/repo"), tool, true,
				[]string{"run", "--rm", "-v", "/repo:/scan:ro", "-w", "/scan", "img:1", "build", loadNone, "./kustomizations/dev"}},
			{"with the flag and the user's arguments", targetOnly("/repo").cut(),
				ToolSpec{Source: ToolSourceContainer, Image: "img:1", Args: []string{"--enable-helm"}}, true,
				[]string{"run", "--rm", "-v", "/repo:/scan:ro", "-w", "/scan", "--network", "none", "img:1", "build", "--enable-helm", loadNone, "./kustomizations/dev"}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := kustomizeArgs(tt.scope, "kustomizations/dev", tt.tool, tt.flag)

				if got.Name != binary || !slices.Equal(got.Args, tt.want) {
					t.Errorf("cmd = %s %q, want %s %q", got.Name, got.Args, binary, tt.want)
				}
			})
		}
	})
}

// A restrictor the user chose is theirs: whatever the caller asks, nothing is
// added next to it.
func TestKustomizeArgsAddsNothingWhenTheUserSetTheRestrictor(t *testing.T) {
	for name, user := range map[string][]string{
		"the = form":          {"--load-restrictor=LoadRestrictionsRootOnly"},
		"the = form, as none": {loadNone},
		"a separate value":    {"--load-restrictor", "LoadRestrictionsRootOnly"},
		"among others":        {"--enable-helm", "--load-restrictor=LoadRestrictionsRootOnly", "--helm-command", "helm"},
	} {
		for _, source := range []ToolSource{ToolSourceBinary, ToolSourceContainer} {
			t.Run(name+" "+string(source), func(t *testing.T) {
				tool := ToolSpec{Source: source, Image: "img:1", Args: user}

				got := kustomizeArgs(targetOnly("/repo"), "k8s/prod", tool, true)

				var restrictors int
				for _, a := range got.Args {
					if strings.HasPrefix(a, "--load-restrictor") {
						restrictors++
					}
				}
				want := 0
				for _, a := range user {
					if strings.HasPrefix(a, "--load-restrictor") {
						want++
					}
				}
				if restrictors != want {
					t.Errorf("argv holds %d --load-restrictor, want the user's %d only: %q", restrictors, want, got.Args)
				}
				without := kustomizeArgs(targetOnly("/repo"), "k8s/prod", tool, false)
				if !slices.Equal(got.Args, without.Args) {
					t.Errorf("with the flag asked: %q, without: %q, want the same", got.Args, without.Args)
				}
			})
		}
	}
}

func TestUserSetsLoadRestrictor(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"nothing", nil, false},
		{"an empty list", []string{}, false},
		{"other arguments", []string{"--enable-helm", "--helm-command", "helm"}, false},
		{"the = form", []string{"--load-restrictor=LoadRestrictionsRootOnly"}, true},
		{"the = form, none", []string{loadNone}, true},
		{"a separate value", []string{"--load-restrictor", "LoadRestrictionsNone"}, true},
		{"the name alone", []string{"--load-restrictor"}, true},
		{"after other arguments", []string{"--enable-helm", "--load-restrictor=LoadRestrictionsRootOnly"}, true},
		{"before other arguments", []string{"--load-restrictor", "LoadRestrictionsRootOnly", "--enable-helm"}, true},
		{"the name inside another argument", []string{"--label=--load-restrictor=x"}, false},
		{"a value that happens to hold it", []string{"--helm-command", "load-restrictor"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := slices.Clone(tt.args)

			if got := userSetsLoadRestrictor(tt.args); got != tt.want {
				t.Errorf("userSetsLoadRestrictor(%q) = %v, want %v", tt.args, got, tt.want)
			}
			if !slices.Equal(tt.args, before) {
				t.Errorf("the arguments were modified: %q, was %q", tt.args, before)
			}
		})
	}
}
