package templates

import (
	"context"
	"fmt"
	"log"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/template"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
)

// Scanning a template is scanning what it would put in a repository, and it is
// worth doing *before* that: a secret in a template ends up in every repository
// made from it.
//
// The template is fetched, written to a fixed directory of the scan cache
// (template.Materialize), and that directory is scanned like any other. It is a
// directory scan on purpose: the result is cached by path, so it appears in the
// `:sec` inventory and opens in the same results view, with nothing new to
// build. What is not reused is the workspaces list — the copy is not in it, and
// nothing here pretends it is.

const (
	reasonNoScanner   = "No scanner available — install Trivy or Gitleaks, or check scan settings"
	reasonScanRunning = "A scan of this template is already running"
)

// scanFunc runs the scanners over a directory. It is a field of the model so a
// test can answer without running Trivy.
type scanFunc func(ctx context.Context, dir string, opts scan.ScanOptions) (*scan.Result, error)

func runScanners(ctx context.Context, dir string, opts scan.ScanOptions) (*scan.Result, error) {
	return scan.NewScanner(opts).Scan(ctx, dir, scan.TargetDirectory)
}

// DepsCheckedMsg carries where the scanners resolve from on this machine.
type DepsCheckedMsg struct {
	Deps scan.DependencyStatus
}

// checkDepsCmd resolves the scanners once, off the Update goroutine.
func checkDepsCmd(cfg config.ScanConfig) tea.Cmd {
	return func() tea.Msg { return DepsCheckedMsg{Deps: scan.CheckDependencies(cfg)} }
}

// ScanStartingMsg says the scan left the queue and holds a worker. The cancel
// goes with it — it is created inside the Cmd, and stored by the registry in the
// same Update that marks the item running, so there is no window where the row
// is running and cannot be stopped (D7).
type ScanStartingMsg struct {
	Path   string
	Cancel context.CancelFunc
}

// ScanCompleteMsg is the scan's outcome.
type ScanCompleteMsg struct {
	Slug   string
	Name   string
	Path   string
	Counts scan.SeverityCounts
	// Partial is set when a stage failed and nothing was found: the scan ran, and
	// "no findings" would be a claim about something it did not look at.
	Partial bool
	Err     error
}

func (m ScanStartingMsg) Transition() jobs.Transition {
	return jobs.Transition{Kind: jobs.KindScan, Target: m.Path, State: jobs.ItemRunning, Cancel: m.Cancel}
}

func (m ScanCompleteMsg) Transition() jobs.Transition {
	t := jobs.Transition{Kind: jobs.KindScan, Target: m.Path, State: jobs.ItemDone}
	if m.Err != nil {
		t.State = jobs.ItemFailed
		t.Detail = "scan failed — check logs"
	}
	return t
}

// Compile-time proof that both can be applied to the registry: a message added
// without it is routed, updates the view, and leaves its row spinning for the
// life of the session.
var (
	_ jobs.Reporter = ScanStartingMsg{}
	_ jobs.Reporter = ScanCompleteMsg{}
)

// scanJob is everything the scan needs, read in Update and carried into the
// Cmd, which runs on another goroutine and must not reach back into the model
// (Rule 110).
type scanJob struct {
	entry template.Entry
	creds template.Credentials
	opts  scan.ScanOptions
	run   scanFunc
}

// scanTemplateCmd fetches, materializes and scans one template.
//
// contextName travels with the command rather than being read at the end (D68):
// the counts belong to the context the scan was launched in, whatever the user
// is looking at when it lands.
func scanTemplateCmd(job scanJob, contextName string) tea.Cmd {
	dir, dirErr := template.ScanDir(job.entry.Slug)
	ctx, cancel := context.WithCancel(context.Background())

	return tea.Sequence(
		func() tea.Msg { return ScanStartingMsg{Path: dir, Cancel: cancel} },
		func() tea.Msg {
			defer cancel()
			complete := ScanCompleteMsg{Slug: job.entry.Slug, Name: job.entry.Name, Path: dir}

			if dirErr != nil {
				complete.Err = dirErr
				return complete
			}
			result, err := job.scanOnce(ctx, contextName)
			if err != nil {
				log.Printf("ERROR [templates] scan %s: %v", job.entry.Slug, err)
				complete.Err = err
				return complete
			}
			complete.Counts = result.Counts
			complete.Partial = len(result.Errors) > 0 && result.TotalFindings() == 0
			return complete
		},
	)
}

