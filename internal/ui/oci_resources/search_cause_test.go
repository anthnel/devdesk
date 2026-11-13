package ociresources

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestSearchErrorCauseDropsTheURL(t *testing.T) {
	err := &url.Error{
		Op:  "Get",
		URL: "https://index.docker.io/v1/search?q=my-typed-secret&n=50",
		Err: errors.New("connection refused"),
	}
	got := searchErrorCause(err)
	if strings.Contains(got, "my-typed-secret") || strings.Contains(got, "index.docker.io") {
		t.Errorf("cause %q leaks the URL", got)
	}
	if !strings.Contains(got, "Get") || !strings.Contains(got, "connection refused") {
		t.Errorf("cause %q lost the operation or the error", got)
	}
	wrapped := errors.Join(errors.New("hub search"), err)
	if got := searchErrorCause(wrapped); strings.Contains(got, "my-typed-secret") {
		t.Errorf("wrapped cause %q leaks the query", got)
	}
}
