package enginefixture_test

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-09a3 (DEVDESK-944), step 11: enginefixture.Redact, what the recorder runs in
// the test VM on every exchange before anything is written. It turns what a real
// engine answered into what a fixture may hold: the ids it made become fixed valid
// ids, the home of the recording user, the host name, the user name and the id of
// the run become the placeholders CheckLeaks accepts.
//
// The API these tests pin down (package enginefixture; no "testing", starts nothing):
//
//	const PlaceholderRun = "fixture-run"            // PlaceholderUser, PlaceholderHost, PlaceholderHome exist already
//	const RecordedByContract = "engine-contract:record"
//	type Redaction struct {
//		RunID            string            // the run id of contract.sh, "" = none
//		IDs              map[string]string // real 64-hex id -> role ("c1")
//		Home, User, Host string            // of the recording user and machine, "" = none
//	}
//	func PlaceholderID(role string) string               // hex(sha256("devdesk-fixture/" + role)): 64 hex, IsContainerID holds
//	func Redact(ex Exchange, r Redaction) (Exchange, string) // the string is the "redacted" header
//
// Order and rules (plan OC-09a, 3.7): full ids, then 12-character prefixes at the
// boundary of a hexadecimal word, then the home (a path prefix), the run id, the
// host (strict boundaries, as lab-identity), the user (at the word). It works on
// bytes, argv, stdout and stderr alike, a stream that is not UTF-8 included. The
// header lists the categories that actually replaced something, sorted, "home,
// host, ids, run, user", or "none". Decisions of this step, beyond the plan:
//
//   - the host and the user are replaced only when enginefixture.LocalIdentityTerms
//     keeps them (3 characters at least, not in its stop list): "root" is the user
//     of every rootful engine and stays; "dd", the lab user, stays too and is left
//     to CheckLeaks (lab-identity) to refuse, which is the net that makes a
//     recording fail rather than leak;
//   - a line start, a line end and the start and end of a stream are word
//     boundaries, as the leak check reads line by line.

const (
	rdRun  = "20261127-a1b2c3d4"
	rdHome = "/home/dd"
)

var (
	rdID1 = sha256Hex("real container 1")
	rdID2 = sha256Hex("real container 2")
)

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// wantPlaceholder is PlaceholderID spelled out, so the tests hold its definition.
func wantPlaceholder(role string) string { return sha256Hex("devdesk-fixture/" + role) }

// inAllThree puts text in the argv, the stdout and the stderr of one exchange.
func inAllThree(text string) enginefixture.Exchange {
	return enginefixture.Exchange{Argv: []string{"inspect", text}, Exit: 3, Stdout: []byte(text), Stderr: []byte(text)}
}

// checkAllThree compares the redacted exchange to what each of the three places
// must now hold, and the exit code to what it was.
func checkAllThree(t *testing.T, got enginefixture.Exchange, want string) {
	t.Helper()
	if len(got.Argv) != 2 || got.Argv[0] != "inspect" || got.Argv[1] != want {
		t.Errorf("argv = %q, want [inspect %q]", got.Argv, want)
	}
	if string(got.Stdout) != want {
		t.Errorf("stdout = %q, want %q", got.Stdout, want)
	}
	if string(got.Stderr) != want {
		t.Errorf("stderr = %q, want %q", got.Stderr, want)
	}
	if got.Exit != 3 {
		t.Errorf("exit = %d, want 3: Redact does not touch the exit code", got.Exit)
	}
}

func TestPlaceholderConstants(t *testing.T) {
	if enginefixture.PlaceholderRun != "fixture-run" {
		t.Errorf("PlaceholderRun = %q, want fixture-run", enginefixture.PlaceholderRun)
	}
	if enginefixture.RecordedByContract != "engine-contract:record" {
		t.Errorf("RecordedByContract = %q, want engine-contract:record", enginefixture.RecordedByContract)
	}
}

