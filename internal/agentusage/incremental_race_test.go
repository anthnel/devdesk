package agentusage

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

// Concurrency tests of the incremental reader (DEVDESK-44 step 6). Run them
// under -race: one goroutine owns the Aggregate (ReadDelta + Apply), another
// appends to the transcripts mid-line, others read copies.

// writerChunks builds the bytes a writer appends: whole lines, cut at
// arbitrary points (also inside a CRLF), so a reader often sees a torn line.
func writerChunks(prefix string, lines int) [][]byte {
	var all bytes.Buffer
	for i := range lines {
		id := fmt.Sprintf("%s-%d", prefix, i%7)
		all.Write(msgLine(id, "claude-r", "end_turn", int64(i), 2, 3, 4))
		if i%3 == 0 {
			all.WriteString("\r\n")
		} else {
			all.WriteByte('\n')
		}
		if i%5 == 0 {
			all.WriteString("not json {{\n")
		}
	}
	data := all.Bytes()
	var out [][]byte
	for off, step := 0, 1; off < len(data); step = step%37 + 5 {
		end := min(off+step, len(data))
		out = append(out, data[off:end])
		off = end
	}
	return out
}

func appendRaw(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// waitFor spins, yielding the processor, until cond holds. It uses no sleep:
// the conditions it waits for are progress counters of goroutines that keep
// running on their own.
func waitFor(cond func() bool) {
	for !cond() {
		runtime.Gosched()
	}
}

// The writer is paced by the owner and the readers: at fixed points it waits
// until the owner has done more ReadDelta+Apply passes, and the readers more
// reads of the published copies, so the interleaving is exercised rather than
// hoped for.
func TestConcurrentWriterReaderMatchesParse(t *testing.T) {
	const (
		ownerPassesPerStop  = 5
		readerPassesPerStop = 20
	)
	s := newSess(t)
	if err := os.MkdirAll(s.subDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	s.put(s.parent(), nil) // the parent transcript exists before the first read
	a := NewAggregate(s.parent())

	parentChunks := writerChunks("p", 60)
	subChunks := writerChunks("s", 60)
	total := max(len(parentChunks), len(subChunks))
	var wg sync.WaitGroup
	var done atomic.Bool
	var werr atomic.Value
	var ownerPasses, readerPasses atomic.Int64

	// The owner is this goroutine; readers see only copies it publishes.
	var mu sync.Mutex
	var curCopy Cursor
	var useCopy Usage
	publish := func() {
		c, u := a.Cursor(), a.Usage()
		mu.Lock()
		curCopy, useCopy = c, u
		mu.Unlock()
		ownerPasses.Add(1)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer done.Store(true)
		for i := range total {
			if i > 0 && i%(total/4+1) == 0 { // a few stops while the files are being written
				o, r := ownerPasses.Load(), readerPasses.Load()
				waitFor(func() bool {
					return ownerPasses.Load() >= o+ownerPassesPerStop && readerPasses.Load() >= r+readerPassesPerStop
				})
			}
			if i < len(parentChunks) {
				if err := appendRaw(s.parent(), parentChunks[i]); err != nil {
					werr.Store(err)
					return
				}
			}
			if i < len(subChunks) {
				if err := appendRaw(s.sub("a1"), subChunks[i]); err != nil {
					werr.Store(err)
					return
				}
			}
		}
	}()

	var rwg sync.WaitGroup
	var stop atomic.Bool
	for range 3 {
		rwg.Add(1)
		go func() {
			defer rwg.Done()
			for !stop.Load() {
				mu.Lock()
				c, u := curCopy, useCopy
				mu.Unlock()
				_ = c.String()
				_ = u.Total() // read-only: the published copy is shared by the readers
				_ = u.Diagnostics.Degraded()
				readerPasses.Add(1)
			}
		}()
	}

	for !done.Load() {
		readApply(t, a)
		publish()
	}
	wg.Wait()
	if err, _ := werr.Load().(error); err != nil {
		t.Fatalf("writer: %v", err)
	}
	if ownerPasses.Load() < 3*ownerPassesPerStop || readerPasses.Load() < 3*readerPassesPerStop {
		t.Errorf("interleaving not exercised: %d owner passes, %d reader passes", ownerPasses.Load(), readerPasses.Load())
	}
	readApply(t, a)
	readApply(t, a)
	publish()
	stop.Store(true)
	rwg.Wait()

	assertMatchesParse(t, a, s)
}

// Mutating what Usage() and Cursor() return does not reach the aggregate. The
// Aggregate is single-owner: only this goroutine calls its methods. The mutators
// get copies taken by the owner beforehand, mutate them while the owner keeps
// reading the aggregate and checking it did not move.
func TestConcurrentCopiesShareNoMemory(t *testing.T) {
	const mutators = 4
	s := newSess(t)
	s.put(s.parent(), append(msgLine("m1", "claude-r", "end_turn", 1, 2, 3, 4), '\n'))
	s.put(s.sub("a1"), append(msgLine("m2", "claude-r", "end_turn", 1, 2, 3, 4), '\n'))
	a := NewAggregate(s.parent())
	readApply(t, a)
	want := a.Usage()

	usages := make([]Usage, mutators)
	cursors := make([]Cursor, mutators)
	for i := range mutators {
		usages[i], cursors[i] = a.Usage(), a.Cursor()
	}

	var wg sync.WaitGroup
	var stop atomic.Bool
	var began atomic.Int64
	for i := range mutators {
		u, c := usages[i], cursors[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			for first := true; !stop.Load(); first = false {
				for j := range u.Agents {
					u.Agents[j].Messages += 100
					if len(u.Agents[j].Models) < 8 {
						u.Agents[j].Models = append(u.Agents[j].Models, "x")
					}
				}
				if len(u.Versions) < 8 {
					u.Versions = append(u.Versions, "v")
				}
				if len(u.Diagnostics.TruncatedFiles) < 8 {
					u.Diagnostics.TruncatedFiles = append(u.Diagnostics.TruncatedFiles, "x")
				}
				if len(c.files) > 0 {
					c.files[0].offset += 99
				}
				if len(c.files) < 8 {
					c.files = append(c.files, fileCursor{})
				}
				if first {
					began.Add(1)
				}
			}
		}()
	}

	waitFor(func() bool { return began.Load() == mutators })
	for range 100 { // the owner reads the aggregate while the copies are being mutated
		assertSameUsage(t, a.Usage(), want)
		_ = a.Cursor()
	}
	stop.Store(true)
	wg.Wait()
	assertSameUsage(t, a.Usage(), want)
	readApply(t, a)
	assertMatchesParse(t, a, s)
}
