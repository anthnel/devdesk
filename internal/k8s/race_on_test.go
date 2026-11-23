//go:build race

package k8s

// raceEnabled slows every timing bound: the race detector multiplies the
// cost of parsing YAML several times over.
const raceEnabled = true
