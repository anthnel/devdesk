package ociresources

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// OC-17 hardening: the migration runs when the view starts, and the "type the
// values again" hint goes once the user has taken over the field.

const legacyCacheWithCanary = `{"api:v1": {"env": "FOO=bar, API_TOKEN=` + cacheCanary + `", "detach": true}}`

func TestTheMigrationCommandRewritesAnOlderCache(t *testing.T) {
	file := cacheHome(t)
	writeLegacyLaunchCache(t, file, legacyCacheWithCanary)

	if msg := run(t, migrateLaunchOptionsCmd()); msg != nil {
		t.Errorf("the migration returned %T, want no message", msg)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), cacheCanary) {
		t.Errorf("a value is still in the file:\n%s", data)
	}
}

// Init starts it, with the other background reads, without the form being open.
func TestTheViewStartsTheLaunchCacheMigration(t *testing.T) {
	noDocker(t)
	file := cacheHome(t)
	writeLegacyLaunchCache(t, file, legacyCacheWithCanary)

	var wg sync.WaitGroup
	var start func(tea.Cmd)
	start = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if batch, ok := cmd().(tea.BatchMsg); ok {
				for _, c := range batch {
					start(c)
				}
			}
		}()
	}
	start(newTestModel(t).Init())

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(file); err == nil && !strings.Contains(string(data), cacheCanary) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("Init did not migrate the older launch cache")
}

func typeInEnv(f *LaunchForm, msgs ...tea.Msg) {
	focusLaunchField(f, f.fieldEnv())
	f.envInput.CursorEnd()
	for _, m := range msgs {
		f.Update(m)
	}
}

// Once the user has typed a value for every restored name, the hint is gone for
// good: deleting that value again is the user's own empty entry, not a restored one.
func TestTheRetypeHintDoesNotComeBackAfterTheUserTookOverTheField(t *testing.T) {
	f := NewLaunchForm("api:v1", nil, nil, 120)
	f.ApplyCached(cacheEntryNamed("FOO"))
	if !strings.Contains(formText(f), "Values are not saved") {
		t.Fatal("precondition: the hint is shown after the restore")
	}

	typeInEnv(f, testutil.Type("bar")...)
	if strings.Contains(formText(f), "Values are not saved") {
		t.Fatal("the hint stays after the value was typed")
	}
	typeInEnv(f, testutil.Key("backspace"), testutil.Key("backspace"), testutil.Key("backspace"))

	if got := f.envInput.Value(); got != "FOO=" {
		t.Fatalf("Environment = %q, want FOO= after deleting the value", got)
	}
	if strings.Contains(formText(f), "Values are not saved") {
		t.Error("the hint came back for an entry the user emptied")
	}
}

// Typing next to a restored name that still has no value keeps the hint.
func TestTheRetypeHintStaysWhileARestoredNameHasNoValue(t *testing.T) {
	f := NewLaunchForm("api:v1", nil, nil, 120)
	f.ApplyCached(cacheEntryNamed("FOO", "BAR"))

	typeInEnv(f, testutil.Type("x")...) // BAR=x, FOO= still empty

	if !strings.Contains(formText(f), "Values are not saved") {
		t.Error("the hint went while FOO still has no value")
	}
}

func TestTheRetypeHintGoesWhenTheFieldIsCleared(t *testing.T) {
	f := NewLaunchForm("api:v1", nil, nil, 120)
	f.ApplyCached(cacheEntryNamed("FOO"))
	typeInEnv(f, testutil.Key("backspace"), testutil.Key("backspace"), testutil.Key("backspace"), testutil.Key("backspace"))
	if f.envInput.Value() != "" {
		t.Fatalf("Environment = %q, want the field cleared", f.envInput.Value())
	}
	typeInEnv(f, testutil.Type("A=")...) // a new, user-made empty entry

	if strings.Contains(formText(f), "Values are not saved") {
		t.Error("the hint shows for a field the user wrote from scratch")
	}
}

func cacheEntryNamed(names ...string) cache.LaunchOptionsEntry {
	return cache.LaunchOptionsEntry{EnvNames: names}
}
