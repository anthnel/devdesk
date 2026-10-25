package agentusage

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// crossCheckTolerance is the largest relative gap accepted between our totals
// and the reference tool's (acceptance criterion 5 of DEVDESK-40).
const crossCheckTolerance = 0.02

const (
	envReference = "DEVDESK_CCUSAGE_REF"
	envProjects  = "DEVDESK_CLAUDE_PROJECTS"
)

// refSession is one session of the reference file. Only Total is required;
// the other counters are compared when present.
type refSession struct {
	ID         string `json:"id"`
	Total      *int64 `json:"total"`
	Input      *int64 `json:"input"`
	Output     *int64 `json:"output"`
	CacheRead  *int64 `json:"cacheRead"`
	CacheWrite *int64 `json:"cacheWrite"`
}

type refFile struct {
	Sessions []refSession `json:"sessions"`
}

// metricReport is one compared counter. It never carries content or a path.
type metricReport struct {
	Name      string
	Got, Want int64
	Deviation float64
	OK        bool
}

// sessionReport is the outcome for one reference session.
type sessionReport struct {
	ID      string
	Missing bool   // no transcript found under the root
	Err     string // locate or parse failure, without paths
	Agents  int
	Diag    Diagnostics
	Metrics []metricReport
}

// OK reports whether the session was found and every metric is in tolerance.
func (r sessionReport) OK() bool {
	if r.Missing || r.Err != "" {
		return false
	}
	for _, m := range r.Metrics {
		if !m.OK {
			return false
		}
	}
	return len(r.Metrics) > 0
}

// deviation is |got-want| / want; a zero reference only matches a zero count.
func deviation(got, want int64) float64 {
	if want == 0 {
		if got == 0 {
			return 0
		}
		return 1
	}
	return math.Abs(float64(got-want)) / math.Abs(float64(want))
}

func newMetric(name string, got int64, want *int64, tol float64) (metricReport, bool) {
	if want == nil {
		return metricReport{}, false
	}
	d := deviation(got, *want)
	return metricReport{Name: name, Got: got, Want: *want, Deviation: d, OK: d < tol}, true
}

