package ociresources

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/engine"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// OC-22 (DEVDESK-820), the follow-ups of the review: the join that says no container
// uses an image, the bilan that outlives a list refresh, and the answers that arrive
// too late to open a confirmation.

// drainAll hands every message of a command back to Update, the list refreshes
// included (drive leaves them out so they do not replace the list a test drives); only
// the footer's timers are skipped. It is what the runtime does.
func drainAll(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for _, msg := range testutil.Msgs(cmd) {
		if _, timer := msg.(sharedcomponents.ClearFooterMsg); !timer {
			m = feed(t, m, msg)
		}
	}
	return m
}

func confirmAndRefresh(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := step(t, m, sharedcomponents.ConfirmModalYesMsg{})
	return drainAll(t, m, cmd)
}

// The bilan of P stays on screen through the list refresh that follows it, through
// the tick, and through the next refresh: only its own timer ends it. A failed load
// still replaces whatever the footer said.
func TestTheBilanOfAPruneSurvivesTheRefreshThatFollows(t *testing.T) {
	eachEngine(t, func(t *testing.T, shape engine.Shape) {
		replay := map[string]struct {
			model   func() Model
			exch    []fakecli.Exchange
			refresh tea.Msg
		}{
			"images": {
				model:   func() Model { return imagesModel(t, ociImages()...) },
				exch:    []fakecli.Exchange{exchange(fakecli.Reply{}, "rmi", imgDangle1), exchange(fakecli.Reply{}, "rmi", imgDangle2)},
				refresh: ImagesListMsg{Images: ociImages()},
			},
			"volumes": {
				model:   func() Model { return volumesModel(t, ociVolumes()...) },
				exch:    []fakecli.Exchange{exchange(fakecli.Reply{}, "volume", "rm", "scratch"), exchange(fakecli.Reply{}, "volume", "rm", "spare")},
				refresh: VolumesListMsg{Volumes: ociVolumes()},
			},
		}
		for name, tt := range replay {
			t.Run(name, func(t *testing.T) {
				engineWith(t, shape, usersOf(shape, ociContainers()), tt.exch...)
				m := confirmAndRefresh(t, previewP(t, tt.model()))
				want := m.footer.Text()
				if !strings.Contains(want, "2 removed") {
					t.Fatalf("footer = %q, want the bilan", want)
				}

				m = feed(t, m, tt.refresh, tea.Msg(RefreshTickMsg{}), tt.refresh)

				if m.footer.Text() != want {
					t.Errorf("footer = %q after the refresh, want the bilan %q to stay", m.footer.Text(), want)
				}
				bad := feed(t, m, ImagesListMsg{Err: errors.New("daemon down")})
				if name == "images" && bad.footer.Level() != sharedcomponents.LevelError {
					t.Errorf("a failed load left %q at %v, want its Error to replace the bilan", bad.footer.Text(), bad.footer.Level())
				}
			})
		}
	})
}

// D follows the same rule as P: the bilan of a removal that was already gone stays
// through the refresh.
func TestTheBilanOfADeleteSurvivesTheRefreshThatFollows(t *testing.T) {
	eachEngine(t, func(t *testing.T, shape engine.Shape) {
		engineWith(t, shape, fakeUsers{}, anyOf(goneImage(shape, "x"), "rmi"))
		m := imagesModel(t, ociImages()...)
		m, _ = step(t, m, testutil.Key(keymap.Delete))
		m = confirmAndRefresh(t, m)
		if !strings.Contains(m.footer.Text(), "already gone") {
			t.Fatalf("footer = %q, want the bilan", m.footer.Text())
		}

		m = feed(t, m, ImagesListMsg{Images: ociImages()})

		if !strings.Contains(m.footer.Text(), "already gone") {
			t.Errorf("footer = %q after the refresh, want the bilan to stay", m.footer.Text())
		}
	})
}

