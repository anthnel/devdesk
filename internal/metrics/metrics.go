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
	RX uint64
	TX uint64
	// RXErrors and TXErrors are read from the same net.IOCounters call as RX
	// and TX, so they reset with it — a counter rollback that invalidates the
	// bytes invalidates these too.
	RXErrors uint64
	TXErrors uint64
	At       time.Time
	Valid    bool
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

// NetWindow tracks bytes and errors since it started watching — since DevDesk
// launched, not since boot: net.IOCounters is cumulative from boot, and a
// total framed that way would read as an old incident rather than what
// happened during this session.
//
// Baseline is the reference the current, unbroken run of readings is measured
// against. Carried is what earlier runs — ended by a counter that went
// backward, an interface or the machine restarting — had already reached: the
// window re-bases onto the reset rather than losing that history to it.
type NetWindow struct {
	Baseline Counters
	Carried  Counters
}

// Advance folds one more valid reading into the window. prev is the counters
// from the tick before cur; it is only consulted when a reset is detected, to
// know how far the window that just ended had gotten.
//
// A prev or window with no usable baseline starts (or restarts) the window at
// cur rather than risk comparing against a zero value that was never
// measured.
func (w NetWindow) Advance(prev, cur Counters) NetWindow {
	if !cur.Valid {
		return w
	}
	if !w.Baseline.Valid || !prev.Valid {
		return NetWindow{Baseline: cur, Carried: w.Carried}
	}
	if cur.RX < w.Baseline.RX || cur.TX < w.Baseline.TX {
		return NetWindow{
			Baseline: cur,
			Carried: Counters{
				RX:       w.Carried.RX + (prev.RX - w.Baseline.RX),
				TX:       w.Carried.TX + (prev.TX - w.Baseline.TX),
				RXErrors: w.Carried.RXErrors + (prev.RXErrors - w.Baseline.RXErrors),
				TXErrors: w.Carried.TXErrors + (prev.TXErrors - w.Baseline.TXErrors),
			},
		}
	}
	return w
}

// Totals reports bytes and errors since the window started watching, valid
// once a baseline has been captured. cur is the latest reading; it is not
// stored on NetWindow because Advance already folded it into Baseline or
// left it out as invalid.
func (w NetWindow) Totals(cur Counters) Counters {
	if !w.Baseline.Valid || !cur.Valid {
		return Counters{}
	}
	return Counters{
		RX:       w.Carried.RX + (cur.RX - w.Baseline.RX),
		TX:       w.Carried.TX + (cur.TX - w.Baseline.TX),
		RXErrors: w.Carried.RXErrors + (cur.RXErrors - w.Baseline.RXErrors),
		TXErrors: w.Carried.TXErrors + (cur.TXErrors - w.Baseline.TXErrors),
		Valid:    true,
	}
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
