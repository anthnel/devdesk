package enginefixture_test

import (
	"bytes"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// Criterion 1, the file half: a fixture file holds one exchange, and reading it
// then writing it gives the same bytes back.

// Every synthetic file survives Parse then Format byte for byte. This is the
// round trip of the plan (Format(Parse(b)) == b); FuzzParse explores it.
func TestSyntheticFixturesRoundTrip(t *testing.T) {
	for _, name := range syntheticNames(t) {
		t.Run(name, func(t *testing.T) {
			data := syntheticFile(t, name)
			f := parseSynthetic(t, name)

			if got := enginefixture.Format(f); !bytes.Equal(got, data) {
				t.Errorf("Format(Parse(file)) differs from the file\n got: %q\nwant: %q", got, data)
			}
			if f.Meta.RecordedBy != "synthetic" {
				t.Errorf("RecordedBy = %q, want synthetic (no real fixture before OC-09b)", f.Meta.RecordedBy)
			}
			if f.Campaign != "synthetic" || f.Name != strings.TrimSuffix(name, ".cli") {
				t.Errorf("Campaign/Name = %q/%q, want synthetic/%s", f.Campaign, f.Name, strings.TrimSuffix(name, ".cli"))
			}
			if want := strings.SplitN(name, "--", 2)[0]; f.Scenario != want {
				t.Errorf("Scenario = %q, want the part of the name before \"--\", %q", f.Scenario, want)
			}
		})
	}
}

// What Parse reads, field by field, for each synthetic file.
func TestParseReadsTheSyntheticFixtures(t *testing.T) {
	psArgv := []string{"ps", "--all", "--format",
		"{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.State}}\t{{.Status}}\t{{.CreatedAt}}\t{{.Ports}}"}

	tests := []struct {
		file     string
		tool     enginefixture.Tool
		version  string
		mode     enginefixture.Mode
		meta     string // the "# scenario:" header
		argv     []string
		exit     int
		stdout   string
		stderr   string // exact; "" with stderrIn set means "not compared exactly"
		stderrIn string // a substring the stderr must hold
	}{
		{
			file: fileRmDocker, tool: enginefixture.Docker, version: "29.10.1", mode: enginefixture.Rootful,
			meta: "rm of a running container is refused",
			argv: []string{"rm", "ddm-run"}, exit: 1, stdout: "",
			stderrIn: "container is running",
		},
		{
			file: fileRmPodman, tool: enginefixture.Podman, version: "6.2.1", mode: enginefixture.Rootless,
			meta: "rm of a running container is refused",
			argv: []string{"rm", "ddm-run"}, exit: 2, stdout: "",
			stderrIn: "container state improper",
		},
		{
			file: fileInspectAbsent, tool: enginefixture.Podman, version: "5.8.11", mode: enginefixture.Rootful,
			meta: "inspect of an absent container fails on stderr",
			argv: []string{"inspect", "3f9d2c7a41be"}, exit: 125, stdout: "[]\n",
			stderr: "Error: no such object: \"3f9d2c7a41be\"\n",
		},
		{
			// The case the plan names: stdout and stderr both written, exit 125.
			file: filePortTaken, tool: enginefixture.Docker, version: "28.5.2", mode: enginefixture.Rootful,
			meta: "run with a published port that is taken",
			argv: []string{"run", "-d", "--name", "ddm-run2", "-p", "18080:80", "alpine:3.20", "sleep", "5"},
			exit: 125, stdout: "3f9d2c7a41be5e8f0a1b2c3d4e5f60718293a4b5c6d7e8f9012345678abcdef0\n",
			stderrIn: "port is already allocated",
		},
		{
			// Not text: stored as base64, read back as the three raw bytes.
			file: fileBinary, tool: enginefixture.Docker, version: "29.10.1", mode: enginefixture.Rootful,
			meta: "stdout that is not text",
			argv: []string{"export", "ddm-run"}, exit: 0, stdout: "\xff\x00\r",
			stderr: "warning: the output is a binary stream\n",
		},
		{
			file: fileNoNewline, tool: enginefixture.Podman, version: "6.2.1", mode: enginefixture.Rootful,
			meta: "stdout without a final newline",
			argv: []string{"version", "--format", "{{.Client.Version}}"}, exit: 0, stdout: "6.2.1",
			stderr: "",
		},
		{
			file: filePS, tool: enginefixture.Docker, version: "29.10.1", mode: enginefixture.Rootful,
			meta: "ps --all with the template DevDesk builds (follows engine.Templates.PS)",
			argv: psArgv, exit: 0, stdout: "", stderr: "",
			stderrIn: "", // stdout is checked below, by shape
		},
		{
			file: fileInspectThere, tool: enginefixture.Docker, version: "29.10.1", mode: enginefixture.Rootful,
			meta: "inspect of a container that exists",
			argv: []string{"inspect", "3f9d2c7a41be"}, exit: 0, stdout: "",
			stderr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			f := parseSynthetic(t, tt.file)

			if f.Meta.Tool != tt.tool || f.Meta.Version != tt.version || f.Meta.Mode != tt.mode {
				t.Errorf("Meta tool/version/mode = %s/%s/%s, want %s/%s/%s",
					f.Meta.Tool, f.Meta.Version, f.Meta.Mode, tt.tool, tt.version, tt.mode)
			}
			if f.Meta.Scenario != tt.meta || f.Meta.Recorded != "2026-11-27" || f.Meta.Redacted != "none" || f.Meta.Record != "" {
				t.Errorf("Meta = %+v, want scenario %q, recorded 2026-11-27, redacted none, record empty", f.Meta, tt.meta)
			}
			// Element by element: a joined string would hide a split argument.
			if !slices.Equal(f.Exchange.Argv, tt.argv) {
				t.Errorf("Argv = %q, want %q", f.Exchange.Argv, tt.argv)
			}
			if f.Exchange.Exit != tt.exit {
				t.Errorf("Exit = %d, want %d", f.Exchange.Exit, tt.exit)
			}
			switch tt.file {
			case filePS:
				// Three rows, each ending in a newline, tab separated.
				if n := bytes.Count(f.Exchange.Stdout, []byte("\n")); n != 3 {
					t.Errorf("ps stdout holds %d lines, want 3", n)
				}
				if n := bytes.Count(f.Exchange.Stdout, []byte("\t")); n != 18 {
					t.Errorf("ps stdout holds %d tabs, want 18 (6 per row): tabs must survive", n)
				}
			case fileInspectThere:
				if !bytes.HasPrefix(f.Exchange.Stdout, []byte("[\n")) || !bytes.HasSuffix(f.Exchange.Stdout, []byte("]\n")) {
					t.Errorf("inspect stdout = %q, want a JSON array ending in a newline", f.Exchange.Stdout)
				}
			default:
				if string(f.Exchange.Stdout) != tt.stdout {
					t.Errorf("Stdout = %q, want %q", f.Exchange.Stdout, tt.stdout)
				}
			}
			if tt.stderrIn != "" {
				if !strings.Contains(string(f.Exchange.Stderr), tt.stderrIn) {
					t.Errorf("Stderr = %q, want it to contain %q", f.Exchange.Stderr, tt.stderrIn)
				}
			} else if string(f.Exchange.Stderr) != tt.stderr {
				t.Errorf("Stderr = %q, want %q", f.Exchange.Stderr, tt.stderr)
			}
		})
	}
}

