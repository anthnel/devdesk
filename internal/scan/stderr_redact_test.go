package scan

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// DEVDESK-583, AC2: a proxy value that holds credentials must not come out of a
// tool's stderr into the progress line (footer, job detail) or into the exitError
// text. The log gets that text through recordStageError (scanner.go), which only
// formats the already redacted error. The tool is the test binary acting as a
// scanner, so what it writes on stderr is fully controlled.

const (
	secretPassword = "s3cr3tpw"
	secretUserInfo = "user:s3cr3tpw"
	secretProxy    = "http://user:s3cr3tpw@proxy.corp:3128"
	// The same password with a character that Go escapes in a URL userinfo.
	escapedPassword = "s3cr3t%40pw"
	escapedProxy    = "http://user:s3cr3t%40pw@proxy.corp:3128"
)

// leaks lists what must not appear in any text the runner produces.
func leaks(text string, forbidden ...string) []string {
	var found []string
	for _, f := range forbidden {
		if strings.Contains(text, f) {
			found = append(found, f)
		}
	}
	return found
}

func TestStderrRedactsProxyCredentials(t *testing.T) {
	cases := []struct {
		name      string
		proxy     string
		stderr    string
		forbidden []string
		// kept is the useful part of the message, which must survive.
		kept []string
	}{
		{
			name:      "plain value",
			proxy:     secretProxy,
			stderr:    "proxyconnect tcp: dial " + secretProxy + " refused",
			forbidden: []string{secretPassword, secretUserInfo, secretProxy},
			kept:      []string{"proxyconnect tcp: dial", "proxy.corp:3128", "refused"},
		},
		{
			name:      "value spread over several lines",
			proxy:     secretProxy,
			stderr:    "FATAL image pull failed\nvia " + secretProxy + "\ntry again later",
			forbidden: []string{secretPassword, secretUserInfo, secretProxy},
			kept:      []string{"FATAL image pull failed", "proxy.corp:3128", "try again later"},
		},
		{
			name:      "escaped value, written as in the variable",
			proxy:     escapedProxy,
			stderr:    "proxyconnect: " + escapedProxy + " refused",
			forbidden: []string{escapedPassword, "user:" + escapedPassword},
			kept:      []string{"proxyconnect:", "proxy.corp:3128", "refused"},
		},
		{
			name:      "escaped value, written decoded by the tool",
			proxy:     escapedProxy,
			stderr:    "proxyconnect: http://user:s3cr3t@pw@proxy.corp:3128 refused",
			forbidden: []string{"s3cr3t@pw", escapedPassword},
			kept:      []string{"proxyconnect:", "refused"},
		},
		{
			name:      "password alone, without the URL around it",
			proxy:     secretProxy,
			stderr:    "407 Proxy Authentication Required (password " + secretPassword + ")",
			forbidden: []string{secretPassword},
			kept:      []string{"407 Proxy Authentication Required"},
		},
	}

	for _, tc := range cases {
		for _, withProgress := range []bool{true, false} {
			name := tc.name + "/no progress callback"
			if withProgress {
				name = tc.name + "/progress callback"
			}
			t.Run(name, func(t *testing.T) {
				useProxy(t, forwardOf(map[string]string{"HTTPS_PROXY": tc.proxy}))
				cmd := asScanner(t, `{"Results":[]}`, tc.stderr, 3)

				var progress []string
				var fn func(string)
				if withProgress {
					fn = func(line string) { progress = append(progress, line) }
				}
				_, err := production.Run(context.Background(), cmd, fn)

				var exit *exitError
				if !errors.As(err, &exit) {
					t.Fatalf("err = %v, want an *exitError", err)
				}
				if exit.Code != 3 {
					t.Errorf("exit code = %d, want 3", exit.Code)
				}

				// (a) the error, as a field and as the text a caller shows.
				if got := leaks(exit.Stderr, tc.forbidden...); len(got) > 0 {
					t.Errorf("exitError.Stderr leaks %q: %q", got, exit.Stderr)
				}
				if got := leaks(err.Error(), tc.forbidden...); len(got) > 0 {
					t.Errorf("the error message leaks %q: %q", got, err.Error())
				}
				// (b) the progress lines.
				if withProgress {
					if got := leaks(strings.Join(progress, "\n"), tc.forbidden...); len(got) > 0 {
						t.Errorf("the progress lines leak %q: %q", got, progress)
					}
				}

				// The diagnostic is still a diagnostic: only the secret goes.
				for _, k := range tc.kept {
					if !strings.Contains(err.Error(), k) {
						t.Errorf("the useful part %q was lost from %q", k, err.Error())
					}
					if withProgress && !strings.Contains(strings.Join(progress, "\n"), k) {
						t.Errorf("the useful part %q was lost from the progress %q", k, progress)
					}
				}
			})
		}
	}
}

