package ports

import (
	"errors"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"
)

// The identity is read once per distinct PID when the table is listed
// (DEVDESK-709, criterion 1), and on Windows within a budget (criterion 7). All
// of it runs against the fake procOS and the fake clock.

func rowsOf(pids ...string) []Socket {
	rows := make([]Socket, len(pids))
	for i, pid := range pids {
		rows[i] = Socket{Protocol: "tcp", State: "LISTEN", LocalAddr: "0.0.0.0:80", PID: pid}
	}
	return rows
}

// Criterion 1: one read per PID however many sockets it holds, and every row of
// the PID carries the same value.
func TestEachDistinctPIDIsReadOnce(t *testing.T) {
	fake := useFakeOS(t, 100, 99, "linux")
	fake.ids[812] = 5001
	fake.ids[1204] = 6002
	rows := rowsOf("812", "812", "1204", "", "812")

	attachIdentities(rows, fake.clock)

	if want := []int{812, 1204}; !slices.Equal(fake.idCalls, want) {
		t.Errorf("identity was read for %v, want %v: one read per distinct PID, in ascending order", fake.idCalls, want)
	}
	for _, i := range []int{0, 1, 4} {
		if rows[i].Identity != IdentityForTest(5001) {
			t.Errorf("row %d (PID 812) carries %+v, want IdentityForTest(5001)", i, rows[i].Identity)
		}
	}
	if rows[2].Identity != IdentityForTest(6002) {
		t.Errorf("row 2 (PID 1204) carries %+v, want IdentityForTest(6002)", rows[2].Identity)
	}
	if rows[3].Identity != (Identity{}) {
		t.Errorf("the row with no PID carries %+v, want the zero identity", rows[3].Identity)
	}
}

// The order of the reads does not depend on the order of the rows: it is what
// keeps a budget cut on the same PIDs from one refresh to the next.
func TestPIDsAreReadInAscendingOrderWhateverTheRowOrder(t *testing.T) {
	fake := useFakeOS(t, 100, 99, "linux")
	for _, pid := range []int{9, 3, 7} {
		fake.ids[pid] = uint64(pid) * 10
	}

	attachIdentities(rowsOf("9", "3", "9", "7", "3"), fake.clock)

	if want := []int{3, 7, 9}; !slices.Equal(fake.idCalls, want) {
		t.Errorf("identity was read for %v, want %v", fake.idCalls, want)
	}
}

// A row with no usable PID is never read: there is nothing to ask the system.
func TestRowsWithoutAUsablePIDAreNeverRead(t *testing.T) {
	fake := useFakeOS(t, 100, 99, "linux")
	rows := rowsOf("", "0", "-3", "sshd", "8 12")

	attachIdentities(rows, fake.clock)

	if len(fake.idCalls) != 0 {
		t.Errorf("identity was read for %v, want no read", fake.idCalls)
	}
	for i, r := range rows {
		if r.Identity.Verifiable() {
			t.Errorf("row %d (PID %q) is verifiable", i, r.PID)
		}
	}
}

func TestAnUnreadablePIDLeavesOnlyItsRowsUnknown(t *testing.T) {
	for _, readErr := range []error{os.ErrPermission, os.ErrProcessDone, errors.New("short read")} {
		t.Run(readErr.Error(), func(t *testing.T) {
			fake := useFakeOS(t, 100, 99, "linux")
			fake.ids[812] = 5001
			fake.idErr[1204] = readErr
			rows := rowsOf("812", "1204", "1204", "812")

			attachIdentities(rows, fake.clock)

			for _, i := range []int{1, 2} {
				if rows[i].Identity.Verifiable() {
					t.Errorf("row %d (PID 1204, unreadable) is verifiable: %+v", i, rows[i].Identity)
				}
			}
			for _, i := range []int{0, 3} {
				if rows[i].Identity != IdentityForTest(5001) {
					t.Errorf("row %d (PID 812) carries %+v, want IdentityForTest(5001): a failure on another PID must not spread", i, rows[i].Identity)
				}
			}
		})
	}
}

// Linux and macOS have no budget: a slow read does not defer anything.
func TestDarwinAndLinuxHaveNoBudget(t *testing.T) {
	for _, goosName := range []string{"linux", "darwin"} {
		t.Run(goosName, func(t *testing.T) {
			fake := useFakeOS(t, 100, 99, goosName)
			fake.readStep = time.Second
			pids := []string{"11", "12", "13", "14", "15"}
			for _, pid := range []int{11, 12, 13, 14, 15} {
				fake.ids[pid] = uint64(pid)
			}
			rows := rowsOf(pids...)

			attachIdentities(rows, fake.clock)

			if len(fake.idCalls) != 5 {
				t.Errorf("identity was read %d times (%v), want 5: no budget on %s", len(fake.idCalls), fake.idCalls, goosName)
			}
			for i, r := range rows {
				if !r.Identity.Verifiable() || r.Identity.kind != identityKnown {
					t.Errorf("row %d (PID %s) = %+v, want a known identity", i, r.PID, r.Identity)
				}
			}
		})
	}
}