// The base64 stream decodes to exactly the bytes ff 00 0d, and the file with no
// final newline keeps its stdout without one.
func TestExactBytesSurviveTheFile(t *testing.T) {
	bin := parseSynthetic(t, fileBinary)
	if !bytes.Equal(bin.Exchange.Stdout, []byte{0xff, 0x00, '\r'}) {
		t.Errorf("binary stdout = % x, want ff 00 0d", bin.Exchange.Stdout)
	}
	plain := parseSynthetic(t, fileNoNewline)
	if len(plain.Exchange.Stdout) == 0 || bytes.HasSuffix(plain.Exchange.Stdout, []byte("\n")) {
		t.Errorf("no-final-newline stdout = %q, want text that does not end in a newline", plain.Exchange.Stdout)
	}
}

// ---- Parse refuses what it could not write back. ----

// cliText builds a whole file (headers of an rm of a running container under
// docker 29.10.1 rootful) around the given exit line and streams, so a test
// changes one thing at a time. The stream headers and bodies are given as they
// are written, which lets a case lie about a length or an encoding.
func cliText(exit, stdoutHeader, stdoutBody, stderrHeader, stderrBody string) string {
	return "# tool: docker\n# version: 29.10.1\n# mode: rootful\n# recorded: 2026-11-27\n" +
		"# scenario: a test\n# redacted: none\n# recorded-by: synthetic\n" +
		"# argv: [\"rm\",\"ddm-run\"]\n# exit: " + exit + "\n" +
		"# stdout: " + stdoutHeader + "\n# stderr: " + stderrHeader + "\n\n" +
		"--- stdout\n" + stdoutBody + "\n--- stderr\n" + stderrBody + "\n"
}

