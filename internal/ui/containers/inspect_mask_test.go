package containers

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/secretenv"
	viewerpkg "github.com/anthnel/devdesk/internal/viewer"
)

// OC-13 (DEVDESK-811, S9): the inspect hides the values of the sensitive
// variables of Config.Env, and only those. The JSON stays valid, and the
// document keeps the order and the layout the engine gave it.

// inspectFixture is shaped like `docker inspect`: an array, four-space indent,
// Config.Env a list of NAME=VALUE strings. The canary also sits in places the
// story leaves alone (Cmd, Labels, another "Env"), so a mask that spills over
// is caught.
const inspectFixture = `[
    {
        "Id": "0123456789ab",
        "Name": "/api",
        "State": {
            "Env": [
                "STATE_TOKEN=canary"
            ]
        },
        "Config": {
            "Env": [
                "API_TOKEN=canary",
                "LOG_LEVEL=info",
                "PATH=/usr/local/bin:/usr/bin"
            ],
            "Cmd": [
                "serve",
                "--token=canary"
            ],
            "Labels": {
                "Env": "canary",
                "com.example.key": "canary"
            }
        }
    }
]
`

func mustMask(t *testing.T, raw string, m *secretenv.Matcher) (string, int) {
	t.Helper()
	out, n, err := maskInspectEnv([]byte(raw), m)
	if err != nil {
		t.Fatalf("maskInspectEnv: %v", err)
	}
	return string(out), n
}

func TestMaskInspectEnvHidesTheSensitiveValue(t *testing.T) {
	out, n := mustMask(t, inspectFixture, secretenv.Default())

	if n != 1 {
		t.Errorf("masked %d values, want 1 (API_TOKEN)", n)
	}
	if !strings.Contains(out, `"API_TOKEN=`+maskedValue+`"`) {
		t.Errorf("API_TOKEN is not shown as a masked value:\n%s", out)
	}
	if !strings.Contains(out, `"LOG_LEVEL=info"`) {
		t.Errorf("LOG_LEVEL=info was touched:\n%s", out)
	}
	if !strings.Contains(out, `"PATH=/usr/local/bin:/usr/bin"`) {
		t.Errorf("PATH was touched:\n%s", out)
	}
}

// The mask is a fixed-length ASCII string: the length of a secret is a hint,
// and a non-ASCII bullet is what a terminal, a clipboard and a test each
// handle their own way.
func TestTheMaskIsFixedLengthASCII(t *testing.T) {
	if maskedValue != "********" {
		t.Fatalf("maskedValue = %q, want eight asterisks", maskedValue)
	}
	for _, value := range []string{"x", "canary", strings.Repeat("s3cr3t", 40), ""} {
		raw := `[{"Config":{"Env":["API_TOKEN=` + value + `"]}}]`
		out, n := mustMask(t, raw, nil)
		if n != 1 {
			t.Errorf("value %q: masked %d, want 1", value, n)
		}
		if want := `[{"Config":{"Env":["API_TOKEN=` + maskedValue + `"]}}]`; out != want {
			t.Errorf("value %q: got %s, want %s", value, out, want)
		}
	}
}

// Only the span of the value changes. The same text in Cmd, in the labels and
// in an "Env" that is not Config.Env is out of scope (OC-37 and later).
func TestOnlyConfigEnvIsMasked(t *testing.T) {
	out, _ := mustMask(t, inspectFixture, nil)

	want := strings.Replace(inspectFixture, `"API_TOKEN=canary"`, `"API_TOKEN=`+maskedValue+`"`, 1)
	if out != want {
		t.Errorf("the document differs from the engine's beyond the masked value.\n got: %s\nwant: %s", out, want)
	}
	for _, kept := range []string{`"--token=canary"`, `"com.example.key": "canary"`, `"STATE_TOKEN=canary"`} {
		if !strings.Contains(out, kept) {
			t.Errorf("%s was masked, but only Config.Env is in scope", kept)
		}
	}
}

func TestMaskedInspectIsStillValidJSON(t *testing.T) {
	out, _ := mustMask(t, inspectFixture, nil)

	var doc []struct {
		Config struct {
			Env []string
		}
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("the masked inspect is not JSON: %v\n%s", err, out)
	}
	got := strings.Join(doc[0].Config.Env, "|")
	if want := "API_TOKEN=" + maskedValue + "|LOG_LEVEL=info|PATH=/usr/local/bin:/usr/bin"; got != want {
		t.Errorf("Config.Env = %q, want %q", got, want)
	}
}

// KEYBOARD_LAYOUT is a false positive the default patterns accept (OC-12): the
// inspect hides it like any other.
func TestMaskUsesTheSecretenvClassification(t *testing.T) {
	raw := `[{"Config":{"Env":["KEYBOARD_LAYOUT=us","db_password=x","LANG=C"]}}]`

	out, n := mustMask(t, raw, secretenv.Default())
	if n != 2 {
		t.Errorf("masked %d, want 2 (KEYBOARD_LAYOUT, db_password): %s", n, out)
	}
	if !strings.Contains(out, `"LANG=C"`) {
		t.Errorf("LANG=C was touched: %s", out)
	}

	// The configured list adds to the defaults.
	extra, err := secretenv.New([]string{"^LANG$"})
	if err != nil {
		t.Fatal(err)
	}
	out, n = mustMask(t, raw, extra)
	if n != 3 || strings.Contains(out, "LANG=C") {
		t.Errorf("a configured pattern did not apply (masked %d): %s", n, out)
	}
}

