package containers

import (
	"encoding/json"
	"strings"
	"testing"
)

// `podman inspect` carries Config.CreateCommand, the whole argv of the
// `podman run` that made the container, so a value typed with -e is there as
// well as in Config.Env. The mask covers both with the same matcher, or the
// header would say "masked" over a readable secret.

const podmanInspectFixture = `[
    {
        "Id": "0123456789ab",
        "Config": {
            "Env": [
                "API_TOKEN=canary",
                "LOG_LEVEL=info",
                "PATH=/usr/bin"
            ],
            "Cmd": [
                "-e",
                "CMD_TOKEN=canary"
            ],
            "CreateCommand": [
                "podman",
                "run",
                "-d",
                "--name",
                "api",
                "-e",
                "API_TOKEN=canary",
                "-e",
                "LOG_LEVEL=info",
                "--env=DB_PASSWORD=canary2",
                "-eSECRET_KEY=canary3",
                "--env-file",
                "/home/u/API_TOKEN=x.env",
                "-p",
                "8080:80",
                "docker.io/library/nginx:latest"
            ]
        }
    }
]
`

func TestMaskHidesTheValuesOfTheCreateCommand(t *testing.T) {
	out, n := mustMask(t, podmanInspectFixture, nil)

	// Env (1) + -e API_TOKEN + --env=DB_PASSWORD + -eSECRET_KEY.
	if n != 4 {
		t.Errorf("masked %d values, want 4", n)
	}
	want := podmanInspectFixture
	for _, r := range [][2]string{
		{`"API_TOKEN=canary"`, `"API_TOKEN=` + maskedValue + `"`},
		{`"--env=DB_PASSWORD=canary2"`, `"--env=DB_PASSWORD=` + maskedValue + `"`},
		{`"-eSECRET_KEY=canary3"`, `"-eSECRET_KEY=` + maskedValue + `"`},
	} {
		want = strings.ReplaceAll(want, r[0], r[1])
	}
	// "CMD_TOKEN=canary" under Cmd stays: Cmd is out of scope.
	if out != want {
		t.Errorf("the document differs from the expected one beyond the masked values.\n got: %s\nwant: %s", out, want)
	}
	for _, kept := range []string{`"-e",
                "LOG_LEVEL=info"`, `"--env-file"`, `/home/u/API_TOKEN=x.env`, `"docker.io/library/nginx:latest"`, `"CMD_TOKEN=canary"`} {
		if !strings.Contains(out, kept) {
			t.Errorf("%q was touched", kept)
		}
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("not valid JSON: %s", out)
	}
	if strings.Contains(strings.ReplaceAll(out, `"CMD_TOKEN=canary"`, ""), "canary") {
		t.Errorf("a canary is still readable:\n%s", out)
	}
}

func TestCreateCommandEdgeCases(t *testing.T) {
	cases := []struct {
		name string
		args string // the elements of CreateCommand
		want string
		n    int
	}{
		{"-e at the end", `"run","-e"`, `"run","-e"`, 0},
		{"a value with several =", `"-e","API_TOKEN=a=b=c"`, `"-e","API_TOKEN=********"`, 1},
		{"--env with its value apart", `"--env","API_TOKEN=x"`, `"--env","API_TOKEN=********"`, 1},
		{"a name with no =", `"-e","API_TOKEN"`, `"-e","API_TOKEN"`, 0},
		{"a name with no = glued", `"--env=API_TOKEN","-eAPI_TOKEN"`, `"--env=API_TOKEN","-eAPI_TOKEN"`, 0},
		{"--env-file is a path, not a value", `"--env-file","API_TOKEN=x"`, `"--env-file","API_TOKEN=x"`, 0},
		{"--env-file glued", `"--env-file=/tmp/TOKEN=x"`, `"--env-file=/tmp/TOKEN=x"`, 0},
		{"a non-sensitive name", `"-e","LOG_LEVEL=info","--env=LANG=C"`, `"-e","LOG_LEVEL=info","--env=LANG=C"`, 0},
		{"an element that only looks like the value", `"--name","API_TOKEN=x"`, `"--name","API_TOKEN=x"`, 0},
		{"an escaped value", `"-e","API_TOKEN=a\"b\\cé"`, `"-e","API_TOKEN=********"`, 1},
		{"a non-string after -e", `"-e",null,"API_TOKEN=x"`, `"-e",null,"API_TOKEN=x"`, 0},
		{"an empty value", `"-e","API_TOKEN="`, `"-e","API_TOKEN=********"`, 1},
		{"the flag does not leak to the next array", `"-e"],"Cmd":["API_TOKEN=x"`, `"-e"],"Cmd":["API_TOKEN=x"`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := `[{"Config":{"CreateCommand":[` + tc.args + `]}}]`
			want := `[{"Config":{"CreateCommand":[` + tc.want + `]}}]`
			out, n := mustMask(t, raw, nil)
			if n != tc.n || out != want {
				t.Errorf("masked %d, want %d\n got  %s\nwant %s", n, tc.n, out, want)
			}
			if !json.Valid([]byte(out)) {
				t.Errorf("not valid JSON: %s", out)
			}
		})
	}
}

// A name or a value written with JSON escapes is read for what it says. The
// rewritten entry carries the characters themselves, which is the one place the
// document is not kept byte for byte.
func TestEscapedEntriesAreRewrittenAsLiteralUTF8(t *testing.T) {
	cases := []struct{ name, raw, want string }{
		{"a unicode escape in the name", `"TOKEN_é=x"`, `"TOKEN_é=********"`},
		{"a surrogate pair in the name", `"KEY_😀=x"`, `"KEY_😀=********"`},
		{"an escaped quote in the name", `"A\"TOKEN=x"`, `"A\"TOKEN=********"`},
		{"an escaped letter in the keyword", `"TOKEN=x"`, `"TOKEN=********"`},
		{"a surrogate pair in the value", `"API_TOKEN=😀"`, `"API_TOKEN=********"`},
	}
	for _, tc := range cases {
		for _, where := range []string{"Env", "CreateCommand"} {
			t.Run(tc.name+"/"+where, func(t *testing.T) {
				doc := func(entry string) string { return `[{"Config":{"` + where + `":[` + entry + `]}}]` }
				if where == "CreateCommand" {
					doc = func(entry string) string { return `[{"Config":{"CreateCommand":["-e",` + entry + `]}}]` }
				}
				out, n := mustMask(t, doc(tc.raw), nil)
				if n != 1 || out != doc(tc.want) {
					t.Errorf("masked %d\n got  %s\nwant %s", n, out, doc(tc.want))
				}
			})
		}
	}
}
