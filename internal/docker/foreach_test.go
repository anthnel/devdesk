package docker

import (
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/engine"
)

// OC-07: a test whose result depends on the engine runs once per declared
// engine, through forEachEngine. These tests pin the helper itself. The seams
// it drives (the runner, the current engine) are global, so none of them calls
// t.Parallel.
//
// The helper is defined next to stub and useEngine in exec_test.go. For every
// name in engine.Names() it runs fn in a subtest named after the engine, with
// useEngine already applied, and puts the original engine back when the
// subtests are over.

func TestForEachEngineRunsOncePerDeclaredEngine(t *testing.T) {
	// A sentinel that is neither declared shape, so "restored" cannot be
	// mistaken for "left on docker".
	sentinel := engine.Shape{Name: engine.Docker, Binary: "oc07-sentinel"}
	previous := engine.Current()
	engine.SetCurrent(sentinel)
	t.Cleanup(func() { engine.SetCurrent(previous) })

	var visited, current, subtests []string
	t.Run("inner", func(t *testing.T) {
		forEachEngine(t, func(t *testing.T, shape engine.Shape) {
			visited = append(visited, shape.Name)
			current = append(current, engine.Current().Name)
			subtests = append(subtests, t.Name())
		})
	})

	if want := engine.Names(); !slices.Equal(visited, want) {
		t.Errorf("forEachEngine visited %v, want once per declared engine %v", visited, want)
	}
	// fn must see the engine it was handed already pinned, or an argv built
	// from engine.Current() would belong to the previous iteration.
	if !slices.Equal(current, visited) {
		t.Errorf("engine.Current() inside fn = %v, want %v", current, visited)
	}
	for i, name := range visited {
		if !strings.HasSuffix(subtests[i], "/"+name) {
			t.Errorf("subtest %q is not named after the engine %q", subtests[i], name)
		}
	}
	if got := engine.Current(); got != sentinel {
		t.Errorf("engine.Current() after forEachEngine = %+v, want the engine it found, %+v", got, sentinel)
	}
}

// The shape handed to fn is the one pinned for the subtest, under the name the
// subtest carries, and it comes with the templates a test builds its argv from.
func TestForEachEngineHandsOverThePinnedShape(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		if got := engine.Current(); shape != got {
			t.Errorf("shape = %+v, want the engine pinned for the subtest, %+v", shape, got)
		}
		if !strings.HasSuffix(t.Name(), "/"+shape.Name) {
			t.Errorf("subtest %q does not carry the name of its shape %q", t.Name(), shape.Name)
		}
		if shape.Binary == "" || shape.Templates.PS == "" {
			t.Errorf("shape = %+v, want a binary and templates to build an argv from", shape)
		}
	})
}

// OC-07 criterion 4: an engine added to the configuration must fail a test
// until forEachEngine runs it. The configuration is the authority on what can
// be selected, so the comparison starts there rather than from engine.Names().
//
// It sits beside TestEveryConfiguredEngineHasAShape (internal/engine), which
// holds config and shapes in step; this one holds the helper to the same list.
func TestForEachEngineCoversEveryConfiguredEngine(t *testing.T) {
	var visited []string
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		visited = append(visited, shape.Name)
	})

	for _, name := range config.ContainerEngines() {
		if name == config.EngineAuto {
			// auto is a resolution strategy, not an engine.
			continue
		}
		if !slices.Contains(visited, name) {
			t.Errorf("config offers %q but forEachEngine never runs it", name)
		}
	}
	for _, name := range visited {
		if !slices.Contains(config.ContainerEngines(), name) {
			t.Errorf("forEachEngine runs %q but the configuration never offers it", name)
		}
	}
}
