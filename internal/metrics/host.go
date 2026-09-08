package metrics

import (
	"log"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
)

// SampleHost reads the host once and returns the reading plus the cumulative
// counters the *next* call must be given.
//
// **Load average is not read anywhere, on any platform.** On
// Windows `load.Avg()` returns `{0, 0, 0}` with `err == nil`: it does not
// fail, it produces a number indistinguishable from real data, and a zero
// reads as "the machine is idle". A metric present on two platforms out of
// three is worse than a metric absent everywhere, because its absence does
// not show.
func SampleHost(prev Counters) (HostSample, Counters) {
	s := HostSample{Taken: time.Now()}

	// cpu.Percent(0, …) computes from the previous call instead of blocking
	// for the interval — a cpu.Percent(500ms) would cost 501 ms on every
	// tick of the fast clock.
	if pct, err := cpu.Percent(0, false); err == nil && len(pct) > 0 {
		s.CPUPercent = pct[0]
		s.OK = true
	} else if err != nil {
		log.Printf("ERROR [metrics/host] cpu.Percent: %v", err)
	}
	s.Cores = coreCount()

	if vm, err := mem.VirtualMemory(); err == nil && vm != nil {
		s.MemPercent = vm.UsedPercent
		s.MemUsed = vm.Used
		s.MemTotal = vm.Total
		s.OK = true
	} else if err != nil {
		log.Printf("ERROR [metrics/host] mem.VirtualMemory: %v", err)
	}

	next := readNetCounters(s.Taken)
	if rx, tx, ok := rate(prev, next); ok {
		s.NetRXPerSec, s.NetTXPerSec, s.HasRate = rx, tx, true
	}

	return s, next
}

// coreCount reports the logical core count, read once. This is not a value
// that changes, and cpu.Counts makes a system call: counting it on every
// sample would be the one cost that grows with the fast clock's frequency
// without learning anything.
//
// The memo is immutable and lives in the package, not in the model: Rule 110
// forbids a Cmd from modifying the *model's* state, and a constant value
// computed once is not that.
var coreCount = sync.OnceValue(func() int {
	n, err := cpu.Counts(true)
	if err != nil {
		log.Printf("ERROR [metrics/host] cpu.Counts: %v", err)
		return 0
	}
	return n
})

// readNetCounters sums every interface's cumulative byte and error counters.
func readNetCounters(at time.Time) Counters {
	stats, err := net.IOCounters(false)
	if err != nil || len(stats) == 0 {
		if err != nil {
			log.Printf("ERROR [metrics/host] net.IOCounters: %v", err)
		}
		return Counters{At: at}
	}
	return Counters{
		RX: stats[0].BytesRecv, TX: stats[0].BytesSent,
		RXErrors: stats[0].Errin, TXErrors: stats[0].Errout,
		At: at, Valid: true,
	}
}

// Disk reports free space on the filesystem holding path. Measured at 1 ms,
// against several seconds for a `du -sh` of the tree: the useful question is
// "how much is left", not "how heavy is this tree".
func Disk(path string) DiskUsage {
	usage, err := disk.Usage(path)
	if err != nil || usage == nil {
		if err != nil {
			log.Printf("ERROR [metrics/host] disk.Usage(%q): %v", path, err)
		}
		return DiskUsage{Path: path}
	}
	return DiskUsage{
		Path:        path,
		Free:        usage.Free,
		Used:        usage.Used,
		Total:       usage.Total,
		UsedPercent: usage.UsedPercent,
		OK:          true,
	}
}
