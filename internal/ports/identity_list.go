package ports

import (
	"slices"
	"strconv"
	"time"
)

// attachIdentities reads the identity of every distinct PID of the table once
// and gives it to each row of that PID (DEVDESK-709, criterion 1). listedAt is
// the instant taken before the socket snapshot.
//
// The PIDs are read in ascending order, so that when the Windows budget cuts the
// reads it cuts the same PIDs from one refresh to the next. A PID that cannot be
// read leaves its rows unknown.
//
// On Windows a creation time is a wall-clock time: a process born after the
// listing (or too close to it to tell) is not the one that was listed, so its
// row stays unknown. On Linux and macOS the stamp is not comparable with a clock
// and is taken as read; the identity is read after the snapshot, so a PID that
// changed hands in between would carry the newcomer's identity. That window is a
// few milliseconds and needs a full turn of pid_max; it is accepted.
func attachIdentities(sockets []Socket, listedAt time.Time) {
	pids := distinctPIDs(sockets)
	identities := make(map[string]Identity, len(pids))
	windows := goos == "windows"
	start := now()
	for _, pid := range pids {
		key := strconv.Itoa(pid)
		if windows && now().Sub(start) >= windowsIdentityBudget {
			identities[key] = deferredAt(listedAt)
			continue
		}
		identities[key] = readListedIdentity(pid, listedAt, windows)
	}
	for i := range sockets {
		sockets[i].Identity = identities[sockets[i].PID]
	}
}

// readListedIdentity reads one PID at listing time.
func readListedIdentity(pid int, listedAt time.Time, windows bool) Identity {
	raw, err := procOS.identity(pid)
	if err != nil {
		return Identity{}
	}
	if windows {
		createdNs, ok := filetimeUnixNano(raw)
		if !ok || judgeBirth(createdNs, listedAt.UnixNano(), windowsBirthSlack) != bornBefore {
			return Identity{}
		}
	}
	return Identity{kind: identityKnown, raw: raw}
}

// distinctPIDs is the sorted list of the PIDs the rows can be signalled by.
func distinctPIDs(sockets []Socket) []int {
	var pids []int
	for _, s := range sockets {
		if n, ok := parsePID(s.PID); ok && n > 0 {
			pids = append(pids, n)
		}
	}
	slices.Sort(pids)
	return slices.Compact(pids)
}