func TestPlaceholderIDIsStableAndAValidContainerID(t *testing.T) {
	hexID := regexp.MustCompile(`^[0-9a-f]{64}$`)
	seen := map[string]string{}
	for _, role := range []string{"c1", "c2", "container-1", "n1", "v1"} {
		got := enginefixture.PlaceholderID(role)
		if got != wantPlaceholder(role) {
			t.Errorf("PlaceholderID(%q) = %s, want hex(sha256(\"devdesk-fixture/%s\")) = %s", role, got, role, wantPlaceholder(role))
		}
		if !hexID.MatchString(got) {
			t.Errorf("PlaceholderID(%q) = %q, want 64 lower-case hexadecimal characters", role, got)
		}
		if got != enginefixture.PlaceholderID(role) {
			t.Errorf("PlaceholderID(%q) changes between two calls", role)
		}
		if other, dup := seen[got]; dup {
			t.Errorf("PlaceholderID(%q) = PlaceholderID(%q)", role, other)
		}
		seen[got] = role
	}
}

func TestRedactReplacesWhatItKnowsEverywhere(t *testing.T) {
	ids := map[string]string{rdID1: "c1", rdID2: "c2"}
	ph1, ph2 := wantPlaceholder("c1"), wantPlaceholder("c2")
	tests := []struct {
		name       string
		r          enginefixture.Redaction
		in, want   string
		wantHeader string
	}{
		// Ids.
		{"a full id", enginefixture.Redaction{IDs: ids}, "id " + rdID1 + " end", "id " + ph1 + " end", "ids"},
		{"two ids get two placeholders", enginefixture.Redaction{IDs: ids}, rdID1 + "," + rdID2, ph1 + "," + ph2, "ids"},
		{"the id twice", enginefixture.Redaction{IDs: ids}, rdID1 + " " + rdID1, ph1 + " " + ph1, "ids"},
		{"the 12-character prefix, as ps prints it", enginefixture.Redaction{IDs: ids},
			"CONTAINER ID  " + rdID1[:12] + "  IMAGE", "CONTAINER ID  " + ph1[:12] + "  IMAGE", "ids"},
		{"the prefix between quotes", enginefixture.Redaction{IDs: ids}, `"` + rdID1[:12] + `"`, `"` + ph1[:12] + `"`, "ids"},
		{"the prefix at the start and the end of the stream", enginefixture.Redaction{IDs: ids}, rdID1[:12], ph1[:12], "ids"},
		{"the prefix inside a longer hexadecimal word is left alone", enginefixture.Redaction{IDs: ids},
			rdID1[:12] + "ff", rdID1[:12] + "ff", "none"},
		{"the prefix after a hexadecimal character is left alone", enginefixture.Redaction{IDs: ids},
			"0" + rdID1[:12], "0" + rdID1[:12], "none"},
		{"an id that is not in the map is left alone", enginefixture.Redaction{IDs: ids}, sha256Hex("stranger"), sha256Hex("stranger"), "none"},
		{"no ids at all", enginefixture.Redaction{}, rdID1, rdID1, "none"},

		// Home.
		{"the home of the recording user", enginefixture.Redaction{Home: rdHome},
			rdHome + "/.local/share/containers", "/home/fixture-user/.local/share/containers", "home"},
		{"the home of root, which personal-path refuses", enginefixture.Redaction{Home: "/root"}, "/root/x", "/home/fixture-user/x", "home"},
		{"the home as an environment value", enginefixture.Redaction{Home: "/root"}, "HOME=/root", "HOME=/home/fixture-user", "home"},
		{"the home alone on its line", enginefixture.Redaction{Home: rdHome}, "path " + rdHome + "\nnext", "path /home/fixture-user\nnext", "home"},
		{"a path that only starts like the home", enginefixture.Redaction{Home: "/root"}, "/rootfs/x", "/rootfs/x", "none"},
		{"a home that is the start of another user's", enginefixture.Redaction{Home: rdHome}, "/home/ddx/y", "/home/ddx/y", "none"},
		{"a home in the middle of a longer path is not a home", enginefixture.Redaction{Home: "/root"}, "/var/lib/root/x", "/var/lib/root/x", "none"},

		// Run id.
		{"the run id in a resource name", enginefixture.Redaction{RunID: rdRun}, "ddc-" + rdRun + "-c1", "ddc-fixture-run-c1", "run"},
		{"the run id in the label", enginefixture.Redaction{RunID: rdRun}, "devdesk.test=" + rdRun, "devdesk.test=fixture-run", "run"},
		{"the run id in a JSON label", enginefixture.Redaction{RunID: rdRun}, `{"devdesk.test":"` + rdRun + `"}`, `{"devdesk.test":"fixture-run"}`, "run"},
		{"no run id given", enginefixture.Redaction{}, "ddc-" + rdRun + "-c1", "ddc-" + rdRun + "-c1", "none"},

		// Host.
		{"the host in quotes", enginefixture.Redaction{Host: "dd-podman"}, `host "dd-podman" up`, `host "fixture-host" up`, "host"},
		{"the host at the end of a line", enginefixture.Redaction{Host: "dd-podman"}, "Hostname: dd-podman\nnext", "Hostname: fixture-host\nnext", "host"},
		{"the host as a path element", enginefixture.Redaction{Host: "dd-podman"}, "/srv/dd-podman/x", "/srv/fixture-host/x", "host"},
		{"a name that only contains the host", enginefixture.Redaction{Host: "dd-podman"}, "xdd-podmanx", "xdd-podmanx", "none"},
		{"a host that is a stop-list name is not an identity", enginefixture.Redaction{Host: "localhost"}, "localhost", "localhost", "none"},

		// User.
		{"the user at the word", enginefixture.Redaction{User: "alice"}, "uid=1000(alice) gid=1000(alice)", "uid=1000(fixture-user) gid=1000(fixture-user)", "user"},
		{"the whole stream is the user", enginefixture.Redaction{User: "alice"}, "alice", "fixture-user", "user"},
		{"a word that contains the user", enginefixture.Redaction{User: "alice"}, "malice alicexyz", "malice alicexyz", "none"},
		{"a user of two characters is not replaced (dd, the lab user: CheckLeaks refuses it)", enginefixture.Redaction{User: "dd"}, "user dd", "user dd", "none"},
		{"root is on the stop list and stays", enginefixture.Redaction{User: "root"}, `"User":"root"`, `"User":"root"`, "none"},

		// Several at once.
		{"the home then the user of the same name", enginefixture.Redaction{Home: "/home/alice", User: "alice"},
			"/home/alice/x alice", "/home/fixture-user/x fixture-user", "home, user"},
		{"everything, in the order of the header", enginefixture.Redaction{RunID: rdRun, IDs: ids, Home: rdHome, User: "alice", Host: "dd-podman"},
			rdID1 + " " + rdHome + "/x dd-podman ddc-" + rdRun + "-c1 alice",
			ph1 + " /home/fixture-user/x fixture-host ddc-fixture-run-c1 fixture-user", "home, host, ids, run, user"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := inAllThree(tt.in)

			got, header := enginefixture.Redact(in, tt.r)

			checkAllThree(t, got, tt.want)
			if header != tt.wantHeader {
				t.Errorf("redacted header = %q, want %q", header, tt.wantHeader)
			}
		})
	}
}

