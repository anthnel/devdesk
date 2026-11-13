package imagesearch

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// recorder is a Searcher that counts its calls and answers one fixed image.
type recorder struct {
	mu    sync.Mutex
	calls []Target
	name  string
}

func (r *recorder) Search(_ context.Context, t Target, _ Query) ([]Image, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, t)
	return []Image{{Name: r.name}}, false, nil
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func TestErrUnsupportedIsAnErrorWithAMessage(t *testing.T) {
	if ErrUnsupported == nil || ErrUnsupported.Error() == "" {
		t.Fatal("ErrUnsupported must be a non-empty error")
	}
}

// Hub is recognised through oci.IsDockerHub, whatever the spelling.
func TestByRegistrySendsEveryHubSpellingToHub(t *testing.T) {
	for _, url := range []string{
		"docker.io", "https://docker.io", "http://docker.io/", "DOCKER.IO",
		"registry-1.docker.io", "registry-1.docker.io/", "https://registry-1.docker.io",
	} {
		t.Run(url, func(t *testing.T) {
			hub, other := &recorder{name: "hub"}, &recorder{name: "other"}
			imgs, _, err := ByRegistry(hub, other).Search(context.Background(), Target{RegistryURL: url}, Query{Text: "nginx"})
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if hub.count() != 1 || other.count() != 0 {
				t.Errorf("hub calls = %d, other calls = %d, want 1 and 0", hub.count(), other.count())
			}
			if len(imgs) != 1 || imgs[0].Name != "hub" {
				t.Errorf("images = %v, want the hub's answer", imgs)
			}
		})
	}
}

// With no other searcher, a non-Hub registry is unsupported and nothing is
// requested anywhere.
func TestByRegistryAnswersErrUnsupportedForANonHubWithoutAnyRequest(t *testing.T) {
	for _, url := range []string{"registry.example.com", "https://nexus.example.com/repository/docker-group", "dhi.io", ""} {
		t.Run(url, func(t *testing.T) {
			hub := &recorder{name: "hub"}
			imgs, partial, err := ByRegistry(hub, nil).Search(context.Background(), Target{RegistryURL: url}, Query{Text: "nginx"})
			if !errors.Is(err, ErrUnsupported) {
				t.Fatalf("err = %v, want ErrUnsupported", err)
			}
			if len(imgs) != 0 || partial {
				t.Errorf("images = %v, partial = %v, want nothing", imgs, partial)
			}
			if hub.count() != 0 {
				t.Errorf("hub was called %d times for a non-Hub registry", hub.count())
			}
		})
	}
}

func TestByRegistrySendsANonHubToTheOtherSearcher(t *testing.T) {
	hub, other := &recorder{name: "hub"}, &recorder{name: "other"}
	imgs, _, err := ByRegistry(hub, other).Search(context.Background(), Target{RegistryURL: "registry.example.com"}, Query{Text: "nginx"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if hub.count() != 0 || other.count() != 1 {
		t.Errorf("hub calls = %d, other calls = %d, want 0 and 1", hub.count(), other.count())
	}
	if len(imgs) != 1 || imgs[0].Name != "other" {
		t.Errorf("images = %v, want the other searcher's answer", imgs)
	}
}

// A nil hub is as unsupported as a nil other: no panic.
func TestByRegistryWithANilHubAnswersErrUnsupported(t *testing.T) {
	_, _, err := ByRegistry(nil, nil).Search(context.Background(), Target{RegistryURL: "docker.io"}, Query{Text: "nginx"})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
}
