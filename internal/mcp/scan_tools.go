package mcp

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/scan"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The scan tools: what this context has already scanned, and what one scan
// found. They are most of why §3.38 is worth doing — an agent can run trivy
// itself, but it cannot know what has already been looked at.

// defaultFindingLimit and maxFindingLimit bound one page of findings. An image
// scan produces thousands, and a tool result is a message: returning all of them
// spends the agent's context on a list it was going to filter anyway.
const (
	defaultFindingLimit = 100
	maxFindingLimit     = 1000
)

// ── scan_inventory ──────────────────────────────────────────────────────────

type scanTarget struct {
	Kind       string    `json:"kind" jsonschema:"image or repository"`
	Name       string    `json:"name" jsonschema:"the Docker reference of an image or the absolute path of a repository; this is what scan_result takes as its target"`
	Critical   int       `json:"critical"`
	High       int       `json:"high"`
	Medium     int       `json:"medium"`
	Low        int       `json:"low"`
	Sensitive  *bool     `json:"sensitive,omitempty" jsonschema:"whether a secret was found; absent means no stage looked which is not the same as none found"`
	ScannedAt  time.Time `json:"scanned_at"`
	AgeSeconds int64     `json:"age_seconds" jsonschema:"how long ago the scan ran so staleness can be judged without knowing the current time"`
}

type scanInventoryOut struct {
	Context string       `json:"context" jsonschema:"the context these targets belong to; repository entries are scoped to it while image entries are shared between contexts"`
	Targets []scanTarget `json:"targets"`
}

func registerScanInventory(s *sdk.Server, env *Env) {
	sdk.AddTool(s, &sdk.Tool{
		Name:        "scan_inventory",
		Description: toolDescription("scan_inventory"),
	}, func(_ context.Context, _ *sdk.CallToolRequest, _ any) (*sdk.CallToolResult, scanInventoryOut, error) {
		return nil, scanInventoryOut{Context: env.Context, Targets: inventory(env.Context)}, nil
	})
}

// inventory reads both scan caches and drops what no longer exists.
//
// The reconciliation is §3.37's, and both halves of its rule are kept: an image
// absent from the daemon's list is dropped, but an enumeration that *failed*
// keeps every image — "docker is not running" and "the image is gone" are the
// same silence from here, and reading the first as the second would empty the
// inventory the moment the daemon stops. A path is gone only on a definite
// absence.
//
// It hides, it never deletes. This server writes nothing at all.
func inventory(contextName string) []scanTarget {
	targets := make([]scanTarget, 0)
	now := time.Now()

	images, imagesKnown := localImages()
	if entries, err := cache.ReadImageScanEntries(); err != nil {
		log.Printf("ERROR [mcp/scan] read image scan cache: %v", err)
	} else {
		for name, entry := range entries {
			if imagesKnown {
				if _, ok := images[name]; !ok {
					continue
				}
			}
			targets = append(targets, target("image", name, entry.Critical, entry.High,
				entry.Medium, entry.Low, entry.Sensitive, entry.ScannedAt, now))
		}
	}

	if entries, err := cache.ReadWorkspaceScanEntries(contextName); err != nil {
		log.Printf("ERROR [mcp/scan] read workspace scan cache: %v", err)
	} else {
		for path, entry := range entries {
			if isGone(path) {
				continue
			}
			targets = append(targets, target("repository", path, entry.Critical, entry.High,
				entry.Medium, entry.Low, entry.Sensitive, entry.ScannedAt, now))
		}
	}

	// Both caches are maps, so their range order is deliberately random —
	// settling one here is what makes two successive calls comparable. CRITICAL
	// descending, for the reason the inventory view sorts that way: what the
	// list is consulted for goes on top.
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Critical != targets[j].Critical {
			return targets[i].Critical > targets[j].Critical
		}
		return strings.ToLower(targets[i].Name) < strings.ToLower(targets[j].Name)
	})
	return targets
}

func target(kind, name string, critical, high, medium, low int, sensitive *bool, scannedAt, now time.Time) scanTarget {
	t := scanTarget{
		Kind: kind, Name: name,
		Critical: critical, High: high, Medium: medium, Low: low,
		Sensitive: sensitive, ScannedAt: scannedAt,
	}
	if !scannedAt.IsZero() {
		t.AgeSeconds = int64(now.Sub(scannedAt).Seconds())
	}
	return t
}

