package config

// The container engine names. They are stated here as well as in
// internal/engine, and TestEveryConfiguredEngineHasAShape keeps the two in
// step — the same arrangement the forge names have (§3.6) and the registry
// `provider` names have (§3.8). config must not import a backend, and a
// backend must not be the authority on what a config file may say, so a test
// is the only thing that can hold them together.
const (
	// EngineAuto is the default: docker if it is on PATH, else podman. It
	// spares anyone who installed exactly one of the two a setting they have
	// no opinion about — app.secret_backend's precedent (§3.9).
	EngineAuto   = "auto"
	EngineDocker = "docker"
	EnginePodman = "podman"
)

// ContainerEngines lists the values app.container_engine names, in the order
// the configuration view cycles them.
//
// It is not the whole set the setting accepts: an explicit path to a binary is
// also honoured (internal/engine.Resolve), and is deliberately not cycled —
// there is nothing to cycle through. The same compromise scan.trivy_path makes.
func ContainerEngines() []string { return []string{EngineAuto, EngineDocker, EnginePodman} }
