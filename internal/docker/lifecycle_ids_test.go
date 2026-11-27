package docker

import (
	"errors"
	"slices"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
)

// The lifecycle verbs that are not destructive (restart, pause, unpause) take a
// container ID the way stop and rm do, so they refuse a malformed one before any
// argv is built (DEVDESK-803). The destructive ones are in destructive_ids_test.go.
var lifecycleByID = []struct {
	name string
	verb string
	call func(id string) error
}{
	{"RestartContainer", "restart", RestartContainer},
	{"PauseContainer", "pause", PauseContainer},
	{"UnpauseContainer", "unpause", UnpauseContainer},
}

func TestLifecycleFunctionsRefuseAMalformedContainerID(t *testing.T) {
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		for _, fn := range lifecycleByID {
			for _, bad := range badIDs[kindContainer] {
				t.Run(fn.name+"/"+bad.label, func(t *testing.T) {
					s := stubStrict(t)

					err := fn.call(bad.id)

					if !errors.Is(err, ErrInvalidContainerID) {
						t.Errorf("%s(%q) = %v, want it to wrap ErrInvalidContainerID", fn.name, bad.id, err)
					}
					if len(s.calls) != 0 {
						t.Errorf("%s(%q) reached the runner with %q", fn.name, bad.id, s.lastArgs())
					}
				})
			}
		}
	})
}

func TestLifecycleFunctionsPassAValidContainerIDAsOneArgvElement(t *testing.T) {
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		for _, fn := range lifecycleByID {
			for _, id := range goodIDs[kindContainer] {
				t.Run(fn.name+"/"+id[:12], func(t *testing.T) {
					want := []string{fn.verb, id}
					s := stubStrict(t, reply{argv: want})

					if err := fn.call(id); err != nil {
						t.Fatalf("%s(%q) = %v", fn.name, id, err)
					}
					if len(s.calls) != 1 || !slices.Equal(s.calls[0].Args, want) {
						t.Errorf("%s(%q) ran %v, want one run of %q", fn.name, id, s.calls, want)
					}
				})
			}
		}
	})
}

// An image ID may come with the prefix some engines print; the prefix alone, or
// a reference, is not an ID.
func TestIsImageIDAcceptsTheShaPrefixOnly(t *testing.T) {
	for id, want := range map[string]bool{
		"0123456789ab":                 true,
		"sha256:0123456789ab":          true,
		"sha256:":                      false,
		"sha256:sha256:0123456789ab":   false,
		"nginx:latest":                 false,
		"sha256:0123456789AB":          false,
		"docker.io/library/nginx:1.27": false,
	} {
		if got := IsImageID(id); got != want {
			t.Errorf("IsImageID(%q) = %v, want %v", id, got, want)
		}
	}
}