// goodText is a valid file: stdout empty, stderr "err\n".
func goodText() string { return cliText("1", "0 bytes, text", "", "4 bytes, text", "err\n") }

const goodName = "rm-running--docker-29.10.1-rootful.cli"

// pendingText is a valid pending file: it stops after argv and the blank line.
const pendingText = "# tool: podman\n# version: 6.2.1\n# mode: rootless\n# recorded: 2026-11-27\n" +
	"# scenario: a test\n# redacted: none\n# record: pending\n# argv: [\"rm\",\"ddm-run\"]\n\n"

const pendingName = "rm-running--podman-6.2.1-rootless.cli"

// replaceOnce edits a valid text and fails when the edit did not apply, so a
// case cannot pass by changing nothing.
func replaceOnce(t *testing.T, text, old, repl string) string {
	t.Helper()
	if !strings.Contains(text, old) {
		t.Fatalf("test bug: %q is not in the text", old)
	}
	return strings.Replace(text, old, repl, 1)
}

func TestParseRefusesWhatItCouldNotWriteBack(t *testing.T) {
	// Sanity: the base text of the cases is itself valid and canonical.
	f, err := enginefixture.Parse("c", goodName, []byte(goodText()))
	if err != nil {
		t.Fatalf("the base text of the cases is refused: %v", err)
	}
	if string(enginefixture.Format(f)) != goodText() {
		t.Fatalf("test bug: the base text is not canonical:\n got: %q\nwant: %q", enginefixture.Format(f), goodText())
	}

	good := goodText()
	tests := []struct {
		name     string
		file     string
		text     string
		contains []string // the offending value or header name, quoted back to the reader
	}{
		// ---- line endings and layout ----
		{name: "a CR before a line feed", file: goodName,
			text: replaceOnce(t, good, "# exit: 1\n", "# exit: 1\r\n")},
		{name: "a lone CR in a stream", file: goodName,
			text: cliText("1", "3 bytes, text", "a\rb", "0 bytes, text", "")},
		{name: "no blank line between the headers and the body", file: goodName,
			text: replaceOnce(t, good, "\n\n--- stdout", "\n--- stdout")},
		{name: "headers swapped: the order is canonical", file: goodName,
			text: replaceOnce(t, good, "# version: 29.10.1\n# mode: rootful\n", "# mode: rootful\n# version: 29.10.1\n")},
		{name: "no space after the colon", file: goodName,
			text: replaceOnce(t, good, "# version: 29.10.1", "# version:29.10.1")},
		{name: "two spaces after the colon", file: goodName,
			text: replaceOnce(t, good, "# version: 29.10.1", "# version:  29.10.1")},
		{name: "trailing space in a value", file: goodName,
			text: replaceOnce(t, good, "# mode: rootful", "# mode: rootful ")},

		// ---- headers ----
		{name: "an unknown header", file: goodName, contains: []string{"bogus"},
			text: replaceOnce(t, good, "# exit: 1\n", "# bogus: 1\n# exit: 1\n")},
		{name: "a duplicate header", file: goodName, contains: []string{"tool"},
			text: replaceOnce(t, good, "# tool: docker\n", "# tool: docker\n# tool: docker\n")},
		{name: "tool kind", file: "rm-running--kind-29.10.1-rootful.cli", contains: []string{"kind"},
			text: replaceOnce(t, good, "# tool: docker", "# tool: kind")},
		{name: "tool in the wrong case", file: "rm-running--Docker-29.10.1-rootful.cli", contains: []string{"Docker"},
			text: replaceOnce(t, good, "# tool: docker", "# tool: Docker")},
		{name: "mode sideways", file: "rm-running--docker-29.10.1-sideways.cli", contains: []string{"sideways"},
			text: replaceOnce(t, good, "# mode: rootful", "# mode: sideways")},
		{name: "a version with a v", file: "rm-running--docker-v29.10.1-rootful.cli", contains: []string{"v29.10.1"},
			text: replaceOnce(t, good, "# version: 29.10.1", "# version: v29.10.1")},
		{name: "a version with an empty part", file: "rm-running--docker-29..1-rootful.cli", contains: []string{"29..1"},
			text: replaceOnce(t, good, "# version: 29.10.1", "# version: 29..1")},
		{name: "a version with one number", file: "rm-running--docker-29-rootful.cli", contains: []string{"29"},
			text: replaceOnce(t, good, "# version: 29.10.1", "# version: 29")},
		{name: "a date in the wrong order", file: goodName, contains: []string{"27/11/2026"},
			text: replaceOnce(t, good, "# recorded: 2026-11-27", "# recorded: 27/11/2026")},
		{name: "a date that does not exist", file: goodName, contains: []string{"2026-02-30"},
			text: replaceOnce(t, good, "# recorded: 2026-11-27", "# recorded: 2026-02-30")},
		{name: "an unknown record value", file: goodName, contains: []string{"maybe"},
			text: replaceOnce(t, good, "# recorded-by: synthetic", "# record: maybe\n# recorded-by: synthetic")},
		{name: "skip without a reason", file: goodName,
			text: replaceOnce(t, good, "# recorded-by: synthetic", "# record: skip:\n# recorded-by: synthetic")},
		{name: "an unknown recorded-by", file: goodName, contains: []string{"somebody"},
			text: replaceOnce(t, good, "# recorded-by: synthetic", "# recorded-by: somebody")},

		// ---- argv ----
		{name: "argv not JSON", file: goodName,
			text: replaceOnce(t, good, `# argv: ["rm","ddm-run"]`, "# argv: rm ddm-run")},
		{name: "argv empty", file: goodName,
			text: replaceOnce(t, good, `# argv: ["rm","ddm-run"]`, "# argv: []")},
		{name: "argv holds a number", file: goodName,
			text: replaceOnce(t, good, `# argv: ["rm","ddm-run"]`, `# argv: ["rm",1]`)},
		{name: "argv is an object", file: goodName,
			text: replaceOnce(t, good, `# argv: ["rm","ddm-run"]`, `# argv: {"rm":1}`)},
		{name: "argv followed by text", file: goodName,
			text: replaceOnce(t, good, `# argv: ["rm","ddm-run"]`, `# argv: ["rm","ddm-run"] x`)},
		{name: "argv not UTF-8", file: goodName,
			text: replaceOnce(t, good, `"ddm-run"]`, "\"\xffrun\"]")},
		{name: "an argv element with a line feed (JSON escaped)", file: goodName,
			text: replaceOnce(t, good, `"ddm-run"]`, `"ddm\nrun"]`)},
		{name: "an argv element with a carriage return (JSON escaped)", file: goodName,
			text: replaceOnce(t, good, `"ddm-run"]`, `"ddm\rrun"]`)},
		{name: "argv with spaces: Format would not write it back", file: goodName,
			text: replaceOnce(t, good, `["rm","ddm-run"]`, `["rm", "ddm-run"]`)},
		{name: "argv with an escape Format does not write", file: goodName,
			text: replaceOnce(t, good, `"ddm-run"]`, "\"ddm\\"+"u003crun\"]")},

		// ---- exit ----
		{name: "exit 256", file: goodName, contains: []string{"256"},
			text: replaceOnce(t, good, "# exit: 1", "# exit: 256")},
		{name: "exit -1", file: goodName, contains: []string{"-1"},
			text: replaceOnce(t, good, "# exit: 1", "# exit: -1")},
		{name: "exit not a number", file: goodName, contains: []string{"abc"},
			text: replaceOnce(t, good, "# exit: 1", "# exit: abc")},
		{name: "exit with a leading zero", file: goodName,
			text: replaceOnce(t, good, "# exit: 1", "# exit: 01")},

		// ---- streams ----
		{name: "a declared length that is too long", file: goodName, contains: []string{"11", "4"},
			text: cliText("1", "11 bytes, text", "abcd", "0 bytes, text", "")},
		{name: "a declared length that is too short", file: goodName, contains: []string{"1", "4"},
			text: cliText("1", "1 bytes, text", "abcd", "0 bytes, text", "")},
		{name: "a stream header without its unit", file: goodName,
			text: cliText("1", "0 text", "", "0 bytes, text", "")},
		{name: "an unknown encoding", file: goodName, contains: []string{"hex"},
			text: cliText("1", "0 bytes, hex", "", "0 bytes, text", "")},
		{name: "invalid base64", file: goodName,
			text: cliText("1", "3 bytes, base64", "/w!N", "0 bytes, text", "")},
		{name: "base64 that could have been text", file: goodName,
			text: cliText("1", "4 bytes, base64", "YWJjCg==", "0 bytes, text", "")},
		{name: "text holding ESC", file: goodName,
			text: cliText("1", "4 bytes, text", "a\x1b[m", "0 bytes, text", "")},
		{name: "text holding NUL", file: goodName,
			text: cliText("1", "3 bytes, text", "a\x00b", "0 bytes, text", "")},
		{name: "text that is not UTF-8", file: goodName,
			text: cliText("1", "2 bytes, text", "a\xff", "0 bytes, text", "")},
		{name: "no stdout marker", file: goodName,
			text: replaceOnce(t, good, "--- stdout\n", "")},
		{name: "no stderr marker", file: goodName,
			text: replaceOnce(t, good, "--- stderr\n", "")},
		{name: "no line feed after stderr", file: goodName,
			text: strings.TrimSuffix(good, "\n")},
		{name: "data after the stderr stream", file: goodName,
			text: good + "extra\n"},
		{name: "a file with headers only", file: goodName,
			text: replaceOnce(t, good, "\n--- stdout\n\n--- stderr\nerr\n\n", "\n")},

		// ---- the name must agree with the headers ----
		{name: "name with another version", file: "rm-running--docker-29.10.2-rootful.cli", text: good},
		{name: "name with another mode", file: "rm-running--docker-29.10.1-rootless.cli", text: good},
		{name: "name with another tool", file: "rm-running--podman-29.10.1-rootful.cli", text: good},
		{name: "name without --", file: "rm-running-docker-29.10.1-rootful.cli", text: good},
		{name: "name without .cli", file: "rm-running--docker-29.10.1-rootful", text: good},
		{name: "name with another extension", file: "rm-running--docker-29.10.1-rootful.txt", text: good},
		{name: "name without a scenario", file: "--docker-29.10.1-rootful.cli", text: good},

		// ---- pending files stop after argv ----
		{name: "a pending file with an exit", file: pendingName,
			text: replaceOnce(t, pendingText, "\n\n", "\n# exit: 0\n\n")},
		{name: "a pending file with a body", file: pendingName,
			text: pendingText + "--- stdout\n\n--- stderr\n\n"},
		{name: "a pending file with stream headers", file: pendingName,
			text: replaceOnce(t, pendingText, "\n\n", "\n# stdout: 0 bytes, text\n\n")},
		{name: "a pending file without its blank line", file: pendingName,
			text: strings.TrimSuffix(pendingText, "\n")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := enginefixture.Parse("c", tt.file, []byte(tt.text))
			if err == nil {
				t.Fatalf("Parse(%s) accepted the file; text:\n%q", tt.file, tt.text)
			}
			if !strings.Contains(err.Error(), tt.file) {
				t.Errorf("error %q does not name the file %s", err, tt.file)
			}
			// The declared and the found lengths, or the offending value, must be
			// quoted back; look at the message without the file name, which holds
			// digits of its own.
			msg := strings.ReplaceAll(err.Error(), tt.file, "")
			for _, want := range tt.contains {
				if !strings.Contains(msg, want) {
					t.Errorf("error %q does not mention %q", msg, want)
				}
			}
		})
	}
}

