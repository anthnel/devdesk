// Package nexusfixture holds the recorded answers of a real Nexus Repository
// (the NX-03 measures and what the recorder regenerates), as one text file per
// exchange: its format, its parser and writer, and the embedded loader.
//
// The package carries no test helper and never imports "testing": the
// recorder of the nexus-bench dev tool imports it. The HTTP server that
// replays the fixtures lives in a sub-package.
package nexusfixture
