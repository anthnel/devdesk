package docker

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// OC-09b1 fix (DEVDESK-807), measured on docker 29.10.1 and 28.5.2: a container the engine
// made for a refused `run` (a port it could not bind) has no label, and the template
// `{{index .Config.Labels "devdesk.test"}}` prints `<no value>` for it, not an empty string.
// The look expectRefusal makes after the window read the line `<id> <no value>`, cut it on
// the space and took `<no` for a label of another run, so the container was left behind.
// podman makes no container for a refused run. A value that says "no label" is empty or
// `<no value>`; the line is cut at its first space (an id has none) and the rest is the label.
// The names of these tests do not start with "TestContract" (guard G5, R3).

func TestExpectRefusalRemovesTheContainerOfARefusedRunWhateverWayTheEngineSaysItHasNoLabel(t *testing.T) {
	tests := []struct {
		name     string
		lookup   string
		wantRm   bool
		wantErrs []string // lower-case substrings of the reports; none = no report
	}{
		{name: "docker prints <no value>", lookup: idB + " <no value>\n", wantRm: true},
		{name: "an empty label", lookup: idB + " \n", wantRm: true},
		{name: "no label field at all", lookup: idB + "\n", wantRm: true},
		{name: "<no value> with the line ended by CRLF", lookup: idB + " <no value>\r\n", wantRm: true},
		{name: "the label of another run", lookup: idB + " " + foreignRunID + "\n", wantErrs: []string{"label", foreignRunID}},
		{name: "the label of this run is the cleanup's", lookup: idB + " " + labRunID + "\n"},
	}
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				name := ctrNameOf("c2")
				c, ct, fake := startOnFake(t, shape,
					replyTo(fakecli.Reply{Stderr: "Bind for 127.0.0.1:18080 failed: port is already allocated", Exit: 125}, launchArgv(name, portGranted)...),
					replyTo(fakecli.Reply{Stdout: tt.lookup}, strayInspectArgv(name)...),
					replyTo(fakecli.Reply{}, "rm", "-f", idB))

				mustReachTheEnd(t, ct, func() {
					c.expectRefusal("campaign", "port-taken", "a launch with a taken port is refused", windowOf(name, portGranted), func() { _ = launchCall(name, "18080:80")() })
				})

				want := [][]string{launchArgv(name, portGranted), strayInspectArgv(name)}
				if tt.wantRm {
					want = append(want, []string{"rm", "-f", idB})
				}
				if got := invokedArgs(fake); !sameArgvs(got, want) {
					t.Errorf("engine calls = %q, want %q", got, want)
				}
				joined := strings.ToLower(strings.Join(ct.errs, "\n"))
				if len(tt.wantErrs) == 0 && len(ct.errs) != 0 {
					t.Errorf("reports = %q, want none", ct.errs)
				}
				for _, w := range tt.wantErrs {
					if !strings.Contains(joined, strings.ToLower(w)) {
						t.Errorf("reports = %q, want one that says %q", ct.errs, w)
					}
				}
			})
		}
	})
}

// podman made nothing: the name is absent, whatever the exact words of the engine.
func TestExpectRefusalFindsNothingToRemoveWhenTheEngineMadeNothing(t *testing.T) {
	for _, stderr := range []string{"Error: no such object: \"x\"", "Error: No such container: x", "Error: no such container x"} {
		forEachEngine(t, func(t *testing.T, shape engine.Shape) {
			name := ctrNameOf("c2")
			c, ct, fake := startOnFake(t, shape,
				replyTo(fakecli.Reply{Stderr: "address already in use", Exit: 126}, launchArgv(name, portGranted)...),
				replyTo(fakecli.Reply{Stderr: stderr, Exit: 1}, strayInspectArgv(name)...))

			mustReachTheEnd(t, ct, func() {
				c.expectRefusal("campaign", "port-taken", "a launch with a taken port is refused", windowOf(name, portGranted), func() { _ = launchCall(name, "18080:80")() })
			})

			if want := [][]string{launchArgv(name, portGranted), strayInspectArgv(name)}; !sameArgvs(invokedArgs(fake), want) || len(ct.errs) != 0 {
				t.Errorf("engine calls = %q, reports = %q, want the launch, the look and nothing else", invokedArgs(fake), ct.errs)
			}
		})
	}
}

// The cleanup reads the label of a candidate with the same template. A candidate with no
// label is reported as such, in words, and never touched.
func TestCleanupSaysNoLabelForWhatHasNone(t *testing.T) {
	for _, label := range []string{"<no value>\n", "\n", ""} {
		forEachEngine(t, func(t *testing.T, shape engine.Shape) {
			openLab(t)
			kinds := resourceKinds(labRunID)
			steps := []queuedStep{{argv: kinds[0].list, out: idB + "\n"}, {argv: kinds[0].inspect(idB), out: label}}
			for _, k := range kinds[1:] {
				steps = append(steps, queuedStep{argv: k.list})
			}
			q := queued(t, steps...)
			ct := &contractT{TB: t}
			c := newContract(ct, sessionOf(shape, labRunID))
			c.tried = true

			c.cleanupNow()

			if len(ct.errs) != 1 || !strings.Contains(ct.errs[0], "no label") || strings.Contains(ct.errs[0], "<no") {
				t.Errorf("reports = %q, want one that says the container has no label", ct.errs)
			}
			for _, dc := range q.calls {
				if len(dc.Args) > 0 && dc.Args[0] == "rm" {
					t.Errorf("the cleanup removed %q: a container with no label is not this run's", dc.Args)
				}
			}
		})
	}
}
