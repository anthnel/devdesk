package docker

import (
	"errors"
	"fmt"
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
func ClassifyRemoval(err error) (RemovalStatus, RefusalReason) {
	if err == nil {
		return RemovalRemoved, ""
	}
	if invalidTarget(err) {
		return RemovalRefused, ReasonOther
	}
	text := strings.ToLower(err.Error())
	// The engine itself is missing or unreachable: nothing was looked up, so
	// whatever "not found" the text holds says nothing about the target.
	if containsAny(text, notReachableMarkers) {
		return RemovalRefused, ReasonOther
	}
	if containsAny(text, runningMarkers) {
		return RemovalRefused, ReasonRunning
	}
	if containsAny(text, inUseMarkers) {
		return RemovalRefused, ReasonInUse
	}
	if containsAny(text, goneMarkers) || dockerNetworkGone(text) {
		return RemovalGone, ""
	}
	return RemovalRefused, ReasonOther
}

// dockerNetworkGone reads docker's `network <name> not found`, whose middle word is
// the name asked for and so cannot be a fixed marker. The text has to end there:
// "not found" elsewhere (a path, a binary) is not this.
func dockerNetworkGone(text string) bool {
	text = strings.TrimSpace(text)
	return strings.Contains(text, "network ") && strings.HasSuffix(text, " not found")
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

// goneMarkers are the wordings for something that does not exist. NOT RECORDED for
// rm, rmi, volume rm and network rm (only `inspect` of an absent container is, as
// "no such object"): they are the engines' known texts, to be recorded in an OC-09
// scenario. A wording that changes with an upgrade falls to ReasonOther, the safe side.
var goneMarkers = []string{
	"no such container",
	"no such image",
	"no such volume",
	"no such network",
	"no such object",
	"no container with name or id",
	"no volume with name",
	"image not known",
	"network not found",
	"no such pod",
}