// The join by image ID: a dangling image a container was created from is not on the
// list, whatever the count of the image list said (every dangling image shares
// `<none>:<none>`, and podman always counts 0).
func TestAnImageAContainerWasCreatedFromIsNotOnTheList(t *testing.T) {
	eachEngine(t, func(t *testing.T, shape engine.Shape) {
		used := exchange(fakecli.Reply{Stdout: "web\nworker\n"}, "ps", "-a", "--filter", "ancestor="+imgDangle1, "--format", "{{.Names}}")
		fake := engineWith(t, shape, fakeUsers{}, used, exchange(fakecli.Reply{}, "rmi", imgDangle2))
		images := []docker.Image{
			{ID: imgDangle1, Repository: "<none>", Tag: "<none>"}, // count 0, used all the same
			{ID: imgDangle2, Repository: "<none>", Tag: "<none>"},
		}
		m := imagesModel(t, images...)

		m = confirmYes(t, previewP(t, m))

		wantArgv(t, fake, []string{"rmi"}, imgDangle2)
		footerIs(t, m, sharedcomponents.LevelInfo, "1 removed")
	})
}

// Fail closed: when the engine cannot say which containers use the images, nothing is
// unused, and nothing is offered.
func TestPruneImagesRemovesNothingWhenTheUsersCannotBeRead(t *testing.T) {
	eachEngine(t, func(t *testing.T, shape engine.Shape) {
		broken := exchange(fakecli.Reply{Stderr: "Cannot connect to the Docker daemon\n", Exit: 1},
			"ps", "-a", "--filter", "ancestor="+imgDangle2, "--format", "{{.Names}}")
		fake := engineWith(t, shape, fakeUsers{}, broken)
		m := imagesModel(t, ociImages()...)

		m, cmd := step(t, m, testutil.Key(keymap.Prune))
		m = drive(t, m, cmd)

		if m.confirmModal != nil {
			t.Error("P opened a confirmation without knowing which images are used")
		}
		footerIs(t, m, sharedcomponents.LevelError)
		if m.pruning != "" {
			t.Errorf("pruning = %q after the answer, want it cleared", m.pruning)
		}
		if calls := destructive(fake); len(calls) != 0 {
			t.Errorf("ran %q without knowing which images are used", calls)
		}
	})
}

// The race: the engine's answer to a preview arrives after the user opened another
// confirmation, or left the tab. It is dropped: no confirmation replaces the one on
// screen, none opens on another tab, and the frozen list is not touched.
func TestALateAnswerToAPreviewOpensNothing(t *testing.T) {
	answer := map[string]tea.Msg{
		"images":  ImagePruneUsersMsg{Candidates: []string{imgDangle1}},
		"volumes": VolumePruneUsersMsg{Volumes: ociVolumes()},
	}
	start := map[string]func(*testing.T) Model{
		"images":  func(t *testing.T) Model { return imagesModel(t, ociImages()...) },
		"volumes": func(t *testing.T) Model { return volumesModel(t, ociVolumes()...) },
	}
	for _, kind := range []string{"images", "volumes"} {
		t.Run(kind+": another confirmation is open", func(t *testing.T) {
			m, _ := step(t, start[kind](t), testutil.Key(keymap.Prune))
			m, _ = step(t, m, testutil.Key(keymap.Delete)) // D opens its own confirmation
			if m.confirmModal == nil {
				t.Fatal("D opened no confirmation")
			}
			action := m.pendingAction

			m = feed(t, m, answer[kind])

			if m.confirmModal == nil || m.pendingAction != action {
				t.Errorf("the late answer replaced the confirmation: pendingAction = %q, want %q", m.pendingAction, action)
			}
			if len(m.pruneTargets) != 0 {
				t.Errorf("pruneTargets = %v, want none from a dropped answer", m.pruneTargets)
			}
			footerIs(t, m, sharedcomponents.LevelInfo, "cancelled")
			if m.pruning != "" {
				t.Errorf("pruning = %q, want it cleared", m.pruning)
			}
		})
		t.Run(kind+": the tab changed", func(t *testing.T) {
			m, _ := step(t, start[kind](t), testutil.Key(keymap.Prune))
			m = feed(t, m, testutil.Key("tab"))

			m = feed(t, m, answer[kind])

			if m.confirmModal != nil {
				t.Error("a late answer opened a confirmation on another tab")
			}
			footerIs(t, m, sharedcomponents.LevelInfo, "cancelled")
		})
	}
}

