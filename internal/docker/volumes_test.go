package docker

import (
	"slices"
	"testing"
)

func TestListVolumesParsesOutput(t *testing.T) {
	stubOutput(t, "volume",
		"pgdata\tlocal\t/var/lib/docker/volumes/pgdata/_data\n"+
			"cache\tlocal\n")

	got, err := ListVolumes()

	if err != nil {
		t.Fatalf("ListVolumes() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListVolumes() returned %d volumes, want 2", len(got))
	}
	want := Volume{Name: "pgdata", Driver: "local", Mountpoint: "/var/lib/docker/volumes/pgdata/_data"}
	if got[0] != want {
		t.Errorf("first volume = %+v, want %+v", got[0], want)
	}
	// A missing mountpoint must not drop the row.
	if got[1].Name != "cache" || got[1].Mountpoint != "" {
		t.Errorf("second volume = %+v, want cache with no mountpoint", got[1])
	}
}

func TestCreateVolumeOmitsEmptyDriver(t *testing.T) {
	tests := []struct {
		name     string
		driver   string
		wantArgs []string
	}{
		{"with driver", "local", []string{"volume", "create", "--driver", "local", "data"}},
		{"without driver lets docker choose", "", []string{"volume", "create", "data"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := stub(t, &stubRunner{})

			if err := CreateVolume("data", tt.driver); err != nil {
				t.Fatalf("CreateVolume() error = %v", err)
			}
			if !slices.Equal(s.lastArgs(), tt.wantArgs) {
				t.Errorf("args = %v, want %v", s.lastArgs(), tt.wantArgs)
			}
		})
	}
}

func TestRemoveVolume(t *testing.T) {
	s := stub(t, &stubRunner{})

	if err := RemoveVolume("data"); err != nil {
		t.Fatalf("RemoveVolume() error = %v", err)
	}
	if want := []string{"volume", "rm", "data"}; !slices.Equal(s.lastArgs(), want) {
		t.Errorf("args = %v, want %v", s.lastArgs(), want)
	}
}