// Criterion 7: on Windows the reads stop once the budget is spent, and the PIDs
// not read are marked deferred with the listing instant, to be read when the
// confirmation opens.
func TestWindowsReadsWithinTheBudgetAndDefersTheRest(t *testing.T) {
	fake := useFakeOS(t, 100, 99, "windows")
	fake.readStep = 40 * time.Millisecond
	listedAt := fake.clock
	created := filetimeOf(listedAt.Add(-time.Second))
	for _, pid := range []int{10, 20, 30, 40} {
		fake.ids[pid] = created + uint64(pid)
	}
	rows := rowsOf("30", "10", "20", "40", "40")

	attachIdentities(rows, listedAt)

	// The reads start at 0, 40 and 80 ms; the fourth would start at 120 ms, past
	// the 100 ms budget.
	if want := []int{10, 20, 30}; !slices.Equal(fake.idCalls, want) {
		t.Errorf("identity was read for %v, want %v", fake.idCalls, want)
	}
	for i, pid := range []int{30, 10, 20} {
		if want := IdentityForTest(created + uint64(pid)); rows[i].Identity != want {
			t.Errorf("row %d (PID %d) carries %+v, want %+v", i, pid, rows[i].Identity, want)
		}
	}
	for _, i := range []int{3, 4} {
		if want := DeferredIdentityForTest(listedAt); rows[i].Identity != want {
			t.Errorf("row %d (PID 40, over the budget) carries %+v, want a deferred identity stamped with the listing instant", i, rows[i].Identity)
		}
	}
}

func TestWindowsDefersNothingWhenTheReadsAreFast(t *testing.T) {
	fake := useFakeOS(t, 100, 99, "windows")
	fake.readStep = time.Millisecond
	listedAt := fake.clock
	created := filetimeOf(listedAt.Add(-time.Second))
	for pid := 1; pid <= 50; pid++ {
		fake.ids[pid] = created
	}
	pids := make([]string, 0, 50)
	for pid := 1; pid <= 50; pid++ {
		pids = append(pids, strconv.Itoa(pid))
	}
	rows := rowsOf(pids...)

	attachIdentities(rows, listedAt)

	if len(fake.idCalls) != 50 {
		t.Errorf("identity was read %d times, want 50: 50 ms of reads fit in the budget", len(fake.idCalls))
	}
	for i, r := range rows {
		if r.Identity.kind != identityKnown {
			t.Errorf("row %d = %+v, want a known identity", i, r.Identity)
		}
	}
}

// A process born after the listing instant is not the one that was listed: on
// Windows the row is left unknown rather than given the newcomer's identity.
func TestWindowsRefusesAProcessBornAfterTheListing(t *testing.T) {
	fake := useFakeOS(t, 100, 99, "windows")
	listedAt := fake.clock
	fake.ids[812] = filetimeOf(listedAt.Add(time.Second))            // born after
	fake.ids[813] = filetimeOf(listedAt.Add(-50 * time.Millisecond)) // inside the slack
	fake.ids[814] = filetimeOf(listedAt.Add(-time.Second))           // born before
	rows := rowsOf("812", "813", "814")

	attachIdentities(rows, listedAt)

	if rows[0].Identity.Verifiable() {
		t.Errorf("PID 812 (born after the listing) carries %+v, want unknown", rows[0].Identity)
	}
	if rows[1].Identity.Verifiable() {
		t.Errorf("PID 813 (born within the slack of the listing) carries %+v, want unknown", rows[1].Identity)
	}
	if want := IdentityForTest(filetimeOf(listedAt.Add(-time.Second))); rows[2].Identity != want {
		t.Errorf("PID 814 (born before the listing) carries %+v, want %+v", rows[2].Identity, want)
	}
}

// An unreadable PID on Windows is unknown, not deferred: deferring would claim
// it can be read later, and the failure is already in hand.
func TestWindowsLeavesAnUnreadablePIDUnknown(t *testing.T) {
	fake := useFakeOS(t, 100, 99, "windows")
	listedAt := fake.clock
	fake.idErr[4412] = os.ErrPermission
	rows := rowsOf("4412")

	attachIdentities(rows, listedAt)

	if rows[0].Identity != (Identity{}) {
		t.Errorf("an unreadable PID carries %+v, want the zero identity", rows[0].Identity)
	}
}

// Off Windows, a creation time is never judged against the listing instant: the
// Linux stamp is in ticks since boot, not a wall-clock time.
func TestOnlyWindowsJudgesTheBirthAgainstTheListing(t *testing.T) {
	for _, goosName := range []string{"linux", "darwin"} {
		fake := useFakeOS(t, 100, 99, goosName)
		fake.ids[812] = filetimeOf(fake.clock.Add(time.Hour)) // would be "born after" on Windows
		rows := rowsOf("812")

		attachIdentities(rows, fake.clock)

		if rows[0].Identity != IdentityForTest(fake.ids[812]) {
			t.Errorf("%s: the row carries %+v, want the stamp as read", goosName, rows[0].Identity)
		}
	}
}
