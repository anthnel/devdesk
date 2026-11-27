package docker

// OC-22 (DEVDESK-820, C2): production code has no force option and no prune of
// containers, images or volumes. The contracts still need both: they remove the
// containers of their own run even when running, and the prune window (expectPrune)
// exercises `<kind> prune -f` on a store that holds nothing but the run's resources.
// The two helpers below build those argv in test code only, which guard G3 and the
// OC-22 guard do not scan.

// forceRemoveContainerForTest runs `rm -f <id>` after the same identifier check as
// RemoveContainer.
func forceRemoveContainerForTest(id string) error {
	if err := requireContainerID("rm", id); err != nil {
		return err
	}
	return mutate(cmdLabel("rm"), "rm", "-f", id)
}

// forceRemoveImageForTest runs `rmi -f <id>` after the same check as RemoveImage.
func forceRemoveImageForTest(id string) error {
	if !IsImageID(id) {
		return ErrInvalidImageID
	}
	return mutate(cmdLabel("rmi"), "rmi", "-f", id)
}

// pruneKindForTest runs `<kind> prune -f` (kind is container, image or volume) and
// returns the engine's report.
func pruneKindForTest(kind string) (string, error) {
	return prune(cmdLabel(kind+" prune"), kind, "prune", "-f")
}
