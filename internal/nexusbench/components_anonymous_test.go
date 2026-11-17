package nexusbench

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// ListImagesAnonymously is the listing of the status command (DEVDESK-544): it
// reads the components of a repository with no credential, and a refusal of the
// instance is an *APIError, which status turns into "unknown". The fake Nexus
// answers 401 to a request without credentials, so a plain handler stands in.

func TestListImagesAnonymouslyReadsNameTagAndDigestWithoutCredentials(t *testing.T) {
	var sawAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = sawAuth || r.Header.Get("Authorization") != ""
		if r.URL.Path != componentsPath || r.URL.Query().Get("repository") != dockerHostedRepository {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"items":[
			{"name":"team/api","version":"1.0.0","assets":[{"path":"v2/team/api/manifests/1.0.0","checksum":{"sha256":"` + digestAlpine[len("sha256:"):] + `"}}]},
			{"name":"api","version":"1.1.0","assets":[]}],"continuationToken":null}`))
	}))
	defer srv.Close()

	got, err := ListImagesAnonymously(context.Background(), srv.URL, dockerHostedRepository)
	if err != nil {
		t.Fatalf("ListImagesAnonymously: %v", err)
	}
	want := []ListedImage{
		{Name: "team/api", Tag: "1.0.0", Digest: digestAlpine},
		{Name: "api", Tag: "1.1.0"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("images = %+v\nwant %+v", got, want)
	}
	if sawAuth {
		t.Error("an Authorization header was sent")
	}
}

func TestListImagesAnonymouslyReportsARefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := ListImagesAnonymously(context.Background(), srv.URL, dockerHostedRepository)
	var api *APIError
	if !errors.As(err, &api) || api.Status != http.StatusUnauthorized {
		t.Errorf("error = %v, want an *APIError with status 401", err)
	}
}
