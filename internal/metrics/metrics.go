// Package metrics samples the machine DevDesk runs on. It owns
// the sampling and the rate computation; display belongs to the view.
//
// Nothing here keeps state: a rate is computed between two cumulative
// readings, and the previous reading **travels with the message** rather
// than being stored in the package. A Bubble Tea `Cmd` must not modify any
// shared state (Rule 110), and two samplings overlapping on package state
// would produce a rate computed against the wrong instant.
package metrics

import "time"

// HostSample is one reading of the host.
type HostSample struct {
	Taken time.Time

	CPUPercent float64
	// Cores is the logical core count, which a percentage needs to be read:
	// 40% on four cores and 40% on thirty-two do not describe the same
	// machine. It does not change from one sample to the next and is
	// counted only once.
	Cores      int
	MemPercent float64
	MemUsed    uint64
	MemTotal   uint64

	// NetRXPerSec and NetTXPerSec are bytes per second, and mean nothing unless
	// HasRate is true: net.IOCounters is cumulative, so the first reading after
	// a start has nothing to subtract from. The view displays `-`, not `0`.
	NetRXPerSec float64
	NetTXPerSec float64
	HasRate     bool

	// OK is false when the host could not be read at all.
	OK bool
}

// Counters is the cumulative network reading a rate is measured against. It
// is returned with the sample and passed back in for the next one.
type Counters struct {
	RX    uint64
	TX    uint64
	At    time.Time
	Valid bool
}

// rate returns the per-second deltas between two cumulative readings.
//
// It refuses three situations rather than inventing a number:
//   - no previous reading — the first sample has no rate;
//   - a counter that goes backward — a reset interface, a rollover: the
//     subtraction would give a negative rate, which does not exist;
//   - a zero or negative interval — a division by zero, or a clock that
//     went backward.
//
// In all three cases the sample is *dropped*, not rendered as zero: zero is
// a measurement, and this one did not happen.
func rate(prev, cur Counters) (rx, tx float64, ok bool) {
	if !prev.Valid || !cur.Valid {
		return 0, 0, false
	}
	if cur.RX < prev.RX || cur.TX < prev.TX {
		return 0, 0, false
	}
	elapsed := cur.At.Sub(prev.At).Seconds()
	if elapsed <= 0 {
		return 0, 0, false
	}
	return float64(cur.RX-prev.RX) / elapsed, float64(cur.TX-prev.TX) / elapsed, true
}

// DiskUsage is one filesystem's occupancy.
//
// `Used` is taken as-is from the system rather than computed as
// `Total - Free`: on ext4 the blocks reserved for root are neither free nor
// used, so the two are not equal, and a byte count displayed next to a
// percentage must come from the same accounting as it.
type DiskUsage struct {
	Path        string
	Free        uint64
	Used        uint64
	Total       uint64
	UsedPercent float64
	OK          bool
}

// TreeSize is how much disk one directory tree occupies — what cloned
// repositories cost, not the fill level of the volume that carries them.
// It is the one of the two that can be acted on.
type TreeSize struct {
	Path  string
	Bytes uint64
	// Partial is true when part of the tree could not be read. A refused
	// directory makes the total an underestimate, and an underestimated
	// total without a mention of it reads as an exact measurement.
	Partial bool
	OK      bool
}