// ---- What Parse accepts that is optional. ----

func TestParseAcceptsTheOptionalHeaders(t *testing.T) {
	good := goodText()
	tests := []struct {
		name       string
		text       string
		wantRecord string
		wantBy     string
	}{
		{"neither record nor recorded-by", replaceOnce(t, good, "# recorded-by: synthetic\n", ""), "", ""},
		{"recorded by the contract task", replaceOnce(t, good, "# recorded-by: synthetic", "# recorded-by: engine-contract:record"), "", "engine-contract:record"},
		{"record replay", replaceOnce(t, good, "# recorded-by: synthetic", "# record: replay\n# recorded-by: synthetic"), "replay", "synthetic"},
		{"record skip with a reason", replaceOnce(t, good, "# recorded-by: synthetic", "# record: skip: needs a rootless engine\n# recorded-by: synthetic"), "skip: needs a rootless engine", "synthetic"},
		{"redacted lists what was replaced", replaceOnce(t, good, "# redacted: none", "# redacted: home, user, host"), "", "synthetic"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := enginefixture.Parse("c", goodName, []byte(tt.text))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if f.Meta.Record != tt.wantRecord || f.Meta.RecordedBy != tt.wantBy {
				t.Errorf("Record/RecordedBy = %q/%q, want %q/%q", f.Meta.Record, f.Meta.RecordedBy, tt.wantRecord, tt.wantBy)
			}
			if got := string(enginefixture.Format(f)); got != tt.text {
				t.Errorf("round trip changed the file\n got: %q\nwant: %q", got, tt.text)
			}
		})
	}
}

