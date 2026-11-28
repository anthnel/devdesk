package docker

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
)

// A volume is never "unused" on the word of a usage nobody read: the zero value, an
// empty name and the usage returned with an error say "unknown", like images and
// networks (OC-25 review).
func TestVolumeUsersOfAUsageNeverReadIsUnknown(t *testing.T) {
	var zero ContainerUsage
	if got, known := zero.VolumeUsers("pgdata"); known || len(got) != 0 {
		t.Errorf("zero.VolumeUsers = %v, known %v; want unknown", got, known)
	}
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		ctrs := []usageCtr{{id: usageID(1), name: "db", state: "running", image: usageImg,
			mounts: []map[string]any{volumeMount("pgdata")}, networks: json.RawMessage(`{}`)}}
		u, _, err := usageOf(t, shape, ctrs)
		if err != nil {
			t.Fatalf("ListContainerUsage() error = %v", err)
		}
		if got, known := u.VolumeUsers("pgdata"); !known || !slices.Equal(got, []string{"db"}) {
			t.Errorf("VolumeUsers(pgdata) = %v, %v; want db, known", got, known)
		}
		if got, known := u.VolumeUsers("free"); !known || len(got) != 0 {
			t.Errorf("VolumeUsers(free) = %v, %v; want nobody, known", got, known)
		}
		if got, known := u.VolumeUsers(""); known {
			t.Errorf("VolumeUsers(\"\") = %v, known; want unknown", got)
		}

		e := &usageEngine{t: t, psErr: errUnexpectedArgv}
		e.install(t, shape.Name)
		failed, err := ListContainerUsage()
		if err == nil {
			t.Fatal("ListContainerUsage() error = nil, want the ps failure")
		}
		if got, known := failed.VolumeUsers("pgdata"); known {
			t.Errorf("the usage returned with an error answers VolumeUsers = %v, known", got)
		}
	})
}

// An empty Networks object is "on no network", known; `none` is a key and is known too.
// A container that shares the network of another one (`container:<id>`) says nothing in
// its own Networks: unknown, for every network. Not measured: that mode, host, macvlan,
// ipvlan, pods.
func TestAnEmptyNetworksObjectIsKnownExceptForASharedNetwork(t *testing.T) {
	cases := []struct {
		name      string
		networks  string
		mode      string
		wantKnown bool
		wantNames []string
	}{
		{"empty object, bridge mode", `{}`, "bridge", true, nil},
		{"empty object, no mode", `{}`, "", true, nil},
		{"none is a key", `{"none":{"NetworkID":"none"}}`, "none", true, []string{"web"}},
		{"shared with another container", `{}`, "container:" + usageID(2), false, nil},
	}
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				web := usageCtr{id: usageID(1), name: "web", state: "running", image: usageImg,
					networks: json.RawMessage(tt.networks), networkMode: tt.mode}
				u, _, err := usageOf(t, shape, []usageCtr{web})
				if err != nil {
					t.Fatalf("ListContainerUsage() error = %v", err)
				}

				query := "none"
				if tt.name != "none is a key" {
					query = "appnet"
				}
				got, known := u.NetworkUsers(Network{Name: query})

				if known != tt.wantKnown || !slices.Equal(got, tt.wantNames) && len(got)+len(tt.wantNames) > 0 {
					t.Errorf("NetworkUsers(%s) = %v, %v; want %v, %v", query, got, known, tt.wantNames, tt.wantKnown)
				}
			})
		}
	})
}
