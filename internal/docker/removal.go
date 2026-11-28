package docker

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// OC-22 (DEVDESK-820, S2/C2): what DevDesk destroys is what the user previewed,
// one identifier at a time, never forced. A batch is one bare argv per target
// (`rm <id>`, `rmi <id>`, `volume rm <name>`), in the order given; the engine's
// refusal of one target does not stop the others and is never answered with a
// force option. The outcome is a RemovalReport the views turn into one footer line.

// RemovalStatus is what happened to one target of a batch.
type RemovalStatus string

const (
	// RemovalRemoved: the engine removed it.
	RemovalRemoved RemovalStatus = "removed"
	// RemovalGone: it did not exist when its turn came (somebody removed it). An
	// information, not a failure.
	RemovalGone RemovalStatus = "gone"
	// RemovalRefused: it still exists. The engine refused, or the target was not a
	// valid identifier and nothing was sent.
	RemovalRefused RemovalStatus = "refused"
)

// RefusalReason says why a target was refused.
type RefusalReason string

const (
	// ReasonRunning: a container that is running (or paused) cannot be removed.
	ReasonRunning RefusalReason = "running"
	// ReasonInUse: an image, a volume or a network that a container uses.
	ReasonInUse RefusalReason = "in use"
	// ReasonOther: any refusal whose text is not one DevDesk knows. It is the safe
	// reading of anything uncertain: the item is reported as not removed.
	ReasonOther RefusalReason = "other"
)

// RemovalResult is the outcome for one target.
type RemovalResult struct {
	Target string // the identifier as the caller passed it
	Status RemovalStatus
	Reason RefusalReason // set when Status is RemovalRefused
	Err    error         // the engine's error, nil when Status is RemovalRemoved
}

// RemovalReport has one result per target, in the order of the targets.
type RemovalReport []RemovalResult

// Count returns how many targets ended in status s.
func (r RemovalReport) Count(s RemovalStatus) int {
	n := 0
	for _, res := range r {
		if res.Status == s {
			n++
		}
	}
	return n
}

// CountRefused returns how many targets were refused for reason.
func (r RemovalReport) CountRefused(reason RefusalReason) int {
	n := 0
	for _, res := range r {
		if res.Status == RemovalRefused && res.Reason == reason {
			n++
		}
	}
	return n
}

// Summary is the one-line outcome the footer shows: `2 removed, 1 not removed
// (running), 1 already gone`. A part with a zero count is left out.
func (r RemovalReport) Summary() string {
	var parts []string
	if n := r.Count(RemovalRemoved); n > 0 {
		parts = append(parts, fmt.Sprintf("%d removed", n))
	}
	if n := r.Count(RemovalRefused); n > 0 {
		var why []string
		if r.CountRefused(ReasonRunning) > 0 {
			why = append(why, string(ReasonRunning))
		}
		if r.CountRefused(ReasonInUse) > 0 {
			why = append(why, string(ReasonInUse))
		}
		if r.CountRefused(ReasonOther) > 0 {
			why = append(why, "see logs")
		}
		parts = append(parts, fmt.Sprintf("%d not removed (%s)", n, strings.Join(why, ", ")))
	}
	if n := r.Count(RemovalGone); n > 0 {
		parts = append(parts, fmt.Sprintf("%d already gone", n))
	}
	if len(parts) == 0 {
		return "Nothing removed"
	}
	return strings.Join(parts, ", ")
}

// SingleRemoval reads the error of one Remove* call the way a batch reads its own, so
// a view can report `D` on one item with the same words as `P` on many.
func SingleRemoval(target string, err error) RemovalReport {
	status, reason := ClassifyRemoval(err)
	return RemovalReport{{Target: target, Status: status, Reason: reason, Err: err}}
}

// RemoveContainers removes each container by its ID, one `rm <id>` per target, in
// order. A malformed ID is refused without building anything, and the batch goes on.
func RemoveContainers(ids []string) RemovalReport { return removeBatch(ids, RemoveContainer) }

// RemoveImages removes each image by its ID, one `rmi <id>` per target.
func RemoveImages(ids []string) RemovalReport { return removeBatch(ids, RemoveImage) }

// RemoveVolumes removes each volume by its name, one `volume rm <name>` per target.
func RemoveVolumes(names []string) RemovalReport { return removeBatch(names, RemoveVolume) }

func removeBatch(targets []string, remove func(string) error) RemovalReport {
	report := make(RemovalReport, 0, len(targets))
	for _, target := range targets {
		err := remove(target)
		report = append(report, SingleRemoval(target, err)...)
	}
	return report
}

