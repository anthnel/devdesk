package security

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/scan"
	ociresources "github.com/anthnel/devdesk/internal/ui/oci_resources"
	"github.com/anthnel/devdesk/internal/ui/workspaces"
)

// startScan initiates the security scan.
// If returnToOCIImages is set, it sends configured options back to OCI images view instead.
func (m Model) startScan() (tea.Model, tea.Cmd) {
	if m.targetPath == "" {
		m.err = fmt.Errorf("target path is required")
		return m, nil
	}
	// Trivy fails the whole scan on an address it cannot parse, and the field is
	// one stray ":" away from holding one. Refusing here says so while the user
	// is still looking at the field.
	if err := scan.ValidateTrivyServer(m.trivyServerInput.Value()); err != nil {
		m.err = err
		return m, nil
	}

	opts := scan.ScanOptions{
		EnableVuln:      m.enableVuln,
		EnableSecret:    m.enableSecret,
		EnableMisconfig: m.enableMisconfig,
		EnableLicense:   m.enableLicense,
		GenerateSBOM:    m.generateSBOM,
		SBOMOutputDir:   m.config.Scan.SBOMOutputDir,
		TrivyImage:      m.config.Scan.TrivyImage,
		GitleaksImage:   m.config.Scan.GitleaksImage,
		TrivyServer:     m.trivyServerInput.Value(),
		IgnoreUnfixed:   m.ignoreUnfixed,
		IgnoreEOL:       m.ignoreEOL,
		GitleaksHistory: m.gitleaksHistory,
		GitleaksConfig:  m.gitleaksConfigInput.Value(),
	}

	// Persist all options to config (may already be up to date if user toggled before scanning)
	m.saveOptionsToConfig()

	// When opened from OCI images view, delegate scan execution back to that view
	if m.isImageScan && m.returnToOCIImages {
		if m.targetPath == "all" {
			capturedOpts := opts
			return m, func() tea.Msg { return ociresources.LaunchBatchScanMsg{Opts: capturedOpts} }
		}
		capturedOpts := opts
		capturedName := m.targetPath
		return m, func() tea.Msg {
			return ociresources.LaunchSingleImageScanMsg{ImageName: capturedName, Opts: capturedOpts}
		}
	}

	m.state = StateScanning
	m.err = nil
	m.scanGen++
	m.scanStartTime = time.Now()
	m.scanStages = nil

	ctx, cancel := context.WithCancel(context.Background())
	m.cancelScan = cancel

	ch := make(chan scan.ProgressUpdate, 32)
	m.progressCh = ch
	gen := m.scanGen
	targetPath := m.targetPath
	targetType := m.getTargetType()
	opts.OnProgress = func(update scan.ProgressUpdate) {
		select {
		case ch <- update:
		default: // don't block if buffer full
		}
	}

	return m, tea.Batch(
		m.spinner.Tick,
		purgeScanCacheCmd(targetPath, targetType),
		waitForProgressCmd(ch),
		func() tea.Msg {
			defer cancel()
			defer close(ch)
			scanner := scan.NewScanner(opts)
			result, err := scanner.Scan(ctx, targetPath, targetType)
			return ScanCompleteMsg{Result: result, Error: err, Gen: gen}
		},
	)
}

// waitForProgressCmd returns a Cmd that blocks until the next ProgressUpdate is available on ch.
// Returns nil when the channel is closed (scan complete).
func waitForProgressCmd(ch <-chan scan.ProgressUpdate) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		update, ok := <-ch
		if !ok {
			return nil // channel closed, scan finished
		}
		return ScanProgressMsg{Update: update}
	}
}

