package docker

import (
	"context"
	"slices"
	"testing"
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

func TestListImagesParsesAndEnriches(t *testing.T) {
	// `docker system df -v` is fixed-width; the columns must line up with the
	// header offsets or the parser reads the wrong slices.
	df := "Images space usage:\n\n" +
		"REPOSITORY          TAG                 IMAGE ID            CREATED             SIZE                SHARED SIZE         UNIQUE SIZE         CONTAINERS\n" +
		"nginx               1.25                abc123456789        2 days ago          142MB               0B                  142MB               2\n" +
		"postgres            16                  def123456789        3 days ago          400MB               0B                  400MB               0\n\n" +
		"Containers space usage:\n\n" +
		"CONTAINER ID        IMAGE               COMMAND\n"

	stub(t, &stubRunner{output: map[string][]byte{
		"image": []byte("abc123456789\tnginx\t1.25\t140MB\t2026-08-01 10:00:00\n" +
			"def123456789\tpostgres\t16\t400MB\t2026-07-30 10:00:00\n"),
		"system": []byte(df),
	}})

	got, err := ListImages()

	if err != nil {
		t.Fatalf("ListImages() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListImages() returned %d images, want 2", len(got))
	}
	// UniqueSize starts from `docker image ls` then df -v overrides it.
	if got[0].UniqueSize != 142_000_000 {
		t.Errorf("UniqueSize = %d, want 142000000 from df -v", got[0].UniqueSize)
	}
	if got[0].Containers != 2 {
		t.Errorf("Containers = %d, want 2", got[0].Containers)
	}
	if got[1].Containers != 0 {
		t.Errorf("Containers = %d for postgres, want 0", got[1].Containers)
	}
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

func TestRemoveImageForceFlag(t *testing.T) {
	tests := []struct {
		name     string
		force    bool
		wantArgs []string
	}{
		{"without force", false, []string{"rmi", "abc"}},
		{"with force", true, []string{"rmi", "-f", "abc"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := stub(t, &stubRunner{})

			if err := RemoveImage("abc", tt.force); err != nil {
				t.Fatalf("RemoveImage() error = %v", err)
			}
			if !slices.Equal(s.lastArgs(), tt.wantArgs) {
				t.Errorf("args = %v, want %v", s.lastArgs(), tt.wantArgs)
			}
		})
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

	if err := RemoveImage("abc", false); err != nil {
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