func (j scanJob) scanOnce(ctx context.Context, contextName string) (*scan.Result, error) {
	files, err := template.Fetch(ctx, j.entry.Source, j.creds)
	if err != nil {
		return nil, fmt.Errorf("fetching the template: %w", err)
	}
	dir, err := template.Materialize(j.entry.Slug, files)
	if err != nil {
		return nil, fmt.Errorf("writing the template: %w", err)
	}
	result, err := j.run(ctx, dir, j.opts)
	if err != nil {
		return nil, err
	}
	if _, err := cache.StoreWorkspaceScan(contextName, dir, result); err != nil {
		// The scan happened and its counts are reported; only the cache write
		// failed, and `:sec` will not show it.
		log.Printf("ERROR [templates] store scan %s: %v", j.entry.Slug, err)
	}
	return result, nil
}

// scanOptions assembles the options for a template: the configured scanners,
// and no CI score — a template is not a repository with a pipeline to grade, and
// plumber would go looking for a remote it does not have.
func (m Model) scanOptions() scan.ScanOptions {
	opts := scan.OptionsFromConfig(m.config)
	opts.EnableCIScore = false
	return opts
}

// scannerState says whether a scan can run at all on this machine. Not knowing
// is not knowing that not: until the check comes back the key stays lit, since
// greying it for a few frames only to un-grey it reads as a fault (Rule 130).
func (m Model) scannerState() shortcut.Availability {
	if m.deps == nil || m.deps.TrivyAvailable || m.deps.GitleaksAvailable {
		return shortcut.Availability{}
	}
	return shortcut.Unavailable(reasonNoScanner)
}

// scanning reports whether a scan of this directory is live, wherever it was
// started — the registry is one bookkeeping.
func (m Model) scanning(dir string) bool {
	for _, run := range jobs.Unfinished(m.jobs) {
		if run.Kind != jobs.KindScan {
			continue
		}
		for _, item := range run.Items {
			if item.Target == dir && !item.State.Terminal() {
				return true
			}
		}
	}
	return false
}

// startScan launches a scan of the selected template.
func (m Model) startScan() (tea.Model, tea.Cmd) {
	if reason := m.refusal(m.availability().Scan); reason != "" {
		cmd := m.footer.Warn(reason)
		return m, cmd
	}
	entry, _ := m.selectedEntry()
	dir, err := template.ScanDir(entry.Slug)
	if err != nil {
		log.Printf("ERROR [templates] scan directory for %s: %v", entry.Slug, err)
		cmd := m.footer.Error("Could not scan the template — check logs")
		return m, cmd
	}

	job := scanJob{
		entry: entry,
		creds: template.CredentialsFor(m.config, m.secrets, entry.Source),
		opts:  m.scanOptions(),
		run:   m.scanner,
	}
	run := jobs.NewRun(jobs.KindScan, command.ViewTemplates, "", entry.Name, dir)
	return m, jobs.StartInContext(run, func(contextName string) tea.Cmd {
		return scanTemplateCmd(job, contextName)
	})
}

func (m Model) handleJobsChanged(msg jobs.ChangedMsg) (tea.Model, tea.Cmd) {
	m.jobs = msg.Runs
	m.footer.SetSpinnerFrame(msg.RenderedFrame)
	return m, nil
}

// handleScanComplete says how the scan ended. The counts are in the footer and
// the report is in `:sec`, where this directory is listed like any other scanned
// target.
func (m Model) handleScanComplete(msg ScanCompleteMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch {
	case msg.Err != nil:
		cmd = m.footer.Error("Scan of " + msg.Name + " failed — check logs")
	case msg.Partial:
		cmd = m.footer.Warn("Scanned " + msg.Name + ", but a scan stage failed — check logs")
	default:
		cmd = m.footer.Info(scanSummary(msg))
	}
	return m, cmd
}

// scanSummary is the one line a finished scan leaves.
func scanSummary(msg ScanCompleteMsg) string {
	c := msg.Counts
	if c.Critical+c.High+c.Medium+c.Low == 0 {
		return "Scanned " + msg.Name + " — no findings"
	}
	return fmt.Sprintf("Scanned %s — %d critical, %d high, %d medium, %d low — :sec for the report",
		msg.Name, c.Critical, c.High, c.Medium, c.Low)
}

// jobsStatus is the footer's derived line while work is live.
func (m Model) jobsStatus() sharedcomponents.Status {
	if text := sharedcomponents.JobsStatusLine(m.jobs, command.ViewTemplates); text != "" {
		return sharedcomponents.Status{Text: text, Spinner: true}
	}
	return sharedcomponents.Status{}
}
