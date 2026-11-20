package nexusfixture_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/nexusfixture"
)

// DEVDESK-580 step 5: the optional "flow" and "effect" headers and the
// "badcred" identity. The inputs are synthetic (see synthHeaders).

// withHeaders inserts header lines between the "redacted" header and the request.
func withHeaders(lines string) string {
	return synthHeaders + lines + synthRequest + synthResponse
}

func TestFlowAndEffectConstantsAreTheWrittenValues(t *testing.T) {
	if nexusfixture.FlowBearer != "bearer" || nexusfixture.FlowRedirects != "redirects" || nexusfixture.EffectCaches != "caches" {
		t.Errorf("values = %q %q %q, want bearer redirects caches",
			nexusfixture.FlowBearer, nexusfixture.FlowRedirects, nexusfixture.EffectCaches)
	}
	if nexusfixture.BadCredentials != "badcred" {
		t.Errorf("BadCredentials = %q, want badcred", nexusfixture.BadCredentials)
	}
}

// Flow and effect are written after state and before record, in that order.
func TestParseFlowAndEffectHeadersRoundTrip(t *testing.T) {
	tests := []struct {
		name       string
		lines      string
		wantFlow   string
		wantEffect string
	}{
		{"flow bearer", "# flow: bearer\n", nexusfixture.FlowBearer, ""},
		{"flow redirects", "# flow: redirects\n", nexusfixture.FlowRedirects, ""},
		{"effect caches", "# effect: caches\n", "", nexusfixture.EffectCaches},
		{"flow and effect", "# flow: bearer\n# effect: caches\n", nexusfixture.FlowBearer, nexusfixture.EffectCaches},
		{"after state, before record and recorded-by",
			"# state: upstream-cut\n# flow: redirects\n# effect: caches\n# record: replay\n# recorded-by: nexus-bench record\n",
			nexusfixture.FlowRedirects, nexusfixture.EffectCaches},
		{"neither", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := withHeaders(tt.lines)
			f, err := nexusfixture.Parse("synthetic", "flow--anon.http", []byte(data))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if f.Meta.Flow != tt.wantFlow || f.Meta.Effect != tt.wantEffect {
				t.Errorf("Meta.Flow, Meta.Effect = %q, %q, want %q, %q", f.Meta.Flow, f.Meta.Effect, tt.wantFlow, tt.wantEffect)
			}
			if got := nexusfixture.Format(f); !bytes.Equal(got, []byte(data)) {
				t.Errorf("Format(Parse(x)) != x:\n%s\nwant:\n%s", got, data)
			}
		})
	}
}

// Format writes the headers a Fixture holds, wherever it came from.
func TestFormatWritesFlowAndEffectInPlace(t *testing.T) {
	f, err := nexusfixture.Parse("synthetic", "built--anon.http", []byte(synthValid))
	if err != nil {
		t.Fatal(err)
	}
	f.Meta.State = "upstream-cut"
	f.Meta.Flow = nexusfixture.FlowBearer
	f.Meta.Effect = nexusfixture.EffectCaches
	f.Meta.Record = "replay"
	want := synthHeaders + "# state: upstream-cut\n# flow: bearer\n# effect: caches\n# record: replay\n" + synthRequest + synthResponse
	if got := string(nexusfixture.Format(f)); got != want {
		t.Errorf("Format = \n%s\nwant\n%s", got, want)
	}
}

func TestParseRefusesAnUnknownFlowOrEffectNamingTheHeader(t *testing.T) {
	tests := []struct {
		name   string
		lines  string
		header string // the header the error must name
	}{
		{"flow follow-all", "# flow: follow-all\n", "flow"},
		{"flow is case sensitive", "# flow: Bearer\n", "flow"},
		{"flow with a second word", "# flow: bearer redirects\n", "flow"},
		{"effect deletes", "# effect: deletes\n", "effect"},
		{"effect is case sensitive", "# effect: Caches\n", "effect"},
		{"flow given twice", "# flow: bearer\n# flow: redirects\n", "flow"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := nexusfixture.Parse("synthetic", "bad--anon.http", []byte(withHeaders(tt.lines)))
			if err == nil {
				t.Fatalf("Parse accepted the file: %+v", f.Meta)
			}
			msg := err.Error()
			if !strings.Contains(msg, "bad--anon.http") {
				t.Errorf("error %q does not name the file", msg)
			}
			if !strings.Contains(strings.ToLower(msg), tt.header) {
				t.Errorf("error %q does not name the %q header", msg, tt.header)
			}
			// The offending value is data of the file: it may be quoted, but the
			// error must be about the header, not the unrelated "unknown header".
			if strings.Contains(msg, `unknown header "`+tt.header+`"`) {
				t.Errorf("error %q reads as if the %q header did not exist", msg, tt.header)
			}
		})
	}
}

func TestParseBadCredentialsIdentity(t *testing.T) {
	f, err := nexusfixture.Parse("synthetic", "login-refused--badcred.http", []byte(synthValid))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Identity != nexusfixture.BadCredentials || f.Name != "login-refused--badcred" || f.Scenario != "login-refused" {
		t.Errorf("got {identity %q, name %q, scenario %q}, want {badcred, login-refused--badcred, login-refused}",
			f.Identity, f.Name, f.Scenario)
	}
	if got := nexusfixture.Format(f); string(got) != synthValid {
		t.Errorf("Format(Parse(x)) != x:\n%s", got)
	}

	// The identity list stays closed: the error names what is accepted now.
	_, err = nexusfixture.Parse("synthetic", "bad--root.http", []byte(synthValid))
	if err == nil || !strings.Contains(err.Error(), "badcred") {
		t.Errorf("error for an unknown suffix = %v, want it to list badcred among the identities", err)
	}
}
