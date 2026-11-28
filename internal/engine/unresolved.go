package engine

// Unresolved builds the shape of the engine a preference NAMES, without looking
// anything up on PATH (OC-24, DEVDESK-822). The router installs it when Resolve
// fails, so the views say "Podman not found" to someone who pinned podman rather
// than naming whichever engine happened to be current before.
//
// The names follow Resolve: "docker" and "podman" name themselves, ""/"auto" is
// docker (the engine every caller assumes before anything is resolved), and
// anything else is a path whose engine engineOfPath reads. It changes nothing: the
// caller installs the shape with SetCurrent.
func Unresolved(preference string) Shape {
	switch preference {
	case "", Auto:
		return shapeFor(Docker, Docker)
	case Docker, Podman:
		return shapeFor(preference, preference)
	}
	return shapeFor(engineOfPath(preference), preference)
}
