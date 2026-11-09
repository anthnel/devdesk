package jobs

import (
	"errors"
	"strings"
	"testing"
)

func TestSafeCause(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		exact    bool     // the output must equal want
		want     string   // expected output when exact
		contains []string // substrings that must be present
		absent   []string // substrings that must be gone
	}{
		{name: "nil error", err: nil, exact: true, want: ""},
		{name: "plain text unchanged", err: errors.New("Repository not found."), exact: true, want: "Repository not found."},
		{name: "text with a colon and no URL unchanged", err: errors.New("fatal: could not read Username: terminal prompts disabled"),
			exact: true, want: "fatal: could not read Username: terminal prompts disabled"},
		{
			name:     "user and password",
			err:      errors.New("fatal: unable to access 'https://oauth2:glpat-abc@gitlab.example/g/app.git/': 403"),
			contains: []string{"https://gitlab.example/g/app.git/", "403"},
			absent:   []string{"glpat-abc", "oauth2:", "@gitlab"},
		},
		{name: "user only", err: errors.New("https://user@host/x"), exact: true, want: "https://host/x"},
		{name: "ssh scheme", err: errors.New("ssh://git@host:22/x"), exact: true, want: "ssh://host:22/x"},
		{name: "scp form has no secret", err: errors.New("git@github.com:o/r.git"), exact: true, want: "git@github.com:o/r.git"},
		{name: "raw @ in the password", err: errors.New("fatal: https://user:p@ss@w0rd@host/x.git failed"),
			exact: true, want: "fatal: https://host/x.git failed"},
		{name: "git scheme", err: errors.New("git://u:pw@host/x.git"), exact: true, want: "git://host/x.git"},
		{name: "ftp scheme", err: errors.New("ftp://u:pw@host/x"), exact: true, want: "ftp://host/x"},
		{name: "git+ssh scheme", err: errors.New("git+ssh://u:pw@host/x"), exact: true, want: "git+ssh://host/x"},
		{name: "URL without userinfo unchanged", err: errors.New("GET https://host/a@b: 404"), exact: true, want: "GET https://host/a@b: 404"},
		{name: "oauth_token keeps the fragment", err: errors.New("GET https://h/x?oauth_token=abc&page=2#frag"),
			exact: true, want: "GET https://h/x?oauth_token=&page=2#frag"},
		{name: "token before a fragment", err: errors.New("GET https://h/x?token=abc#frag"),
			exact: true, want: "GET https://h/x?token=#frag"},
		{name: "family of names", err: errors.New("https://h/x?job_token=a1&refresh_token=a2&api_key=a3&client_secret=a4&password=a5&passwd=a6&secret=a7&page=2"),
			exact: true, want: "https://h/x?job_token=&refresh_token=&api_key=&client_secret=&password=&passwd=&secret=&page=2"},
		{name: "name case-insensitive", err: errors.New("https://h/x?API_KEY=abc&page=2"), exact: true, want: "https://h/x?API_KEY=&page=2"},
		{name: "Authorization Bearer", err: errors.New("sent Authorization: Bearer abc.def failed"), exact: true, want: "sent Authorization: Bearer  failed"},
		{name: "authorization basic lowercase", err: errors.New("authorization: basic dXNlcjpwdw== failed"), exact: true, want: "authorization: basic  failed"},
		{name: "Authorization Token", err: errors.New("Authorization: Token abc failed"), exact: true, want: "Authorization: Token  failed"},
		{name: "PRIVATE-TOKEN header", err: errors.New("private-token: glpat-abc failed"), exact: true, want: "private-token:  failed"},
		{
			name:     "private_token query value",
			err:      errors.New("GET https://h/api/v4/groups?private_token=abc&page=2: 401"),
			contains: []string{"page=2", "401"},
			absent:   []string{"abc"},
		},
		{
			name:     "token query value",
			err:      errors.New("GET https://h/x?token=abc123&page=2: 401"),
			contains: []string{"page=2"},
			absent:   []string{"abc123"},
		},
		{
			name:     "access_token query value",
			err:      errors.New("GET https://h/x?page=2&access_token=abc123: 401"),
			contains: []string{"page=2"},
			absent:   []string{"abc123"},
		},
		{
			name:     "query key is case-insensitive",
			err:      errors.New("GET https://h/x?PRIVATE_TOKEN=abc123&page=2: 401"),
			contains: []string{"page=2"},
			absent:   []string{"abc123"},
		},
		{
			name:     "several URLs in one message",
			err:      errors.New("clone https://a:s1@one.example/x.git then https://b:s2@two.example/y.git?token=s3 failed"),
			contains: []string{"one.example/x.git", "two.example/y.git", "failed"},
			absent:   []string{"s1", "s2", "s3", "a:", "b:"},
		},
		{
			name:     "wrapped error",
			err:      errors.Join(errors.New("walk"), errors.New("https://u:pw@h/x")),
			contains: []string{"walk", "https://h/x"},
			absent:   []string{"pw"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SafeCause(tt.err)
			if tt.exact {
				if got != tt.want {
					t.Fatalf("SafeCause = %q, want %q", got, tt.want)
				}
			}
			for _, s := range tt.contains {
				if !strings.Contains(got, s) {
					t.Errorf("SafeCause = %q, want it to contain %q", got, s)
				}
			}
			for _, s := range tt.absent {
				if strings.Contains(got, s) {
					t.Errorf("SafeCause = %q, must not contain %q", got, s)
				}
			}
		})
	}
}