func TestMaskWithoutAMatcherUsesTheDefaults(t *testing.T) {
	out, n := mustMask(t, inspectFixture, nil)
	if n != 1 || strings.Contains(out, "API_TOKEN=canary") {
		t.Errorf("a nil matcher did not apply the defaults (masked %d)", n)
	}
}

func TestMaskEdgeCases(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
		n    int
	}{
		{"no Config", `[{"Id":"a"}]`, `[{"Id":"a"}]`, 0},
		{"no Env", `[{"Config":{"Cmd":["x"]}}]`, `[{"Config":{"Cmd":["x"]}}]`, 0},
		{"null Env", `[{"Config":{"Env":null}}]`, `[{"Config":{"Env":null}}]`, 0},
		{"empty Env", `[{"Config":{"Env":[]}}]`, `[{"Config":{"Env":[]}}]`, 0},
		{"empty array", `[]`, `[]`, 0},
		{"a name without =", `[{"Config":{"Env":["API_TOKEN","LOG_LEVEL"]}}]`, `[{"Config":{"Env":["API_TOKEN","LOG_LEVEL"]}}]`, 0},
		{"several =", `[{"Config":{"Env":["API_TOKEN=a=b=c"]}}]`, `[{"Config":{"Env":["API_TOKEN=********"]}}]`, 1},
		{"an empty value is masked like any other", `[{"Config":{"Env":["API_TOKEN="]}}]`, `[{"Config":{"Env":["API_TOKEN=********"]}}]`, 1},
		{"an escaped value", `[{"Config":{"Env":["API_TOKEN=a\"b\\cé"]}}]`, `[{"Config":{"Env":["API_TOKEN=********"]}}]`, 1},
		{"an escaped name", `[{"Config":{"Env":["MY_TOKEN=x"]}}]`, `[{"Config":{"Env":["MY_TOKEN=********"]}}]`, 1},
		{"a non-string entry is left alone", `[{"Config":{"Env":[1,null,"API_TOKEN=x"]}}]`, `[{"Config":{"Env":[1,null,"API_TOKEN=********"]}}]`, 1},
		{"several containers", `[{"Config":{"Env":["A_TOKEN=1"]}},{"Config":{"Env":["B_SECRET=2","C=3"]}}]`,
			`[{"Config":{"Env":["A_TOKEN=********"]}},{"Config":{"Env":["B_SECRET=********","C=3"]}}]`, 2},
		{"Env before other keys and after", `[{"Config":{"Image":"x","Env":["K_SECRET=1"],"User":"u"}}]`,
			`[{"Config":{"Image":"x","Env":["K_SECRET=********"],"User":"u"}}]`, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, n := mustMask(t, tc.raw, nil)
			if n != tc.n {
				t.Errorf("masked %d, want %d", n, tc.n)
			}
			if out != tc.want {
				t.Errorf("got  %s\nwant %s", out, tc.want)
			}
			if !json.Valid([]byte(out)) {
				t.Errorf("not valid JSON: %s", out)
			}
		})
	}
}

// A document that cannot be read as JSON is refused, never passed through: a
// pass-through is the one way the masking could fail open.
func TestMaskRefusesWhatItCannotRead(t *testing.T) {
	for _, raw := range []string{``, `not json`, `[{"Config":{"Env":["API_TOKEN=canary"`, `[{"Config":{"Env":["API_TOKEN=canary"]}`} {
		out, n, err := maskInspectEnv([]byte(raw), nil)
		if err == nil {
			t.Errorf("%q: no error (output %q, %d masked)", raw, out, n)
		}
		if strings.Contains(string(out), "canary") {
			t.Errorf("%q: the refused output still carries the canary: %q", raw, out)
		}
	}
}

func TestInspectSourceIsMaskable(t *testing.T) {
	var src viewerpkg.Source = inspectSource{ID: "0123456789ab", Container: "api"}
	masker, ok := src.(viewerpkg.Maskable)
	if !ok {
		t.Fatal("inspectSource is not Maskable, so `m` never appears for an inspect")
	}
	out, n, err := masker.Mask([]byte(inspectFixture), nil)
	if err != nil || n != 1 || strings.Contains(string(out), "API_TOKEN=canary") {
		t.Errorf("Mask = (%d masked, %v), canary shown: %v", n, err, strings.Contains(string(out), "API_TOKEN=canary"))
	}
}

// The logs are not masked here (OC-37): they must not offer `m`.
func TestLogsSourceIsNotMaskable(t *testing.T) {
	if _, ok := viewerpkg.Source(logsSource{ID: "0123456789ab", Container: "api"}).(viewerpkg.Maskable); ok {
		t.Error("logsSource is Maskable; masking the logs is out of OC-13's scope")
	}
}