// listImages is docker.ListImages, indirected for the tests and nothing else —
// production never reassigns it. A test cannot pull an image, and without the
// seam the reconciliation could only be asserted by checking that a fixture is
// absent, which it would pass for the wrong reason. Same seam, same reason, as
// the security view's.
var listImages = docker.ListImages

// localImages names the images on this machine, and says whether it could find
// out. The second return is the whole point — see inventory.
func localImages() (map[string]struct{}, bool) {
	list, err := listImages()
	if err != nil {
		log.Printf("INFO [mcp/scan] image list unavailable, keeping every cached image: %v", err)
		return nil, false
	}
	names := make(map[string]struct{}, len(list))
	for _, img := range list {
		names[img.Name()] = struct{}{}
	}
	return names, true
}

// isGone says a repository path has been removed, and only that. A permission
// error, or a share answering slowly, means the path could not be *read*, which
// is a different claim.
func isGone(path string) bool {
	_, err := os.Stat(path)
	return err != nil && os.IsNotExist(err)
}

// ── scan_result ─────────────────────────────────────────────────────────────

// finding is what a scan finding looks like from outside DevDesk.
//
// It is a projection of scan.Finding and it has **no Match field**. That is the
// guarantee, not a filter applied on the way out: the string a secret scanner
// matched is the secret itself, so a field able to carry it is a field that gets
// filled in by accident one day. Rule, file, line and fingerprint identify the
// finding without reproducing it, which is where §3.10 already landed for
// Gitleaks triage.
type finding struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Severity    string   `json:"severity"`
	Category    string   `json:"category" jsonschema:"vulnerability, secret, license or misconfiguration"`
	Source      string   `json:"source" jsonschema:"which scanner reported it"`
	File        string   `json:"file,omitempty"`
	Line        int      `json:"line,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty" jsonschema:"identifies the finding for .gitleaksignore; it is not the matched string, which is never exposed"`
	PkgName     string   `json:"pkg_name,omitempty"`
	Version     string   `json:"version,omitempty"`
	FixedIn     string   `json:"fixed_in,omitempty"`
	Resolution  string   `json:"resolution,omitempty"`
	References  []string `json:"references,omitempty"`
}

type scanResultIn struct {
	Target   string   `json:"target" jsonschema:"an image reference or a repository path, exactly as scan_inventory reports it"`
	Severity []string `json:"severity,omitempty" jsonschema:"keep only these severities among CRITICAL HIGH MEDIUM LOW UNKNOWN; empty keeps every one"`
	Category []string `json:"category,omitempty" jsonschema:"keep only these categories among vulnerability secret license misconfiguration; empty keeps every one"`
	Offset   int      `json:"offset,omitempty" jsonschema:"how many findings to skip, for paging through a large scan"`
	Limit    int      `json:"limit,omitempty" jsonschema:"how many findings to return; defaults to 100 and is capped at 1000"`
}

type scanResultOut struct {
	Target         string    `json:"target"`
	ScannedAt      time.Time `json:"scanned_at"`
	Critical       int       `json:"critical"`
	High           int       `json:"high"`
	Medium         int       `json:"medium"`
	Low            int       `json:"low"`
	Unknown        int       `json:"unknown"`
	SecretCount    int       `json:"secret_count"`
	SecretsScanned bool      `json:"secrets_scanned" jsonschema:"false means no secret stage ran, so secret_count says nothing about this target"`
	LicenseCount   int       `json:"license_count"`
	MisconfigCount int       `json:"misconfig_count"`
	Errors         []string  `json:"errors,omitempty" jsonschema:"stages that failed; findings may be missing rather than absent"`

	Matched  int       `json:"matched" jsonschema:"how many findings the filters kept, before paging"`
	Total    int       `json:"total" jsonschema:"how many findings the scan holds in all"`
	Offset   int       `json:"offset"`
	Findings []finding `json:"findings"`
}

func registerScanResult(s *sdk.Server, env *Env) {
	sdk.AddTool(s, &sdk.Tool{
		Name:        "scan_result",
		Description: toolDescription("scan_result"),
	}, func(_ context.Context, _ *sdk.CallToolRequest, in scanResultIn) (*sdk.CallToolResult, scanResultOut, error) {
		name := strings.TrimSpace(in.Target)
		if name == "" {
			return nil, scanResultOut{}, fmt.Errorf("target is required — scan_inventory lists the names this context has results for")
		}

		result, err := storedResult(name)
		if err != nil {
			return nil, scanResultOut{}, fmt.Errorf("no stored scan for %q in context %q — scan_inventory lists what there is, and a target it does not list has either never been scanned or no longer exists", name, env.Context)
		}

		return nil, project(result, in), nil
	})
}

