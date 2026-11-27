package enginefixture

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Replay answers an argv from a selection of fixtures. The key is the tool and the
// argv, exact, element by element: the order counts and no flag is reordered.
// Version and mode are not in the key; a test chooses its configuration
// (ForConfig). It has no hidden order: a sequence of states (ps before and after)
// is two Replays. Safe for concurrent use.
type Replay struct {
	mu    sync.Mutex
	byKey map[string]Fixture
	used  map[string]bool
}

// NewReplay builds a Replay. It refuses an empty selection, a pending fixture, a
// skipped one ("record: skip: <reason>", kept in the tree but never replayed) and
// two fixtures with the same tool and argv, and its error names them.
func NewReplay(fixtures ...Fixture) (*Replay, error) {
	if len(fixtures) == 0 {
		return nil, errors.New("enginefixture: no fixture to replay")
	}
	r := &Replay{byKey: map[string]Fixture{}, used: map[string]bool{}}
	for _, f := range fixtures {
		if f.Meta.Record == RecordPending {
			return nil, fmt.Errorf("enginefixture: fixture %s is pending (no recorded answer)", label(f))
		}
		if isSkipped(f) {
			return nil, fmt.Errorf("enginefixture: fixture %s is skipped (%s) and is never replayed", label(f), f.Meta.Record)
		}
		key := keyOf(f.Meta.Tool, f.Exchange.Argv)
		if twin, ok := r.byKey[key]; ok {
			return nil, fmt.Errorf("enginefixture: fixtures %s and %s have the same tool and argv; select one (ForConfig)", label(twin), label(f))
		}
		r.byKey[key] = f
	}
	return r, nil
}

// Answer returns a copy of the fixture recorded for the tool and the argv, and
// marks it used. A miss uses nothing.
func (r *Replay) Answer(tool Tool, argv []string) (Fixture, bool) {
	key := keyOf(tool, argv)
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.byKey[key]
	if !ok {
		return Fixture{}, false
	}
	r.used[key] = true
	f.Exchange.Argv = slices.Clone(f.Exchange.Argv)
	f.Exchange.Stdout = bytes.Clone(f.Exchange.Stdout)
	f.Exchange.Stderr = bytes.Clone(f.Exchange.Stderr)
	return f, true
}

// Unused lists, sorted, the names of the fixtures no Answer has returned.
func (r *Replay) Unused() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var names []string
	for key, f := range r.byKey {
		if !r.used[key] {
			names = append(names, label(f))
		}
	}
	slices.Sort(names)
	return names
}

// keyOf is the match key. Each word is quoted, so a word that holds a space, a
// quote or a byte that is not UTF-8 cannot be taken for another argv.
func keyOf(tool Tool, argv []string) string {
	words := make([]string, len(argv))
	for i, w := range argv {
		words[i] = strconv.Quote(w)
	}
	return string(tool) + " " + strings.Join(words, " ")
}

// label is how a fixture is named in a message: its file name, or its key.
func label(f Fixture) string {
	if f.Name != "" {
		return f.Name
	}
	return keyOf(f.Meta.Tool, f.Exchange.Argv)
}