// Redacting must not swallow lines: one line in, one line out.
func TestStderrRedactionKeepsEveryProgressLine(t *testing.T) {
	useProxy(t, forwardOf(map[string]string{"HTTPS_PROXY": secretProxy}))
	cmd := asScanner(t, "", "first\nvia "+secretProxy+"\nthird", 0)

	var progress []string
	if _, err := production.Run(context.Background(), cmd, func(line string) {
		progress = append(progress, line)
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(progress) != 3 || progress[0] != "first" || progress[2] != "third" {
		t.Errorf("progress = %q, want three lines with the outer two untouched", progress)
	}
	if got := leaks(strings.Join(progress, "\n"), secretPassword, secretUserInfo); len(got) > 0 {
		t.Errorf("the progress lines leak %q: %q", got, progress)
	}
}

// A tool whose output os/exec captured itself (the tool probes of Detect, toolbox.go,
// with Output()) hands its diagnostic back on the ExitError, not through Run.
func TestWaitErrRedactsTheStderrOfTheExitError(t *testing.T) {
	useProxy(t, forwardOf(map[string]string{"HTTPS_PROXY": secretProxy}))
	cmd := asScanner(t, "", "proxyconnect tcp: dial "+secretProxy+" refused", 2)

	_, err := exec.Command(cmd.Name).Output() //nolint:gosec // the test binary, acting as a scanner
	got := waitErr("trivy", err, "")

	var exit *exitError
	if !errors.As(got, &exit) {
		t.Fatalf("err = %v, want an *exitError", got)
	}
	if l := leaks(exit.Stderr+got.Error(), secretPassword, secretUserInfo, secretProxy); len(l) > 0 {
		t.Errorf("the error leaks %q: %q", l, got.Error())
	}
	if !strings.Contains(exit.Stderr, "refused") || !strings.Contains(exit.Stderr, "proxy.corp:3128") {
		t.Errorf("the useful part was lost: %q", exit.Stderr)
	}
}

// AC4: with no proxy variable, nothing is rewritten, whatever the text looks
// like.
func TestStderrIsUnchangedWithAnEmptyForward(t *testing.T) {
	useProxy(t, forwardOf(nil))
	line := "proxyconnect tcp: dial " + secretProxy + " refused"
	cmd := asScanner(t, "", line+"\nplain line", 4)

	var progress []string
	_, err := production.Run(context.Background(), cmd, func(l string) { progress = append(progress, l) })

	var exit *exitError
	if !errors.As(err, &exit) {
		t.Fatalf("err = %v, want an *exitError", err)
	}
	if want := line + "\nplain line\n"; exit.Stderr != want {
		t.Errorf("exitError.Stderr = %q, want %q", exit.Stderr, want)
	}
	if len(progress) != 2 || progress[0] != line || progress[1] != "plain line" {
		t.Errorf("progress = %q, want the lines as written", progress)
	}
}

// A Forward that holds a proxy must not rewrite text that does not mention it.
func TestStderrWithoutTheSecretIsUnchanged(t *testing.T) {
	useProxy(t, forwardOf(map[string]string{"HTTPS_PROXY": secretProxy}))
	cmd := asScanner(t, "", "downloading db\n25%\nFATAL unable to initialize scanner", 2)

	var progress []string
	_, err := production.Run(context.Background(), cmd, func(l string) { progress = append(progress, l) })

	var exit *exitError
	if !errors.As(err, &exit) {
		t.Fatalf("err = %v, want an *exitError", err)
	}
	if want := "downloading db\n25%\nFATAL unable to initialize scanner\n"; exit.Stderr != want {
		t.Errorf("exitError.Stderr = %q, want %q", exit.Stderr, want)
	}
	if strings.Join(progress, "|") != "downloading db|25%|FATAL unable to initialize scanner" {
		t.Errorf("progress = %q, want the lines as written", progress)
	}
}
