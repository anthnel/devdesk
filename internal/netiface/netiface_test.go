package netiface

import (
	"context"
	"go/parser"
	"go/token"
	"net"
	"path/filepath"
	"strings"
	"testing"
)

// TestThisMachineListsItsOwnInterfaces exercises the real read. Every machine
// has a loopback, container and CI runner included, so the assertion holds
// wherever the suite runs — and it is the one that would have caught D57, where
// the tab listed a VM's interfaces and not one of the machine's.
func TestThisMachineListsItsOwnInterfaces(t *testing.T) {
	ifaces, err := List(context.Background())
	if err != nil {
		t.Fatalf("listing interfaces: %v", err)
	}
	if len(ifaces) == 0 {
		t.Fatal("no interfaces at all — a machine with no loopback does not exist")
	}

	var loop *Interface
	for i := range ifaces {
		if ifaces[i].State == StateLoop {
			loop = &ifaces[i]
			break
		}
	}
	if loop == nil {
		t.Fatalf("no loopback among %d interfaces", len(ifaces))
	}
	if len(loop.IPv4) == 0 && len(loop.IPv6) == 0 {
		t.Fatal("the loopback carries no address")
	}
	// 127.0.0.1 is IPv4 wherever this runs, so the split has to have filed it
	// as one — reading the family off the rendered string is what this checks
	// did not happen.
	if len(loop.IPv4) == 0 {
		t.Errorf("the loopback has no IPv4 address; IPv6 = %v", loop.IPv6)
	}
	for _, a := range loop.IPv6 {
		if strings.Count(a, ".") == 3 {
			t.Errorf("%q is filed under IPv6", a)
		}
	}
}

// The two lists are disjoint by construction: an address goes to exactly one of
// them, so an interface never shows the same address twice on one row.
func TestNoAddressIsFiledUnderBothFamilies(t *testing.T) {
	ifaces, err := List(context.Background())
	if err != nil {
		t.Fatalf("listing interfaces: %v", err)
	}
	for _, i := range ifaces {
		seen := map[string]bool{}
		for _, a := range append(append([]string{}, i.IPv4...), i.IPv6...) {
			if seen[a] {
				t.Errorf("%s carries %q in both families", i.Name, a)
			}
			seen[a] = true
		}
	}
}

// TestAnInterfaceWhoseCountersAreMissingSaysSoRatherThanZero is D58 in this
// package. A zero written because nobody looked reads as an interface with no
// errors, which is the one thing it must not say.
func TestAnInterfaceWhoseCountersAreMissingSaysSoRatherThanZero(t *testing.T) {
	ifaces, err := List(context.Background())
	if err != nil {
		t.Fatalf("listing interfaces: %v", err)
	}
	for _, i := range ifaces {
		if (i.RxErrors == nil) != (i.TxErrors == nil) {
			t.Fatalf("%s has one counter and not the other — they come from one row "+
				"and must be present or absent together", i.Name)
		}
	}
}

// TestTheStateIsReadFromTheFlagsAndNotGuessed pins the three states against the
// flags they come from, loopback first: an interface that is both up and
// loopback is a loopback, because "is this the machine talking to itself" is
// the first thing to know about the row.
func TestTheStateIsReadFromTheFlagsAndNotGuessed(t *testing.T) {
	cases := []struct {
		flags net.Flags
		want  string
	}{
		{net.FlagUp | net.FlagLoopback | net.FlagRunning, StateLoop},
		{net.FlagUp | net.FlagRunning | net.FlagBroadcast, StateUp},
		{net.FlagBroadcast | net.FlagMulticast, StateDown},
		{0, StateDown},
	}
	for _, c := range cases {
		if got := stateOf(c.flags); got != c.want {
			t.Errorf("stateOf(%v) = %q, want %q", c.flags, got, c.want)
		}
	}
}

// TestAnMTUWindowsDoesNotMeanIsNotShown covers the one platform quirk measured
// here: the loopback pseudo-interface reports -1 under Windows, where ip
// reports 65536. A cell reading "-1" would be a number the machine never meant.
func TestAnMTUWindowsDoesNotMeanIsNotShown(t *testing.T) {
	if (Interface{MTU: -1}).HasMTU() {
		t.Error("an MTU of -1 is shown, want it withheld")
	}
	if (Interface{MTU: 0}).HasMTU() {
		t.Error("an MTU of 0 is shown, want it withheld")
	}
	if !(Interface{MTU: 1500}).HasMTU() {
		t.Error("an MTU of 1500 is withheld, want it shown")
	}
}

// TestNothingHereRunsASubprocessOrTalksToDocker is the guard that keeps the
// defect from coming back the way it arrived. The whole point of this package
// is that it reads this machine, and a container started here would read the
// Docker Desktop VM again without anything on screen saying so.
func TestNothingHereRunsASubprocessOrTalksToDocker(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	banned := map[string]string{
		"os/exec": "starting a process",
		"github.com/anthnel/devdesk/internal/docker": "asking Docker",
	}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing %s: %v", f, err)
		}
		for _, imp := range parsed.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if why, bad := banned[path]; bad {
				t.Errorf("%s imports %s (%s) — this package reads this machine, in this process",
					f, path, why)
			}
		}
	}
}