// A pending file has the headers and an argv, and nothing the engine would have
// answered: the recorder writes it before it has run anything.
func TestAPendingFileStopsAfterArgv(t *testing.T) {
	f, err := enginefixture.Parse("c", pendingName, []byte(pendingText))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Meta.Record != enginefixture.RecordPending || enginefixture.RecordPending != "pending" {
		t.Errorf("Record = %q (RecordPending = %q), want pending", f.Meta.Record, enginefixture.RecordPending)
	}
	if !slices.Equal(f.Exchange.Argv, []string{"rm", "ddm-run"}) {
		t.Errorf("Argv = %q, want [rm ddm-run]", f.Exchange.Argv)
	}
	if f.Exchange.Exit != 0 || len(f.Exchange.Stdout) != 0 || len(f.Exchange.Stderr) != 0 {
		t.Errorf("a pending file carries an answer: exit %d, stdout %q, stderr %q",
			f.Exchange.Exit, f.Exchange.Stdout, f.Exchange.Stderr)
	}
	if got := string(enginefixture.Format(f)); got != pendingText {
		t.Errorf("Format(pending) = %q, want %q", got, pendingText)
	}
}

// ---- Format chooses the writing; there is only one. ----

func fixtureWith(stdout, stderr []byte) enginefixture.Fixture {
	return enginefixture.Fixture{
		Campaign: "c", Name: "x--docker-29.10.1-rootful", Scenario: "x",
		Meta: enginefixture.Meta{
			Tool: enginefixture.Docker, Version: "29.10.1", Mode: enginefixture.Rootful,
			Recorded: "2026-11-27", Scenario: "a test", Redacted: "none", RecordedBy: "synthetic",
		},
		Exchange: enginefixture.Exchange{Argv: []string{"ps"}, Exit: 0, Stdout: stdout, Stderr: stderr},
	}
}

