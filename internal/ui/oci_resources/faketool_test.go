package ociresources

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// The commands in commands.go shell out to docker and, for a scan, to trivy.
// Rather than stub the seam inside internal/docker — which is unexported, and
// stubbing it would leave the wrapper's own error handling untested — the tools
// are installed on a PATH of the test's own making, by the shared fake binary of
// internal/testutil/fakecli (OC-02). It runs in strict mode: an argv the script
// does not declare fails the test, with the argv received.

// fakeReply is what a fake tool does for one invocation.
type fakeReply = fakecli.Reply

// fakeScript maps an invocation prefix — the tool name followed by the leading
// arguments, e.g. "docker image ls" — to the reply it produces. The longest
// matching prefix wins, so "docker image ls" and "docker image inspect" can be
// scripted separately. A bare tool name ("docker") answers every argv of that
// tool, which is how "docker accepted the command and printed nothing" is
// expressed.
type fakeScript map[string]fakeReply

// Exchanges makes the script a fakecli.Source: every key is a prefix exchange.
func (s fakeScript) Exchanges() []fakecli.Exchange {
	keys := make([]string, 0, len(s))
	for key := range s {
		keys = append(keys, key)
	}
	slices.Sort(keys) // map order is random; the longest prefix decides, ties do not occur
	out := make([]fakecli.Exchange, 0, len(s))
	for _, key := range keys {
		words := strings.Fields(key)
		out = append(out, fakecli.Exchange{Tool: words[0], Args: words[1:], Prefix: true, Reply: s[key]})
	}
	return out
}

// installFakeTools puts the named tools on an otherwise empty PATH and scripts
// how they answer. The returned Fake holds the journal of what they were asked.
func installFakeTools(t *testing.T, script fakeScript, names ...string) *fakecli.Fake {
	t.Helper()
	return fakecli.Install(t, script, names...)
}

// installFakeDocker is the common case: docker alone, answering one script.
func installFakeDocker(t *testing.T, script fakeScript) *fakecli.Fake {
	t.Helper()
	return installFakeTools(t, script, "docker")
}

// noDocker points PATH at a directory holding nothing, which is what an
// uninstalled docker looks like to exec.LookPath.
func noDocker(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

// run executes a command and returns the single message it produced. tea.Cmd is
// a function returning a message, so this is what the Bubble Tea runtime does
// with it.
func run(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command to run")
	}
	return cmd()
}