// findTranscript looks for <root>/*/<id>.jsonl and returns its path.
func findTranscript(root, id string) (string, bool) {
	if id == "" || id != filepath.Base(id) || strings.ContainsAny(id, `*?[\`) {
		return "", false
	}
	dirs, err := os.ReadDir(root)
	if err != nil {
		return "", false
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		p := filepath.Join(root, d.Name(), id+transcriptExt)
		if fi, err := os.Lstat(p); err == nil && fi.Mode().IsRegular() {
			return p, true
		}
	}
	return "", false
}

// compareToReference reads the reference file and compares every session it
// lists with what ParseSession computes from the transcripts under root. An
// unreadable or invalid reference is an error; a session problem is reported
// in its sessionReport.
func compareToReference(refPath, root string, tol float64) ([]sessionReport, error) {
	raw, err := os.ReadFile(refPath)
	if err != nil {
		return nil, fmt.Errorf("reading reference file %q failed", filepath.Base(refPath))
	}
	var ref refFile
	if err := json.Unmarshal(raw, &ref); err != nil {
		return nil, fmt.Errorf("reference file is not valid JSON")
	}
	if len(ref.Sessions) == 0 {
		return nil, fmt.Errorf("reference file lists no session")
	}
	reports := make([]sessionReport, 0, len(ref.Sessions))
	for _, s := range ref.Sessions {
		reports = append(reports, compareSession(s, root, tol))
	}
	return reports, nil
}

func compareSession(s refSession, root string, tol float64) sessionReport {
	rep := sessionReport{ID: s.ID}
	if s.Total == nil {
		rep.Err = "reference session has no total"
		return rep
	}
	path, ok := findTranscript(root, s.ID)
	if !ok {
		rep.Missing = true
		return rep
	}
	files, err := LocateSession(path)
	if err != nil {
		rep.Err = "locate failed"
		return rep
	}
	u, err := ParseSession(files)
	if err != nil {
		rep.Err = "parse failed"
		return rep
	}
	rep.Agents, rep.Diag = len(u.Agents), u.Diagnostics
	tot := u.Total()
	for _, c := range []struct {
		name string
		got  int64
		want *int64
	}{
		{"total", tot.Total(), s.Total},
		{"input", tot.Input, s.Input},
		{"output", tot.Output, s.Output},
		{"cacheRead", tot.CacheRead, s.CacheRead},
		{"cacheWrite", tot.CacheWrite, s.CacheWrite},
	} {
		if m, ok := newMetric(c.name, c.got, c.want, tol); ok {
			rep.Metrics = append(rep.Metrics, m)
		}
	}
	return rep
}

// diagCounters renders Diagnostics as counters only.
func diagCounters(d Diagnostics) string {
	return fmt.Sprintf("unreadable=%d oversized=%d noUsage=%d noMsgID=%d crossFileDup=%d metaMissing=%d metaUnreadable=%d truncatedFiles=%d fileIssues=%d",
		d.UnreadableLines, d.OversizedLines, d.NoUsage, d.NoMessageID, d.CrossFileDuplicates,
		d.MetaMissing, d.MetaUnreadable, len(d.TruncatedFiles), len(d.Files))
}

// TestSessionTotalsMatchCcusage is the real-data check of acceptance
// criterion 5. It needs a reference file kept outside the repository, so it
// is skipped unless DEVDESK_CCUSAGE_REF is set (CI never sets it).
func TestSessionTotalsMatchCcusage(t *testing.T) {
	refPath := os.Getenv(envReference)
	if refPath == "" {
		t.Skipf("%s not set: no ccusage reference file, cross-check skipped", envReference)
	}
	root := os.Getenv(envProjects)
	if root == "" {
		var err error
		if root, err = ProjectsRoot(); err != nil {
			t.Fatalf("no transcripts root: %v", err)
		}
	}
	reports, err := compareToReference(refPath, root, crossCheckTolerance)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range reports {
		t.Logf("session %s: agents=%d diagnostics{%s}", r.ID, r.Agents, diagCounters(r.Diag))
		for _, m := range r.Metrics {
			t.Logf("  %-10s parsed=%d reference=%d deviation=%.4f%%", m.Name, m.Got, m.Want, m.Deviation*100)
		}
		switch {
		case r.Missing:
			t.Errorf("session %s: transcript not found under the projects root", r.ID)
		case r.Err != "":
			t.Errorf("session %s: %s", r.ID, r.Err)
		case !r.OK():
			t.Errorf("session %s: deviation at or above %.0f%%", r.ID, crossCheckTolerance*100)
		}
	}
}

// TestCompareToReference covers the comparison and the reference parsing
// without any external data.
func TestCompareToReference(t *testing.T) {
	const id = "11111111-2222-3333-4444-555555555555"
	root := t.TempDir()
	proj := filepath.Join(root, "-some-project")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	line := `{"message":{"id":"m1","model":"x","stop_reason":"end_turn","usage":{"input_tokens":400,"output_tokens":300,"cache_read_input_tokens":200,"cache_creation_input_tokens":100}}}` + "\n"
	if err := os.WriteFile(filepath.Join(proj, id+transcriptExt), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		ref      string
		wantErr  bool
		missing  bool
		wantOK   bool
		wantDevs map[string]float64 // approximate, in fraction
	}{
		{"zero gap", `{"sessions":[{"id":"` + id + `","total":1000}]}`, false, false, true, map[string]float64{"total": 0}},
		{"1.9 percent", `{"sessions":[{"id":"` + id + `","total":981}]}`, false, false, true, map[string]float64{"total": 0.01937}},
		{"2.1 percent", `{"sessions":[{"id":"` + id + `","total":979}]}`, false, false, false, map[string]float64{"total": 0.02145}},
		{"optional fields match", `{"sessions":[{"id":"` + id + `","total":1000,"input":400,"output":300,"cacheRead":200,"cacheWrite":100}]}`, false, false, true, map[string]float64{"input": 0, "cacheWrite": 0}},
		{"optional field off", `{"sessions":[{"id":"` + id + `","total":1000,"output":100}]}`, false, false, false, map[string]float64{"output": 2}},
		{"missing session", `{"sessions":[{"id":"99999999-2222-3333-4444-555555555555","total":1000}]}`, false, true, false, nil},
		{"path-like id is not found", `{"sessions":[{"id":"../` + id + `","total":1000}]}`, false, true, false, nil},
		{"no total", `{"sessions":[{"id":"` + id + `"}]}`, false, false, false, nil},
		{"invalid JSON", `{"sessions":[`, true, false, false, nil},
		{"no sessions", `{"sessions":[]}`, true, false, false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refPath := filepath.Join(t.TempDir(), "ref.json")
			if err := os.WriteFile(refPath, []byte(tt.ref), 0o600); err != nil {
				t.Fatal(err)
			}
			reps, err := compareToReference(refPath, root, crossCheckTolerance)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(reps) != 1 {
				t.Fatalf("got %d reports", len(reps))
			}
			r := reps[0]
			if r.Missing != tt.missing {
				t.Errorf("Missing = %v, want %v", r.Missing, tt.missing)
			}
			if r.OK() != tt.wantOK {
				t.Errorf("OK = %v, want %v (%+v)", r.OK(), tt.wantOK, r)
			}
			for _, m := range r.Metrics {
				if want, ok := tt.wantDevs[m.Name]; ok && math.Abs(m.Deviation-want) > 1e-4 {
					t.Errorf("%s deviation = %.5f, want %.5f", m.Name, m.Deviation, want)
				}
			}
		})
	}

	if _, err := compareToReference(filepath.Join(t.TempDir(), "absent.json"), root, crossCheckTolerance); err == nil {
		t.Error("absent reference file: want an error")
	}
}
