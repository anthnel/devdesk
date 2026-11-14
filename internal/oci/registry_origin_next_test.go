package oci

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// DEVDESK-410 review round. This file holds the test that names the new
// sentinel ErrTagsNextOffOrigin, kept apart so the rest of the package still
// compiles while that symbol does not exist.

// A Link rel="next" to another origin is an error, not a silent short list.
func TestListRegistryTagsNextOffOriginIsAnError(t *testing.T) {
	var other hitLog
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		other.record(r)
		_, _ = fmt.Fprint(w, `{"tags":["evil"]}`)
	}))
	defer foreign.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<`+foreign.URL+`/v2/x/tags/list?last=SECRET>; rel="next"`)
		_, _ = fmt.Fprint(w, `{"tags":["a"]}`)
	}))
	defer srv.Close()

	_, err := ListRegistryTagsContext(context.Background(), srv.URL, "x", "user", "pass")
	if err == nil {
		t.Fatal("err = nil, want an error for a next link to another origin")
	}
	if !errors.Is(err, ErrTagsNextOffOrigin) {
		t.Errorf("err = %v, want errors.Is ErrTagsNextOffOrigin", err)
	}
	// Only the destination host may be named, never its path or query.
	for _, leak := range []string{"SECRET", "last=", "/v2/x/tags/list"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("error %q leaks %q from the next link", err, leak)
		}
	}
	if other.count() != 0 {
		t.Errorf("other origin hit %d times, want 0", other.count())
	}
	if a := other.anyAuth(); a != "" {
		t.Errorf("credentials reached the other origin: %q", a)
	}
}