// A stream that is not UTF-8 keeps its other bytes: Redact works on bytes.
func TestRedactKeepsTheBytesAroundAReplacementInANonUTF8Stream(t *testing.T) {
	in := enginefixture.Exchange{Argv: []string{"ps"}, Stdout: []byte("\xff\xfe" + rdHome + "/x\x80\n"), Stderr: []byte("\x00\x01")}

	got, header := enginefixture.Redact(in, enginefixture.Redaction{Home: rdHome})

	if want := "\xff\xfe/home/fixture-user/x\x80\n"; string(got.Stdout) != want {
		t.Errorf("stdout = %q, want %q", got.Stdout, want)
	}
	if string(got.Stderr) != "\x00\x01" {
		t.Errorf("stderr = %q, want it untouched", got.Stderr)
	}
	if header != "home" {
		t.Errorf("header = %q, want home", header)
	}
}

func TestRedactChangesNothingWhenThereIsNothingToChange(t *testing.T) {
	in := enginefixture.Exchange{Argv: []string{"version", "--format", "{{.Client.Version}}"}, Exit: 0, Stdout: []byte("6.2.1\n")}

	got, header := enginefixture.Redact(in, enginefixture.Redaction{
		RunID: rdRun, IDs: map[string]string{rdID1: "c1"}, Home: rdHome, User: "alice", Host: "dd-podman",
	})

	if !sameExchange(got, in) {
		t.Errorf("Redact = %+v, want the exchange unchanged", got)
	}
	if header != "none" {
		t.Errorf("header = %q, want none", header)
	}
}

