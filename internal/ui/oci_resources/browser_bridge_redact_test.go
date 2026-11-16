package ociresources

import (
	"errors"
	"strings"
	"testing"
)

// DEVDESK-412 step 8 (group B): the registry browser logs the registry's address
// when a tag listing or its dates fail. An address written with its password
// (https://user:pass@host) must not reach the log.
func TestBrowserBridgeLogsDoNotCarryTheRegistryPassword(t *testing.T) {
	const (
		secret = "S3CRETPW"
		raw    = "https://alice:" + secret + "@r.example"
	)
	withTagLister(t)
	logs := captureLog(t)

	m, _ := submit(t, browsingModel(t), "api")
	gen := m.registryBrowser.gen

	t.Run("tags failed", func(t *testing.T) {
		logs.Reset()
		feed(t, m, MultiRegistryTagsLoadedMsg{
			Gen: gen, EntryKey: "prod", RegistryURL: raw, Alias: "prod", Repo: "api", Err: errors.New("boom"),
		})
		if !strings.Contains(logs.String(), "r.example") {
			t.Fatalf("the failure was not logged with the registry (the test checks nothing): %q", logs.String())
		}
		if strings.Contains(logs.String(), secret) {
			t.Errorf("the log holds the password: %q", logs.String())
		}
	})

	t.Run("tag dates unavailable", func(t *testing.T) {
		logs.Reset()
		feed(t, m, MultiRegistryTagsMetaMsg{
			Gen: gen, EntryKey: "prod", RegistryURL: raw, Repo: "api", Err: errors.New("boom"),
		})
		if !strings.Contains(logs.String(), "r.example") {
			t.Fatalf("the failure was not logged with the registry (the test checks nothing): %q", logs.String())
		}
		if strings.Contains(logs.String(), secret) {
			t.Errorf("the log holds the password: %q", logs.String())
		}
	})
}
