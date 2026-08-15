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
// **La moyenne de charge n'est lue nulle part, sur aucune plateforme.** Sur
// Windows `load.Avg()` retourne `{0, 0, 0}` avec `err == nil` : elle n'échoue
// pas, elle produit un nombre indiscernable d'une donnée, et un zéro se lit
// comme « la machine est au repos ». Une métrique présente sur deux
// plateformes sur trois est pire qu'une métrique absente partout, parce que son
// absence ne se voit pas.
func SampleHost(prev Counters) (HostSample, Counters) {
	s := HostSample{Taken: time.Now()}

	// cpu.Percent(0, …) calcule depuis l'appel précédent au lieu de bloquer
	// pendant l'intervalle — un cpu.Percent(500ms) coûterait 501 ms à chaque
	// tick de l'horloge rapide.
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

// coreCount reports the logical core count, read once. Ce n'est pas une mesure
// qui bouge, et cpu.Counts fait un appel système : le compter à chaque
// échantillon serait le seul coût qui grandit avec la fréquence de l'horloge
// rapide sans rien apprendre.
//
// Le memo est immuable et vit dans le package, pas dans le modèle : la Rule 110
// interdit à un Cmd de modifier l'état *du modèle*, et une valeur constante
// calculée une fois n'en est pas.
var coreCount = sync.OnceValue(func() int {
	n, err := cpu.Counts(true)
	if err != nil {
		log.Printf("ERROR [metrics/host] cpu.Counts: %v", err)
		return 0
	}
	return n
})

// readNetCounters sums every interface's cumulative byte counters.
func readNetCounters(at time.Time) Counters {
	stats, err := net.IOCounters(false)
	if err != nil || len(stats) == 0 {
		if err != nil {
			log.Printf("ERROR [metrics/host] net.IOCounters: %v", err)
		}
		return Counters{At: at}
	}
	return Counters{RX: stats[0].BytesRecv, TX: stats[0].BytesSent, At: at, Valid: true}
}

// Disk reports free space on the filesystem holding path. Mesuré à 1 ms, contre
// plusieurs secondes pour un `du -sh` de l'arborescence : la question utile est
// « combien reste-t-il », pas « combien pèse cet arbre ».
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
