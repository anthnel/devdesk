package oci

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// DEVDESK-412 final audit: a registry written with its password never reaches an
// error or a log, whether or not it carries a scheme. The registry config holds
// both forms ("https://r.example" and "r.example:5000").

func TestRedactRegistryURL(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"empty", "", ""},
		{"with scheme", "https://alice:" + realmTestPassword + "@r.example:5000", "https://alice:xxxxx@r.example:5000"},
		{"no scheme, host and port", "registry.example.com:5000", "registry.example.com:5000"},
		{"no scheme, bare host", "docker.io", "docker.io"},
		{"no scheme, userinfo", "alice:" + realmTestPassword + "@r.example:5000", "alice:xxxxx@r.example:5000"},
		{"no scheme, userinfo and path", "alice:" + realmTestPassword + "@r.example/team", "alice:xxxxx@r.example/team"},
		{"unreadable", "https://alice:" + realmTestPassword + "@r.example/%zz", "<invalid URL>"},
		{"unreadable, no scheme", "alice:" + realmTestPassword + "@r.example:bad", "<invalid URL>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactRegistryURL(tc.in)
			if got != tc.want {
				t.Errorf("RedactRegistryURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if strings.Contains(got, realmTestPassword) {
				t.Errorf("RedactRegistryURL(%q) = %q holds the password", tc.in, got)
			}
		})
	}
}

// Client.get builds its request with a registry URL the user typed: a URL that
// cannot be read must not come back whole in the error.
func TestClientBuildRequestErrorHidesThePassword(t *testing.T) {
	c := NewClient("https://alice:"+realmTestPassword+"@registry.example/%zz", "", "")

	t.Run("ListTags", func(t *testing.T) {
		_, err := c.ListTags("tpl")
		wantNoPassword(t, err)
	})
	t.Run("DownloadTemplate", func(t *testing.T) {
		_, err := c.DownloadTemplate("tpl", "v1")
		wantNoPassword(t, err)
	})
}

// The status error of ListTags names the URL it asked for.
func TestClientListTagsStatusErrorHidesThePassword(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	_, err := NewClient(withPassword(srv.URL), "", "").ListTags("tpl")

	wantNoPassword(t, err)
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error %q lost the status (the test checks nothing)", err)
	}
}
