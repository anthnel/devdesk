package docker

import (
	"slices"
	"testing"
)

// Docker stores Hub credentials under "https://index.docker.io/v1/" no matter
// which alias the user typed, so a lookup for any alias must try all of them or
// a logged-in user reads as logged out.
func TestRegistryCandidatesExpandsDockerHubAliases(t *testing.T) {
	for _, alias := range []string{"docker.io", "https://index.docker.io/v1/", "registry-1.docker.io"} {
		got := registryCandidates(alias)

		if !slices.Contains(got, "https://index.docker.io/v1/") {
			t.Errorf("registryCandidates(%q) = %v, missing the canonical Hub key", alias, got)
		}
		if len(got) != len(dockerHubKeys) {
			t.Errorf("registryCandidates(%q) returned %d candidates, want the full Hub set", alias, len(got))
		}
	}
}

func TestRegistryCandidatesForPrivateRegistries(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want []string
	}{
		{
			name: "bare hostname gains both schemes",
			url:  "registry.example.com",
			want: []string{"registry.example.com", "https://registry.example.com", "http://registry.example.com"},
		},
		{
			name: "https URL gains the bare hostname",
			url:  "https://registry.example.com",
			want: []string{"https://registry.example.com", "registry.example.com"},
		},
		{
			name: "http URL gains the bare hostname",
			url:  "http://registry.example.com",
			want: []string{"http://registry.example.com", "registry.example.com"},
		},
		{
			name: "a trailing slash is normalised away",
			url:  "registry.example.com/",
			want: []string{"registry.example.com", "https://registry.example.com", "http://registry.example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := registryCandidates(tt.url); !slices.Equal(got, tt.want) {
				t.Errorf("registryCandidates(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestDecodeAuth(t *testing.T) {
	tests := []struct {
		name         string
		auth         string
		wantUser     string
		wantPassword string
		wantOK       bool
	}{
		{"user and password", "dXNlcjpwYXNz", "user", "pass", true},                // user:pass
		{"password containing a colon", "dXNlcjpwOmFzcw==", "user", "p:ass", true}, // user:p:ass
		{"empty password is still a pair", "dXNlcjo=", "user", "", true},           // user:
		{"not base64", "!!!not-base64!!!", "", "", false},
		{"no colon separator", "dXNlcg==", "", "", false}, // user
		{"empty input", "", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, password, ok := decodeAuth(tt.auth)

			if ok != tt.wantOK {
				t.Fatalf("decodeAuth(%q) ok = %v, want %v", tt.auth, ok, tt.wantOK)
			}
			if user != tt.wantUser || password != tt.wantPassword {
				t.Errorf("decodeAuth(%q) = %q/%q, want %q/%q", tt.auth, user, password, tt.wantUser, tt.wantPassword)
			}
		})
	}
}

// The password must reach docker over stdin: putting it in argv would expose it
// in the host process list.
func TestRegistryLoginPassesPasswordOnStdin(t *testing.T) {
	s := stub(t, &stubRunner{})

	if err := RegistryLogin("registry.example.com", "alice", "s3cret"); err != nil {
		t.Fatalf("RegistryLogin() error = %v", err)
	}

	call := s.calls[0]
	if call.Stdin != "s3cret" {
		t.Errorf("Stdin = %q, want the password", call.Stdin)
	}
	if slices.Contains(call.Args, "s3cret") {
		t.Error("the password appears in argv, where the process list exposes it")
	}
	if !slices.Contains(call.Args, "--password-stdin") {
		t.Errorf("args = %v, missing --password-stdin", call.Args)
	}
}

func TestGetCredsFromHelperUsesTheHelperBinary(t *testing.T) {
	s := stub(t, &stubRunner{
		output: map[string][]byte{"get": []byte(`{"Username":"alice","Secret":"s3cret"}`)},
	})

	user, password, ok := getCredsFromHelper("osxkeychain", "registry.example.com")

	if !ok {
		t.Fatal("getCredsFromHelper() ok = false, want true")
	}
	if user != "alice" || password != "s3cret" {
		t.Errorf("getCredsFromHelper() = %q/%q, want alice/s3cret", user, password)
	}
	if s.calls[0].Helper != "osxkeychain" {
		t.Errorf("Helper = %q, want the call routed to docker-credential-osxkeychain", s.calls[0].Helper)
	}
	// The server URL is how the helper picks an entry.
	if s.calls[0].Stdin != "registry.example.com" {
		t.Errorf("Stdin = %q, want the server URL", s.calls[0].Stdin)
	}
}

// A helper that returns no secret must read as "no credentials" rather than as
// an empty-password pair, which would send a broken credential to the registry.
func TestGetCredsFromHelperRejectsEmptySecret(t *testing.T) {
	tests := []struct {
		name   string
		output string
	}{
		{"empty secret", `{"Username":"alice","Secret":""}`},
		{"missing secret field", `{"Username":"alice"}`},
		{"not JSON", `credentials not found`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub(t, &stubRunner{output: map[string][]byte{"get": []byte(tt.output)}})

			if _, _, ok := getCredsFromHelper("osxkeychain", "registry.example.com"); ok {
				t.Error("getCredsFromHelper() ok = true, want false")
			}
		})
	}
}

func TestApplyAliases(t *testing.T) {
	aliases := []RegistryAlias{
		{URL: "gitlab.com/my-group", Alias: "gl"},
		{URL: "registry.example.com/", Alias: "ex"},
	}

	tests := []struct {
		name  string
		image string
		want  string
	}{
		{"prefix is replaced", "gitlab.com/my-group/my-image:1.0", "gl/my-image:1.0"},
		{"a trailing slash in the URL is normalised", "registry.example.com/app:2.0", "ex/app:2.0"},
		{"unmatched images pass through", "docker.io/library/nginx:1.25", "docker.io/library/nginx:1.25"},
		{"a partial prefix does not match", "gitlab.com/other-group/img:1.0", "gitlab.com/other-group/img:1.0"},
		{"the first matching alias wins", "gitlab.com/my-group/a/b:1", "gl/a/b:1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ApplyAliases(tt.image, aliases); got != tt.want {
				t.Errorf("ApplyAliases(%q) = %q, want %q", tt.image, got, tt.want)
			}
		})
	}
}

func TestApplyAliasesIgnoresIncompleteEntries(t *testing.T) {
	aliases := []RegistryAlias{
		{URL: "gitlab.com", Alias: ""},
		{URL: "", Alias: "gl"},
	}

	const image = "gitlab.com/group/img:1.0"
	if got := ApplyAliases(image, aliases); got != image {
		t.Errorf("ApplyAliases() = %q, want the name unchanged", got)
	}
}