// storedResult finds a target's stored findings without being told what kind of
// target it is.
//
// The caller has a name out of scan_inventory and nothing else, so asking it to
// carry the kind as well would be asking it to remember something it can look
// up. An image reference and an absolute path do not collide in practice.
func storedResult(target string) (*scan.Result, error) {
	if result, err := cache.ReadImageScanResult(target); err == nil {
		return result, nil
	}
	return cache.ReadWorkspaceScanResult(target)
}

// project turns a stored result into what leaves the process: the summary, the
// findings the filters kept, and one page of them.
func project(result *scan.Result, in scanResultIn) scanResultOut {
	out := scanResultOut{
		Target:         result.Target,
		ScannedAt:      result.EndTime,
		Critical:       result.Counts.Critical,
		High:           result.Counts.High,
		Medium:         result.Counts.Medium,
		Low:            result.Counts.Low,
		Unknown:        result.Counts.Unknown,
		SecretCount:    result.SecretCount,
		SecretsScanned: result.SecretsScanned,
		LicenseCount:   result.LicenseCount,
		MisconfigCount: result.MisconfigCount,
		Errors:         result.Errors,
		Total:          len(result.Findings),
		Findings:       []finding{},
	}

	severities := normalisedSet(in.Severity, strings.ToUpper)
	categories := normalisedSet(in.Category, strings.ToLower)

	kept := make([]scan.Finding, 0, len(result.Findings))
	for _, f := range result.Findings {
		if len(severities) > 0 {
			if _, ok := severities[strings.ToUpper(string(f.Severity))]; !ok {
				continue
			}
		}
		if len(categories) > 0 {
			if _, ok := categories[categoryName(scan.Categorize(f))]; !ok {
				continue
			}
		}
		kept = append(kept, f)
	}
	out.Matched = len(kept)

	offset, limit := page(in.Offset, in.Limit)
	out.Offset = offset
	if offset >= len(kept) {
		return out
	}
	end := offset + limit
	if end > len(kept) {
		end = len(kept)
	}
	for _, f := range kept[offset:end] {
		out.Findings = append(out.Findings, expose(f))
	}
	return out
}

// page normalises what the caller asked for. A negative offset is a mistake, not
// a request to count from the end; an absent limit is the default and an
// oversized one is capped, because the cap protects the agent's own context and
// a caller cannot waive that on its behalf.
func page(offset, limit int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	switch {
	case limit <= 0:
		limit = defaultFindingLimit
	case limit > maxFindingLimit:
		limit = maxFindingLimit
	}
	return offset, limit
}

// expose is the one place a scan.Finding becomes something that leaves the
// process. It names every field it copies, so a field added to scan.Finding
// arrives here as an omission rather than as an exposure — the same argument the
// allow-list of tools makes.
func expose(f scan.Finding) finding {
	return finding{
		ID:          f.ID,
		Title:       f.Title,
		Description: f.Description,
		Severity:    string(f.Severity),
		Category:    categoryName(scan.Categorize(f)),
		Source:      f.Source,
		File:        f.File,
		Line:        f.Line,
		Fingerprint: f.Fingerprint,
		PkgName:     f.PkgName,
		Version:     f.Version,
		FixedIn:     f.FixedIn,
		Resolution:  f.Resolution,
		References:  f.References,
	}
}

// categoryName is scan.Category as a word. The type is an int with no String
// method, so a conversion would yield a rune rather than a name — and the four
// values are a vocabulary an agent filters on, which makes them part of the
// tool's contract rather than an internal enum.
//
// The default is "vulnerability" for the reason Categorize's is: a source this
// does not know stays visible in the family that is looked at first, rather than
// disappearing into a name nothing filters for.
func categoryName(c scan.Category) string {
	switch c {
	case scan.CategorySecret:
		return "secret"
	case scan.CategoryLicense:
		return "license"
	case scan.CategoryMisconfiguration:
		return "misconfiguration"
	default:
		return "vulnerability"
	}
}

func normalisedSet(values []string, normalise func(string) string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, v := range values {
		if v = normalise(strings.TrimSpace(v)); v != "" {
			out[v] = struct{}{}
		}
	}
	return out
}