// Redact returns a new exchange: the input, which the contract keeps using, is
// not edited, and the argv returned is not the input's slice.
func TestRedactDoesNotModifyItsInput(t *testing.T) {
	in := enginefixture.Exchange{
		Argv: []string{"inspect", rdID1}, Exit: 1,
		Stdout: []byte(rdID1 + " " + rdHome), Stderr: []byte(rdID1),
	}
	argv, stdout, stderr := slices.Clone(in.Argv), slices.Clone(in.Stdout), slices.Clone(in.Stderr)

	got, _ := enginefixture.Redact(in, enginefixture.Redaction{IDs: map[string]string{rdID1: "c1"}, Home: rdHome})
	got.Argv[0] = "changed"
	if len(got.Stdout) > 0 {
		got.Stdout[0] = '!'
	}

	if !slices.Equal(in.Argv, argv) || string(in.Stdout) != string(stdout) || string(in.Stderr) != string(stderr) {
		t.Errorf("the input was modified: %+v", in)
	}
}

// Redact(Redact(x)) == Redact(x): the placeholders are not themselves redacted.
func TestRedactIsIdempotent(t *testing.T) {
	r := enginefixture.Redaction{
		RunID: rdRun, IDs: map[string]string{rdID1: "c1", rdID2: "c2"}, Home: rdHome, User: "alice", Host: "dd-podman",
	}
	in := enginefixture.Exchange{
		Argv:   []string{"inspect", rdID1, "ddc-" + rdRun + "-c1"},
		Exit:   2,
		Stdout: []byte(rdID1[:12] + " " + rdID2 + " " + rdHome + "/x dd-podman alice"),
		Stderr: []byte("Error: " + rdID1 + " devdesk.test=" + rdRun),
	}

	once, header := enginefixture.Redact(in, r)
	twice, again := enginefixture.Redact(once, r)

	if !sameExchange(once, twice) {
		t.Errorf("Redact(Redact(x)) = %+v, want Redact(x) = %+v", twice, once)
	}
	if header == "none" || again != "none" {
		t.Errorf("headers = %q then %q, want the first to list what was replaced and the second to say none", header, again)
	}
}

// ---- Redact, then the leak check: the net under the whole recorder. ----

// rawExchange is an exchange as a real engine answered it on the machine of the
// user "alice", host "alicebox", in the run rdRun.
type rawExchange struct {
	name  string
	ex    enginefixture.Exchange
	home  string // the home of the recording user in this exchange
	leaky bool   // the raw text, unredacted, is a leak (ps and a refusal carry only ids and the run id)
}

func rawExchanges() []rawExchange {
	p := rdRun
	return []rawExchange{
		{"inspect of a rootless podman container", enginefixture.Exchange{
			Argv: []string{"inspect", rdID1},
			Stdout: []byte(`[{"Id":"` + rdID1 + `","Name":"ddc-` + p + `-c1","Config":{"Hostname":"` + rdID1[:12] +
				`","Labels":{"devdesk.test":"` + p + `"},"Env":["HOME=/home/alice","PATH=/usr/bin"]},` +
				`"GraphDriver":{"Data":{"UpperDir":"/home/alice/.local/share/containers/storage/overlay/` + rdID1 + `/diff"}},"Host":"alicebox"}]` + "\n"),
		}, "/home/alice", true},
		{"ps of the run", enginefixture.Exchange{
			Argv: []string{"ps", "-a"},
			Stdout: []byte("CONTAINER ID  IMAGE  COMMAND  CREATED  STATUS  PORTS  NAMES\n" +
				rdID1[:12] + "  docker.io/library/alpine:latest  sleep 600  2 seconds ago  Up 2 seconds  ddc-" + p + "-c1\n"),
		}, "/home/alice", false},
		{"a refused removal on stderr", enginefixture.Exchange{
			Argv: []string{"inspect", rdID1}, Exit: 2,
			Stderr: []byte("Error: cannot remove container " + rdID1 + " as it is running - running or paused containers cannot be removed without force: container state improper\n"),
		}, "/home/alice", false},
		{"info with the host name and the paths of the user", enginefixture.Exchange{
			Argv: []string{"info"},
			Stdout: []byte("host:\n  hostname: alicebox\n  rootlessNetworkCmd: pasta\nstore:\n  graphRoot: /home/alice/.local/share/containers/storage\n" +
				"  runRoot: /run/user/1000/containers\n"),
		}, "/home/alice", true},
		{"a binary stream that holds the home", enginefixture.Exchange{
			Argv: []string{"logs", rdID2}, Stdout: []byte("\xff\xfe /home/alice/x\n\x00"),
		}, "/home/alice", true},
		{"a rootful engine: root's home", enginefixture.Exchange{
			Argv:   []string{"inspect", rdID2},
			Stdout: []byte(`[{"Id":"` + rdID2 + `","Config":{"User":"root","Env":["HOME=/root"]},"Path":"/root/.local/share/containers"}]` + "\n"),
		}, "/root", true},
	}
}