// A second P while one is being read, or removed, is refused with the reason, and
// starts nothing.
func TestAPruneIsRefusedWhileAnotherRuns(t *testing.T) {
	for _, tab := range []string{"images", "volumes"} {
		t.Run(tab, func(t *testing.T) {
			var m Model
			if tab == "images" {
				m = imagesModel(t, ociImages()...)
			} else {
				m = volumesModel(t, ociVolumes()...)
			}
			m.pruning = "networks"

			m, cmd := step(t, m, testutil.Key(keymap.Prune))

			if m.confirmModal != nil || m.pruning != "networks" {
				t.Errorf("P while another prune runs: modal = %v, pruning = %q", m.confirmModal, m.pruning)
			}
			footerIs(t, m, sharedcomponents.LevelWarning)
			for _, msg := range testutil.Msgs(cmd) {
				if _, timer := msg.(sharedcomponents.ClearFooterMsg); !timer {
					t.Errorf("P while another prune runs started %T", msg)
				}
			}
		})
	}
}

// The read of "which containers use these images" costs one call per image, so it has
// a deadline for the whole loop: past it, an Error and no confirmation (M2).
func TestPruneImagesStopsWhenTheReadPassesItsDeadline(t *testing.T) {
	previous := imageUsersDeadline
	imageUsersDeadline = -time.Second // already expired when the read starts
	t.Cleanup(func() { imageUsersDeadline = previous })
	eachEngine(t, func(t *testing.T, shape engine.Shape) {
		fake := engineWith(t, shape, fakeUsers{})
		m := imagesModel(t, ociImages()...)

		m, cmd := step(t, m, testutil.Key(keymap.Prune))
		m = drive(t, m, cmd)

		if m.confirmModal != nil {
			t.Error("P opened a confirmation although the read did not finish")
		}
		footerIs(t, m, sharedcomponents.LevelError)
		if m.pruning != "" {
			t.Errorf("pruning = %q after the answer, want it cleared", m.pruning)
		}
		for _, inv := range fake.Invocations() {
			if slices.Contains(inv.Args, "--filter") {
				t.Errorf("the engine was asked after the deadline: %q", inv.Args)
			}
		}
		if calls := destructive(fake); len(calls) != 0 {
			t.Errorf("ran %q after a read that did not finish", calls)
		}
	})
}

// An answer the router marks abandoned (the user is in another view) ends the wait
// and opens nothing, whatever it says.
func TestAnAbandonedAnswerEndsTheWaitAndOpensNothing(t *testing.T) {
	answers := map[string]tea.Msg{
		"images":  ImagePruneUsersMsg{Candidates: []string{imgDangle1}, Abandoned: true},
		"volumes": VolumePruneUsersMsg{Volumes: ociVolumes(), Abandoned: true},
	}
	for kind, answer := range answers {
		t.Run(kind, func(t *testing.T) {
			var m Model
			if kind == "images" {
				m = imagesModel(t, ociImages()...)
			} else {
				m = volumesModel(t, ociVolumes()...)
			}
			m, _ = step(t, m, testutil.Key(keymap.Prune))
			if m.pruning != kind {
				t.Fatalf("setup: pruning = %q, want %q", m.pruning, kind)
			}

			m = feed(t, m, answer)

			if m.confirmModal != nil || len(m.pruneTargets) != 0 {
				t.Error("an abandoned answer opened a confirmation")
			}
			if m.pruning != "" {
				t.Errorf("pruning = %q, want the wait ended", m.pruning)
			}
		})
	}
}