const fixtureName = "x--docker-29.10.1-rootful.cli"

func TestFormatChoosesTheEncodingOfEachStream(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string // the "# stdout:" header
	}{
		{"empty", nil, "# stdout: 0 bytes, text\n"},
		{"plain text", []byte("ok\n"), "# stdout: 3 bytes, text\n"},
		{"tab and line feed are text", []byte("a\tb\nc"), "# stdout: 5 bytes, text\n"},
		{"UTF-8 text counts bytes, not runes", []byte("héllo ✓\n"), "# stdout: 11 bytes, text\n"},
		{"a carriage return", []byte("a\r\nb"), "# stdout: 4 bytes, base64\n"},
		{"a NUL", []byte("a\x00b"), "# stdout: 3 bytes, base64\n"},
		{"an escape sequence hides text from a reviewer", []byte("\x1b[31mred\x1b[0m"), "# stdout: 12 bytes, base64\n"},
		{"a bell", []byte("a\ab"), "# stdout: 3 bytes, base64\n"},
		{"not UTF-8", []byte("a\xffb"), "# stdout: 3 bytes, base64\n"},
		{"only binary", []byte{0xff, 0x00, '\r'}, "# stdout: 3 bytes, base64\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := fixtureWith(tt.data, nil)
			out := enginefixture.Format(f)

			if !bytes.Contains(out, []byte(tt.want)) {
				t.Errorf("Format wrote\n%s\nwant the header %q", out, tt.want)
			}
			back, err := enginefixture.Parse("c", fixtureName, out)
			if err != nil {
				t.Fatalf("Parse(Format(f)): %v\n%s", err, out)
			}
			if !bytes.Equal(back.Exchange.Stdout, tt.data) {
				t.Errorf("stdout came back as %q, want %q", back.Exchange.Stdout, tt.data)
			}
			if !bytes.Equal(enginefixture.Format(back), out) {
				t.Error("Format is not stable: Format(Parse(Format(f))) differs")
			}
		})
	}
}