// ClassifyRemoval reads what a Remove* function returned. nil is RemovalRemoved. An
// engine that says the thing does not exist is RemovalGone. Everything else is
// RemovalRefused, with the reason when the text is one DevDesk knows and ReasonOther
// when it is not: "could not tell" is never read as "already gone".
//
// Only the engine's own words are read (engineText), never DevDesk's label.
func ClassifyRemoval(err error) (RemovalStatus, RefusalReason) {
	if err == nil {
		return RemovalRemoved, ""
	}
	if invalidTarget(err) {
		return RemovalRefused, ReasonOther
	}
	// An engine that could not be asked looked nothing up (OC-24): whatever its
	// cause holds says nothing about the target. The second test is a belt for an
	// error that is still the engine's text (an *engineError): it is never "gone".
	var unavailable *UnavailableError
	if errors.As(err, &unavailable) || ClassifyUnavailable(err) != UnavailabilityUnknown {
		return RemovalRefused, ReasonOther
	}
	label, text := engineText(err)
	lower := strings.ToLower(text)
	// The engine itself is missing or unreachable: nothing was looked up, so
	// whatever "not found" the text holds says nothing about the target.
	if containsAny(lower, notReachableMarkers) {
		return RemovalRefused, ReasonOther
	}
	if containsAny(lower, runningMarkers) {
		return RemovalRefused, ReasonRunning
	}
	if containsAny(lower, inUseMarkers) {
		return RemovalRefused, ReasonInUse
	}
	if containsAny(lower, goneMarkers) || dockerNetworkGone(label, text) {
		return RemovalGone, ""
	}
	return RemovalRefused, ReasonOther
}

// engineText splits an error of a Remove* call into DevDesk's label and the engine's
// text. A mutation error carries them apart; any other error that reads `<label>
// failed: <text>` is split on that phrase; anything else is all engine text with no
// label.
func engineText(err error) (label, text string) {
	var ee *engineError
	if errors.As(err, &ee) {
		return ee.Label, ee.Output
	}
	full := err.Error()
	if i := strings.Index(full, " failed: "); i >= 0 {
		return full[:i], full[i+len(" failed: "):]
	}
	return "", full
}

// dockerNetworkGoneRE is docker's answer to `network rm` of a network that does not
// exist, probed on docker 28.5.2 and 29.10.1.
var dockerNetworkGoneRE = regexp.MustCompile(`^Error response from daemon: network [A-Za-z0-9_.-]+ not found$`)

// dockerNetworkGone reads docker's `network <name> not found`, whose middle word is
// the name asked for and so cannot be a fixed marker. It applies to `network rm`
// only, and to nothing but the whole text of the engine: a "not found" anywhere
// else (a path, a binary) is not this.
func dockerNetworkGone(label, text string) bool {
	return strings.HasSuffix(label, "network rm") && dockerNetworkGoneRE.MatchString(strings.TrimSpace(text))
}

func invalidTarget(err error) bool {
	for _, sentinel := range []error{ErrInvalidContainerID, ErrInvalidImageID, ErrInvalidVolumeName, ErrInvalidNetworkID} {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	return false
}

func containsAny(text string, markers []string) bool {
	for _, m := range markers {
		if strings.Contains(text, m) {
			return true
		}
	}
	return false
}

// notReachableMarkers: the engine could not be asked. Checked before anything else,
// because "executable file not found" and "cannot connect" carry words of the others.
var notReachableMarkers = []string{
	"executable file not found",
	"cannot connect to",
	"permission denied",
	"is the docker daemon running",
	"connection refused",
	"no such file or directory",
}

// runningMarkers are the engines' wordings for a container that is running, recorded
// (containers/rm-running, docker 28 and 29, podman 5 and 6) and older ones.
var runningMarkers = []string{
	"container is running",
	"as it is running",
	"cannot remove a running container",
	"container state improper",
}

// inUseMarkers are the wordings for something a container uses: images/rmi-used,
// volumes/rm-used and networks/rm-used, recorded on the same four configurations.
var inUseMarkers = []string{
	"image is being used",
	"image is in use",
	"image used by",
	"volume is in use",
	"volume is being used",
	"has active endpoints",
	"network is being used",
	"has associated containers",
}

// goneMarkers are the wordings for something that does not exist, probed on docker
// 29.10.1 and 28.5.2 and podman 6.2.1 and 5.8.11 rootless (2026-11): docker `No such
// container: X`, `No such image: X`, `get X: no such volume`; podman `no container
// with ID or name "X" found: no such container`, `X: image not known`, `no volume
// with name "X" found: no such volume`. NOT a versioned fixture (only `inspect` of an
// absent container is recorded, as "no such object"): to be recorded in an OC-09
// scenario (DEVDESK-949). A wording that changes with an upgrade falls to ReasonOther,
// the safe side.
var goneMarkers = []string{
	"no such container",
	"no such image",
	"no such volume",
	"no such network",
	"no such object",
	"no container with id or name",
	"no container with name or id",
	"no volume with name",
	"image not known",
	"network not found", // podman: unable to find network with name or ID X: network not found
	"no such pod",
}

// StopAndRemoveContainer stops the container, then removes the same ID: `stop <id>`,
// then `rm <id>`, never forced (OC-25, DEVDESK-823). A stop that fails removes nothing.
//
// It is StopContainer then RemoveContainer: the identifier is validated by the first,
// `stop <id>` and `rm <id>` are written there and nowhere else, and the engine's
// grace delay applies, as for `K`. A stop that fails ends it: the error is read like a
// removal's, so a container that is already gone is RemovalGone and one nobody could
// ask is a refusal. A `rm` the engine refuses (the container started again between
// the two) is a refusal too, read the same way: nothing is retried and nothing forced.
//
// It BLOCKS: `stop` waits for the engine's grace delay (10 s by default, or the
// container's own StopTimeout) before it kills, so a caller runs it in a tea.Cmd, never
// from Update or View, and answers with the report as a message.
func StopAndRemoveContainer(id string) RemovalReport {
	if err := StopContainer(id); err != nil {
		return SingleRemoval(id, err)
	}
	return SingleRemoval(id, RemoveContainer(id))
}
