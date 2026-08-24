package ports

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	gnet "github.com/shirou/gopsutil/v4/net"
)

// ── The reason this package exists ───────────────────────────────────────────

// D55: the Ports tab read `ss` inside `docker run --net=host --pid=host
// --privileged`, and on Docker Desktop that is the VM's namespace — so the tab
// listed the VM's sockets and `K` killed the VM's processes. The correction is
// not "call Docker differently", it is "do not call Docker", and that is a
// property of the source rather than of a behaviour: a helper added here that
// shelled out would put the defect back without failing anything else.
func TestNothingHereRunsASubprocessOrTalksToDocker(t *testing.T) {
	banned := map[string]string{
		"os/exec": "a socket table read in this process is the whole point (D55)",
		"github.com/anthnel/devdesk/internal/docker": "the Ports tab must work on a machine with no Docker",
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if why, bad := banned[path]; bad {
				t.Errorf("%s imports %q — %s", fset.Position(imp.Pos()), path, why)
			}
		}
	}
}

// ── A row ────────────────────────────────────────────────────────────────────

// A PID of zero is the system declining to attribute the socket, and `K` keys on
// this field: rendered as "0" the row would look killable, and on Unix signalling
// process 0 reaches the whole process group.
func TestASocketWithNoProcessCarriesNoPID(t *testing.T) {
	s := toSocket(gnet.ConnectionStat{
		Family: 2, Type: 1, Status: "LISTEN", Pid: 0,
		Laddr: gnet.Addr{IP: "0.0.0.0", Port: 445},
	}, map[int32]string{0: "System Idle Process"})

	if s.PID != "" {
		t.Errorf("PID = %q, want empty — the system named no process", s.PID)
	}
	if s.Process != "" {
		t.Errorf("Process = %q, want empty", s.Process)
	}
}

// The name is a separate question from the PID: on Windows another user's
// process is enumerable but not openable, so a row can legitimately know which
// process holds the socket and not what it is called. It must still be killable
// — that is the row the user is trying to act on.
func TestAPIDSurvivesAnUnnamedProcess(t *testing.T) {
	s := toSocket(gnet.ConnectionStat{
		Family: 2, Type: 1, Status: "LISTEN", Pid: 1388,
		Laddr: gnet.Addr{IP: "0.0.0.0", Port: 135},
	}, nil)

	if s.PID != "1388" {
		t.Errorf("PID = %q, want 1388", s.PID)
	}
	if s.Process != "" {
		t.Errorf("Process = %q, want empty", s.Process)
	}
}

// ── The state vocabulary ─────────────────────────────────────────────────────

// An unconnected datagram socket is "NONE" on Linux and nothing at all on
// Windows: without this the same socket read differently depending on where
// DevDesk was running, and the l/e filters would answer differently too.
func TestAnUnconnectedDatagramSocketReadsTheSameOnEveryPlatform(t *testing.T) {
	for _, reported := range []string{"", "NONE"} {
		if got := stateOf(reported, 2); got != "UNCONN" {
			t.Errorf("stateOf(%q, udp) = %q, want UNCONN", reported, got)
		}
	}
}

// The State column is ten cells wide, so the long spelling is truncated to
// "ESTABLISHE" — and the filter token toggled with `e` reads better short.
func TestAnEstablishedSocketIsShortenedToFitItsColumn(t *testing.T) {
	if got := stateOf("ESTABLISHED", 1); got != "ESTAB" {
		t.Errorf("stateOf(ESTABLISHED, tcp) = %q, want ESTAB", got)
	}
	if got := stateOf("TIME_WAIT", 1); got != "TIME_WAIT" {
		t.Errorf("stateOf(TIME_WAIT) = %q; only ESTABLISHED is shortened", got)
	}
}

// A stream socket keeps whatever the system called it, even when that is
// nothing: inventing a state for TCP would be saying something the kernel did
// not.
func TestAStreamSocketKeepsTheStateTheSystemGaveIt(t *testing.T) {
	if got := stateOf("", 1); got != "" {
		t.Errorf("stateOf(empty, tcp) = %q, want it left alone", got)
	}
}

func TestBothFamiliesPrintTheSameProtocolWord(t *testing.T) {
	if got := protocolOf(1); got != "tcp" {
		t.Errorf("SOCK_STREAM = %q, want tcp", got)
	}
	if got := protocolOf(2); got != "udp" {
		t.Errorf("SOCK_DGRAM = %q, want udp", got)
	}
}

// ── Addresses ────────────────────────────────────────────────────────────────

func TestAnIPv6AddressIsBracketed(t *testing.T) {
	got := formatAddr(gnet.Addr{IP: "::", Port: 111}, 23)
	if got != "[::]:111" {
		t.Errorf("formatAddr = %q, want [::]:111 — unbracketed it is unreadable beside a port", got)
	}
}

// A listening socket has no peer, and the wildcard says which family it would
// have had one on. The AF_INET6 constant differs per platform, which is why all
// three are named rather than one compared against.
func TestASocketWithNoPeerShowsTheWildcardForItsFamily(t *testing.T) {
	if got := formatAddr(gnet.Addr{}, 2); got != "0.0.0.0:*" {
		t.Errorf("v4 peer = %q, want 0.0.0.0:*", got)
	}
	for _, family := range []uint32{23, 10, 30} { // windows, linux, darwin
		if got := formatAddr(gnet.Addr{}, family); got != "[::]:*" {
			t.Errorf("v6 peer (family %d) = %q, want [::]:*", family, got)
		}
	}
}

// ── Kill ─────────────────────────────────────────────────────────────────────

// The one thing Kill must never do is act on something that is not a process id.
// Zero is a process group on Unix and the idle process on Windows; a negative
// number is a whole group; an empty string is a row the system attributed to
// nobody, which is exactly the row a user is most likely to press K on.
func TestKillRefusesAnythingThatIsNotAProcessID(t *testing.T) {
	for _, pid := range []string{"", "0", "-1", "-812", "sshd", "8 12", "812.0"} {
		if err := Kill(pid); err == nil {
			t.Errorf("Kill(%q) returned no error", pid)
		}
	}
}

// ── Against the machine ──────────────────────────────────────────────────────

// The invariants that matter are the ones a fixture cannot check: that the
// system answers at all, and that every row it produces is one the rest of the
// application can act on.
func TestTheMachinesOwnSocketTableSatisfiesTheRowInvariants(t *testing.T) {
	sockets, err := List(context.Background(), false)
	if err != nil {
		t.Skipf("the socket table is not readable here: %v", err)
	}
	if len(sockets) == 0 {
		t.Skip("no sockets to check")
	}
	for _, s := range sockets {
		if s.PID != "" {
			n, err := strconv.Atoi(s.PID)
			if err != nil || n <= 0 {
				t.Fatalf("PID %q is not something Kill would accept", s.PID)
			}
		}
		if s.Process != "" && s.PID == "" {
			t.Fatalf("a socket named a process (%q) with no PID to act on", s.Process)
		}
		if s.Protocol != "tcp" && s.Protocol != "udp" {
			t.Fatalf("protocol = %q, want tcp or udp", s.Protocol)
		}
		if !strings.Contains(s.LocalAddr, ":") {
			t.Fatalf("local address %q carries no port", s.LocalAddr)
		}
	}
}