// A nil stream and an empty stream are the same stream.
func TestFormatTreatsNilAndEmptyStreamsAlike(t *testing.T) {
	a := enginefixture.Format(fixtureWith(nil, nil))
	b := enginefixture.Format(fixtureWith([]byte{}, []byte{}))
	if !bytes.Equal(a, b) {
		t.Errorf("nil and empty streams are written differently:\n%q\n%q", a, b)
	}
}

// The length is what makes a stream unambiguous: a line that looks like the next
// marker, a blank line, or a missing final newline must come back unchanged.
func TestAStreamMayHoldWhatLooksLikeTheFormat(t *testing.T) {
	tests := []struct {
		name           string
		stdout, stderr string
	}{
		{"a stderr marker inside stdout", "before\n--- stderr\nafter\n", "e\n"},
		{"a stdout marker inside stderr", "o\n", "--- stdout\nx\n"},
		{"both markers, and a header look-alike", "--- stdout\n--- stderr\n# exit: 9\n", "# argv: [\"x\"]\n--- stderr\n"},
		{"only blank lines", "\n\n\n", "\n"},
		{"no final newline on either stream", "abc", "def"},
		{"a final blank line", "abc\n\n", ""},
		{"a header-shaped first line", "# tool: podman\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := fixtureWith([]byte(tt.stdout), []byte(tt.stderr))
			out := enginefixture.Format(f)

			back, err := enginefixture.Parse("c", fixtureName, out)
			if err != nil {
				t.Fatalf("Parse(Format(f)): %v\n%s", err, out)
			}
			if !sameExchange(back.Exchange, f.Exchange) {
				t.Errorf("exchange changed on the way:\n got %+v\nwant %+v", back.Exchange, f.Exchange)
			}
			if !bytes.Equal(enginefixture.Format(back), out) {
				t.Error("Format(Parse(Format(f))) != Format(f)")
			}
		})
	}
}

