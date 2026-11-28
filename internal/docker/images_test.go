package docker

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
)

func TestImageName(t *testing.T) {
	tests := []struct {
		name string
		img  Image
		want string
	}{
		{"repository and tag", Image{Repository: "nginx", Tag: "1.25"}, "nginx:1.25"},
		{"untagged images omit the suffix", Image{Repository: "nginx", Tag: "<none>"}, "nginx"},
		{"empty tag omits the suffix", Image{Repository: "nginx"}, "nginx"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.img.Name(); got != tt.want {
				t.Errorf("Name() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A dangling image has no usable name, so Trivy must be pointed at the ID.
// Passing "repository:latest" would make it try to pull from a registry.
func TestImageScanTarget(t *testing.T) {
	tests := []struct {
		name string
		img  Image
		want string
	}{
		{"tagged images scan by name", Image{ID: "abc123", Repository: "nginx", Tag: "1.25"}, "nginx:1.25"},
		{"untagged images scan by ID", Image{ID: "abc123", Repository: "nginx", Tag: "<none>"}, "abc123"},
		{"empty tag scans by ID", Image{ID: "abc123", Repository: "nginx"}, "abc123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.img.ScanTarget(); got != tt.want {
				t.Errorf("ScanTarget() = %q, want %q", got, tt.want)
			}
		})
	}
}

// What ListImages reads depends on the engine twice over: the `image ls` and
// `image inspect` templates are the shape's, and `system df -v` is scraped only
// where the shape says its table is known (ParsesSystemDFVerbose).
func TestListImagesParsesAndEnriches(t *testing.T) {
	// `docker system df -v` is fixed-width; the columns must line up with the
	// header offsets or the parser reads the wrong slices.
	df := "Images space usage:\n\n" +
		"REPOSITORY          TAG                 IMAGE ID            CREATED             SIZE                SHARED SIZE         UNIQUE SIZE         CONTAINERS\n" +
		"nginx               1.25                abc123456789        2 days ago          142MB               0B                  142MB               2\n" +
		"postgres            16                  def123456789        3 days ago          400MB               0B                  400MB               0\n\n" +
		"Containers space usage:\n\n" +
		"CONTAINER ID        IMAGE               COMMAND\n"

	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		replies := []reply{
			{
				argv: []string{"image", "ls", "--format", shape.Templates.ImageLS},
				out: "abc123456789\tnginx\t1.25\t140MB\t2026-08-01 10:00:00\n" +
					"def123456789\tpostgres\t16\t400MB\t2026-07-30 10:00:00\n",
			},
			{argv: []string{"image", "inspect", "--format", shape.Templates.ImageInspect, "abc123456789", "def123456789"}},
		}
		// nginx: unique size and container count as `image ls` and df -v give
		// them. Without the df table only the `image ls` size is known and the
		// count stays zero, which the table renders as a dim "-".
		wantUnique, wantContainers := int64(140_000_000), 0
		if shape.ParsesSystemDFVerbose() {
			replies = append(replies, reply{argv: []string{"system", "df", "-v"}, out: df})
			wantUnique, wantContainers = 142_000_000, 2
		}
		stubStrict(t, replies...)

		got, err := ListImages()

		if err != nil {
			t.Fatalf("ListImages() error = %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("ListImages() returned %d images, want 2", len(got))
		}
		if got[0].UniqueSize != wantUnique {
			t.Errorf("%s: UniqueSize = %d, want %d", shape.Name, got[0].UniqueSize, wantUnique)
		}
		if got[0].Containers != wantContainers {
			t.Errorf("%s: Containers = %d, want %d", shape.Name, got[0].Containers, wantContainers)
		}
		if got[1].Containers != 0 {
			t.Errorf("%s: Containers = %d for postgres, want 0", shape.Name, got[1].Containers)
		}
	})
}

func TestParseImagesDiskUsage(t *testing.T) {
	header := "REPOSITORY          TAG                 IMAGE ID            CREATED             SIZE                SHARED SIZE         UNIQUE SIZE         CONTAINERS\n"

	t.Run("reads unique size and container count", func(t *testing.T) {
		out := "Images space usage:\n\n" + header +
			"nginx               1.25                abc123              2 days ago          142MB               0B                  100MB               3\n"

		got := parseImagesDiskUsage(out)

		info, ok := got["nginx:1.25"]
		if !ok {
			t.Fatalf("parseImagesDiskUsage() = %v, missing the nginx:1.25 key", got)
		}
		if info.UniqueSize != 100_000_000 || info.Containers != 3 {
			t.Errorf("info = %+v, want {100000000 3}", info)
		}
	})

	t.Run("stops at the containers section", func(t *testing.T) {
		out := "Images space usage:\n\n" + header +
			"nginx               1.25                abc123              2 days ago          142MB               0B                  100MB               1\n\n" +
			"Containers space usage:\n\n" +
			"leaked              row                 zzz                 1 day ago           1MB                 0B                  1MB                 9\n"

		got := parseImagesDiskUsage(out)

		if _, leaked := got["leaked:row"]; leaked {
			t.Error("parseImagesDiskUsage() read past the Images section into Containers")
		}
		if len(got) != 1 {
			t.Errorf("parseImagesDiskUsage() = %v, want only the image row", got)
		}
	})

	t.Run("returns nothing without a header", func(t *testing.T) {
		out := "Images space usage:\n\nnginx 1.25 abc123 2 days ago 142MB 0B 100MB 1\n"

		if got := parseImagesDiskUsage(out); len(got) != 0 {
			t.Errorf("parseImagesDiskUsage() = %v, want empty: column offsets are unknown", got)
		}
	})

	t.Run("skips rows with no repository or tag", func(t *testing.T) {
		out := "Images space usage:\n\n" + header +
			"                                        abc123              2 days ago          142MB               0B                  100MB               1\n"

		if got := parseImagesDiskUsage(out); len(got) != 0 {
			t.Errorf("parseImagesDiskUsage() = %v, want empty", got)
		}
	})
}

func TestParseExposedPorts(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{"single port", `{"80/tcp":{}}`, []string{"80/tcp"}},
		{"several ports", `{"80/tcp":{},"443/tcp":{},"53/udp":{}}`, []string{"80/tcp", "443/tcp", "53/udp"}},
		{"images with no EXPOSE report null", "null", nil},
		{"empty output", "", nil},
		{"empty object", "{}", nil},
		{"trailing newline is tolerated", "{\"80/tcp\":{}}\n", []string{"80/tcp"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseExposedPorts(tt.raw); !slices.Equal(got, tt.want) {
				t.Errorf("parseExposedPorts(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

// RemoveImage has no force option since OC-22 (DEVDESK-820): the argv is `rmi <id>`.
func TestRemoveImageIsNeverForced(t *testing.T) {
	s := stub(t, &stubRunner{})

	if err := RemoveImage("0123456789ab"); err != nil {
		t.Fatalf("RemoveImage() error = %v", err)
	}
	if want := []string{"rmi", "0123456789ab"}; !slices.Equal(s.lastArgs(), want) {
		t.Errorf("args = %v, want %v", s.lastArgs(), want)
	}
}

// The pull is the one mutating call that carries a context: `K` is offered on
// it in `:jobs`, and a nil one there would make that key a silent no-op.
func TestPullCarriesACancellableContext(t *testing.T) {
	s := stub(t, &stubRunner{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := PullImageContext(ctx, "api:v1"); err != nil {
		t.Fatalf("PullImageContext() error = %v", err)
	}

	call := s.calls[len(s.calls)-1]
	if !slices.Equal(call.Args, []string{"pull", "api:v1"}) {
		t.Errorf("args = %v, want the pull of the named image", call.Args)
	}
	if call.Ctx == nil {
		t.Error("the pull reached the runner with no context, so K could not stop it")
	}
}

// PullImage keeps its signature for callers with nothing to cancel, and still
// goes through the same path.
func TestPullImageStillPulls(t *testing.T) {
	s := stub(t, &stubRunner{})

	if err := PullImage("api:v1"); err != nil {
		t.Fatalf("PullImage() error = %v", err)
	}
	if !slices.Equal(s.lastArgs(), []string{"pull", "api:v1"}) {
		t.Errorf("args = %v, want the pull of the named image", s.lastArgs())
	}
}

// Everything else keeps building the command it built before — a context on a
// delete would offer a stop that must never be honoured (D7).
func TestOtherMutationsCarryNoContext(t *testing.T) {
	s := stub(t, &stubRunner{})

	if err := RemoveImage("0123456789ab"); err != nil {
		t.Fatalf("RemoveImage() error = %v", err)
	}
	if call := s.calls[len(s.calls)-1]; call.Ctx != nil {
		t.Error("a removal carries a context, so something could ask to cut it")
	}
}

func TestFirstWord(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want string
	}{
		{"splits at the first space", "1.25 abc123", "1.25"},
		{"single word is returned whole", "1.25", "1.25"},
		{"empty stays empty", "", ""},
		{"a leading space yields an empty word", " x", " x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstWord(tt.s); got != tt.want {
				t.Errorf("firstWord(%q) = %q, want %q", tt.s, got, tt.want)
			}
		})
	}
}

// The inspect line carries the registry digests as JSON; an image built locally
// has none, and says `[]`.
func TestParseImageInspectReadsTheRepoDigests(t *testing.T) {
	out := []byte("sha256:abc123456789ffff\t1000\t[\"alpine@sha256:aa\",\"docker.io/library/alpine@sha256:aa\"]\n" +
		"def123456789\t2000\t[]\n" +
		"0123456789ab\t3000\n")
	got := parseImageInspect(out)
	if a := got["abc123456789"]; a.size != 1000 || !slices.Equal(a.repoDigests, []string{"alpine@sha256:aa", "docker.io/library/alpine@sha256:aa"}) {
		t.Errorf("pulled image = %+v", a)
	}
	if d := got["def123456789"]; d.size != 2000 || len(d.repoDigests) != 0 {
		t.Errorf("built image = %+v", d)
	}
	if o := got["0123456789ab"]; o.size != 3000 || o.repoDigests != nil {
		t.Errorf("line without digests = %+v", o)
	}
}

// A container's digests are those of the image it runs, read through its image
// ID: a pull of the same tag since then does not change them.
func TestContainerImageDigestsFollowTheImageTheContainerRuns(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		stubStrict(t,
			reply{
				argv: []string{"container", "inspect", "--format", "{{.Image}}", "c1", "c2"},
				out:  "sha256:aaaaaaaaaaaa1111\nsha256:bbbbbbbbbbbb2222\n",
			},
			reply{
				argv: []string{"image", "inspect", "--format", shape.Templates.ImageInspect,
					"sha256:aaaaaaaaaaaa1111", "sha256:bbbbbbbbbbbb2222"},
				out: "sha256:aaaaaaaaaaaa1111\t10\t[\"alpine@sha256:old\"]\n" +
					"sha256:bbbbbbbbbbbb2222\t20\t[]\n",
			})

		got := ContainerImageDigests([]string{"c1", "c2"})

		if !slices.Equal(got["c1"], []string{"alpine@sha256:old"}) {
			t.Errorf("c1 = %v", got["c1"])
		}
		if _, ok := got["c2"]; ok {
			t.Errorf("a built image has digests: %v", got["c2"])
		}
	})
}

func TestSameImageID(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want bool
	}{
		{"sha256:abc123456789ffff", "abc123456789", true},
		{"abc123456789", "abc123456789", true},
		{"sha256:abc123456789ffff", "def123456789", false},
		{"", "abc", false},
	} {
		if got := SameImageID(tt.a, tt.b); got != tt.want {
			t.Errorf("SameImageID(%q, %q) = %v", tt.a, tt.b, got)
		}
	}
}

func TestContainersUsingImageFiltersByAncestor(t *testing.T) {
	r := &stubRunner{output: map[string][]byte{"ps": []byte("web\nworker\n")}}
	stub(t, r)
	got, err := ContainersUsingImage("abc123")
	if err != nil || !slices.Equal(got, []string{"web", "worker"}) {
		t.Fatalf("ContainersUsingImage = %v, %v", got, err)
	}
	if args := r.calls[len(r.calls)-1].Args; !slices.Contains(args, "ancestor=abc123") || !slices.Contains(args, "-a") {
		t.Errorf("args = %v, want every container filtered by ancestor", args)
	}
}

// ImagesInUse is the join by image ID behind the preview of `P` on the Images tab
// (OC-22): one `ps -a --filter ancestor=<id>` per image, fail closed.
func TestImagesInUse(t *testing.T) {
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		ps := func(id string) []string {
			return []string{"ps", "-a", "--filter", "ancestor=" + id, "--format", "{{.Names}}"}
		}
		stubStrict(t,
			reply{argv: ps("d1d1d1d1d1d1"), out: "web\nworker\n"},
			reply{argv: ps("d2d2d2d2d2d2")},
		)

		users, err := ImagesInUse([]string{"d1d1d1d1d1d1", "sha256:d2d2d2d2d2d2"})

		if err != nil {
			t.Fatalf("ImagesInUse() = %v", err)
		}
		if got := users["d1d1d1d1d1d1"]; !slices.Equal(got, []string{"web", "worker"}) {
			t.Errorf("users of the first image = %v, want web and worker", got)
		}
		if len(users) != 1 {
			t.Errorf("users = %v, want only the image a container uses", users)
		}
	})
}

func TestImagesInUseFailsClosed(t *testing.T) {
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		stubStrict(t, reply{
			argv: []string{"ps", "-a", "--filter", "ancestor=d1d1d1d1d1d1", "--format", "{{.Names}}"},
			err:  errors.New("exit status 1"),
		})
		if users, err := ImagesInUse([]string{"d1d1d1d1d1d1"}); err == nil || users != nil {
			t.Errorf("ImagesInUse() = %v, %v, want an error and no map when the engine fails", users, err)
		}

		stubStrict(t) // no declared argv: an invalid ID builds nothing
		if _, err := ImagesInUse([]string{"-f"}); !errors.Is(err, ErrInvalidImageID) {
			t.Errorf("ImagesInUse(-f) = %v, want ErrInvalidImageID and no call", err)
		}
	})
}