func contractMeta(redacted string) enginefixture.Meta {
	return enginefixture.Meta{
		Tool: enginefixture.Podman, Version: "6.2.1", Mode: enginefixture.Rootless, Recorded: "2026-11-27",
		Scenario: "a scenario", Redacted: redacted, RecordedBy: enginefixture.RecordedByContract,
	}
}

// "Redact then CheckLeaksAs is clean": what the recorder writes passes the leak
// check with the identities of the recording machine, and without the redaction
// the same text is a leak. That is the net: the recorder runs this very check
// before it writes.
func TestRedactedExchangesPassTheLeakCheckWithTheIdentitiesOfTheMachine(t *testing.T) {
	local := enginefixture.LocalIdentityTerms("alice", "alicebox")
	if len(local) != 2 {
		t.Fatalf("LocalIdentityTerms = %v, want both identities kept", local)
	}
	for _, raw := range rawExchanges() {
		t.Run(raw.name, func(t *testing.T) {
			r := enginefixture.Redaction{
				RunID: rdRun, IDs: map[string]string{rdID1: "c1", rdID2: "c2"},
				Home: raw.home, User: "alice", Host: "alicebox",
			}
			redacted, header := enginefixture.Redact(raw.ex, r)

			// The fixture as the recorder writes it: formatted, then checked.
			f := enginefixture.Fixture{Meta: contractMeta(header), Exchange: redacted}
			clean := enginefixture.Format(f)
			for _, l := range enginefixture.CheckLeaksAs("clean", clean, nil, local) {
				t.Errorf("the redacted exchange leaks: line %d, rule %q", l.Line, l.Rule)
			}
			if _, err := enginefixture.Parse("containers", "a--podman-6.2.1-rootless.cli", clean); err != nil {
				t.Errorf("the redacted exchange is not a valid fixture: %v", err)
			}

			// The same exchange, not redacted, is what the net is there for.
			f.Exchange, f.Meta.Redacted = raw.ex, "none"
			if raw.leaky && len(enginefixture.CheckLeaksAs("raw", enginefixture.Format(f), nil, local)) == 0 {
				t.Errorf("the raw exchange passes the leak check: the sample proves nothing")
			}
		})
	}
}

// A user that Redact leaves in the text because it is too short to be told apart
// (dd, the lab user) is caught by the leak check: the recording fails instead of
// leaking. That is wanted (plan 3.7).
func TestAShortUserLeftInTheTextIsAnInvariantViolationTheLeakCheckSees(t *testing.T) {
	in := enginefixture.Exchange{Argv: []string{"info"}, Stdout: []byte("uid=1000(dd) gid=1000(dd)\n")}

	redacted, header := enginefixture.Redact(in, enginefixture.Redaction{User: "dd", Home: "/home/dd"})

	if header != "none" {
		t.Fatalf("header = %q, want none: the user dd is not replaced", header)
	}
	leaks := enginefixture.CheckLeaksAs("dd", enginefixture.Format(enginefixture.Fixture{Meta: contractMeta(header), Exchange: redacted}), nil, nil)
	var rules []string
	for _, l := range leaks {
		rules = append(rules, l.Rule)
	}
	if !slices.Contains(rules, "lab-identity") {
		t.Errorf("rules = %v, want lab-identity: the net for a user Redact cannot replace", rules)
	}
	for _, l := range leaks {
		if strings.Contains(l.Rule, "dd") {
			t.Errorf("a rule name %q holds the text found", l.Rule)
		}
	}
}