// purgeScanCacheCmd deletes the existing cache entries for the target before re-scanning,
// so that stale results don't persist if the scan is interrupted or fails.
// For workspace (directory) targets, both the counts cache and the full result file are removed.
func purgeScanCacheCmd(target string, targetType scan.TargetType) tea.Cmd {
	return func() tea.Msg {
		switch targetType {
		case scan.TargetDirectory:
			if c, err := cache.NewWorkspaceScanCache(config.CurrentContextName()); err == nil {
				_ = c.Delete(target)
			}
			_ = cache.DeleteWorkspaceScanResult(target)
		case scan.TargetImage:
			if c, err := cache.NewImageScanCache(config.CurrentContextName()); err == nil {
				_ = c.Delete(target)
			}
		}
		return nil
	}
}

// getTargetType converts string to TargetType
func (m Model) getTargetType() scan.TargetType {
	switch m.targetType {
	case "image":
		return scan.TargetImage
	default:
		return scan.TargetDirectory
	}
}

// cancelCurrentScan cancels the running scan and returns to StateInput.
// The goroutine will still complete and send ScanCompleteMsg, but it will be
// discarded because scanGen is incremented here.
func (m Model) cancelCurrentScan() (tea.Model, tea.Cmd) {
	if m.cancelScan != nil {
		m.cancelScan()
		m.cancelScan = nil
	}
	m.scanGen++ // invalidate any pending ScanCompleteMsg from the cancelled goroutine
	m.state = StateInput
	return m, nil
}

// handleScanProgress updates the per-stage progress state and schedules the next read.
func (m Model) handleScanProgress(msg ScanProgressMsg) (tea.Model, tea.Cmd) {
	u := msg.Update
	for i, s := range m.scanStages {
		if s.Stage == u.Stage {
			m.scanStages[i] = u
			return m, waitForProgressCmd(m.progressCh)
		}
	}
	// New stage: append in arrival order
	m.scanStages = append(m.scanStages, u)
	return m, waitForProgressCmd(m.progressCh)
}

// handleScanComplete processes scan results
func (m Model) handleScanComplete(msg ScanCompleteMsg) (tea.Model, tea.Cmd) {
	if msg.Gen != m.scanGen {
		return m, nil // stale result from a cancelled scan, discard
	}
	if msg.Error != nil {
		m.err = msg.Error
		m.state = StateInput
		return m, nil
	}

	m.result = msg.Result
	m.state = StateResults
	m.updateFindingsTable()

	// Cache CVE counts and full result for image scans (Rule 126: Enter loads from cache)
	if m.targetType == "image" && m.result != nil {
		result := m.result
		path := m.targetPath
		go func() {
			saveImageScanToCache(path, result)
			_ = cache.SaveImageScanResult(path, result)
		}()
	}

	// For workspace scans: persist full result and send summary back to workspaces view
	if !m.isImageScan && m.returnToWorkspaces && m.result != nil {
		result := m.result
		path := m.targetPath
		go func() { _ = cache.SaveWorkspaceScanResult(path, result) }()
		sensitive := hasScanSource(result, "gitleaks")
		return m, func() tea.Msg {
			return workspaces.WorkspaceScanCompleteMsg{
				RepoPath:  path,
				Critical:  result.Counts.Critical,
				High:      result.Counts.High,
				Medium:    result.Counts.Medium,
				Low:       result.Counts.Low,
				Sensitive: sensitive,
				ScannedAt: result.EndTime,
			}
		}
	}

	return m, nil
}

// hasScanSource returns true if any finding in the result comes from the given source
func hasScanSource(result *scan.Result, source string) bool {
	for _, f := range result.Findings {
		if f.Source == source {
			return true
		}
	}
	return false
}

// saveImageScanToCache persists CVE severity counts to the image scan cache
func saveImageScanToCache(imageName string, result *scan.Result) {
	c, err := cache.NewImageScanCache(config.CurrentContextName())
	if err != nil {
		return
	}
	_ = c.Set(imageName, cache.ImageScanEntry{
		Critical:  result.Counts.Critical,
		High:      result.Counts.High,
		Medium:    result.Counts.Medium,
		Low:       result.Counts.Low,
		ScannedAt: result.EndTime,
	})
}