// base64 is cut at 76 columns so a diff of a recorded binary stays readable.
func TestBase64IsWrappedAt76Columns(t *testing.T) {
	data := bytes.Repeat([]byte{0xff, 0x00, 0x80}, 100) // 300 bytes: 400 base64 characters
	f := fixtureWith(data, nil)

	out := enginefixture.Format(f)

	body := string(out)
	start := strings.Index(body, "--- stdout\n")
	end := strings.Index(body, "--- stderr\n")
	if start < 0 || end < start {
		t.Fatalf("no stdout section in:\n%s", out)
	}
	lines := strings.Split(strings.TrimSuffix(body[start+len("--- stdout\n"):end], "\n"), "\n")
	if len(lines) < 5 {
		t.Fatalf("400 base64 characters in %d line(s), want them wrapped", len(lines))
	}
	for i, l := range lines {
		if len(l) > 76 || (i < len(lines)-1 && len(l) != 76) {
			t.Errorf("line %d is %d characters, want 76 (the last one may be shorter)", i+1, len(l))
		}
	}
	back, err := enginefixture.Parse("c", fixtureName, out)
	if err != nil || !bytes.Equal(back.Exchange.Stdout, data) {
		t.Errorf("round trip: err %v, equal %v", err, err == nil && bytes.Equal(back.Exchange.Stdout, data))
	}
}

// Every exit code of a process fits the header.
func TestEveryExitCodeRoundTrips(t *testing.T) {
	for _, code := range []int{0, 1, 2, 125, 126, 127, 255} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			f := fixtureWith([]byte("o"), []byte("e"))
			f.Exchange.Exit = code
			back, err := enginefixture.Parse("c", fixtureName, enginefixture.Format(f))
			if err != nil || back.Exchange.Exit != code {
				t.Errorf("exit %d came back as %d (err %v)", code, back.Exchange.Exit, err)
			}
		})
	}
}

// ---- Layering. ----

// The format package is read by the recorder (OC-09a) and the test helpers; it
// must not drag "testing" into a binary, and the import guard (E1) is what
// keeps the package itself out of dk.
func TestProductionCodeDoesNotImportTesting(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	prod := 0
	fset := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		prod++
		af, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imp := range af.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if p == "testing" || strings.HasPrefix(p, "testing/") || strings.HasSuffix(p, "/fakecli") {
				t.Errorf("%s imports %q: the production code of enginefixture must not", path, p)
			}
		}
	}
	if prod == 0 {
		t.Error("no production Go file in internal/enginefixture")
	}
}

// Format must keep working on a name-less fixture: the name is not part of the
// bytes, the headers are.
func TestFormatDoesNotWriteTheName(t *testing.T) {
	f := fixtureWith([]byte("o\n"), nil)
	f.Name = "something-else"
	f.Campaign = "elsewhere"
	if out := enginefixture.Format(f); bytes.Contains(out, []byte("something-else")) || bytes.Contains(out, []byte("elsewhere")) {
		t.Errorf("Format wrote the name or the campaign:\n%s", out)
	}
}
