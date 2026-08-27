package scan

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// plumber reads a repository's CI configuration — GitHub workflows, .gitlab-ci
// — through a Rego policy engine and grades it: a letter from A to E, points
// out of 100, and the issues that explain them (§3.42).
//
// Everything here is measured on plumber 0.4.40 and 0.4.42, by real runs. The
// section it implements was twice wrong about this tool from reading its
// documentation, so nothing below is inferred.

// Exit codes. Four, not three: `3` says the analysis ran on incomplete data and
// the score is withheld, and it is the one that matters most here — it maps
// exactly onto the `?` state of the CI column, without anyone having to read
// dataCollectionDegraded out of the JSON to find out.
const (
	plumberGateFailed    = 1 // a score exists and is below --min-points
	plumberRuntimeError  = 2 // no token, bad config, unreachable host, no repository
	plumberScoreWithheld = 3 // it ran, and says it cannot conclude
)

// plumberConfigMount is where a configured rules file is mounted inside the
// container, on §3.50's model: the argument plumber sees is never the host path.
//
// The container root is free of everything except /plumber, the binary itself
// (checked on getplumber/plumber). The repository is mounted at
// containerScanPath, so no file of it can land beside this one.
const plumberConfigMount = "/plumber.yaml"

// plumberSafeDirectory is what lets a containerised plumber read the mounted
// repository at all.
//
// The image runs as uid 65532, so git refuses the mount with `fatal: detected
// dubious ownership in repository at '/scan'` and plumber answers *could not
// determine the provider: not in a git repository*. Passing it through the
// environment rather than a config file is what keeps the mount read-only and
// the user non-root; `--user` is not portable to Windows hosts. Measured, not
// deduced: `--provider` alone does **not** lift it.
var plumberSafeDirectory = []string{
	"-e", "GIT_CONFIG_COUNT=1",
	"-e", "GIT_CONFIG_KEY_0=safe.directory",
	"-e", "GIT_CONFIG_VALUE_0=" + containerScanPath,
}

// PlumberOptions is what one plumber run needs beyond the tool itself.
type PlumberOptions struct {
	// Provider is the forge this context targets, declared rather than sniffed
	// from the remote: config.ForgeGitLab or config.ForgeGitHub.
	Provider string
	// Branch is the repository's current branch. It is passed and the failure
	// accepted: a score describing the default branch when the user is looking
	// at another one would be right about something they did not ask about.
	// The flag has no effect on the GitHub local path, which reads the tree.
	Branch string
	// ConfigPath is scan.plumber_config, already absolute (config.ExpandPaths).
	ConfigPath string
	// Token is the context's forge token. It reaches plumber through the
	// environment, never argv, which is readable from the process list.
	Token string
	// GitLabURL is the instance to talk to on the GitLab path.
	GitLabURL string
}

// plumberArgs builds the invocation.
//
// The JSON leaves by two different routes, and the asymmetry is measured rather
// than chosen: in a container `--output /dev/stdout` works, and for a native
// Windows binary it writes nothing at all — there is no such path. So the
// binary mode writes to a file the caller passes in and deletes.
//
// `--print=false` is what keeps stdout pure JSON: the human report and the
// score banner both go there otherwise. The tool's own logs go to stderr, which
// is where progressFn reads them.
func plumberArgs(target string, tool ToolSpec, opts PlumberOptions, outPath string) toolCmd {
	docker := tool.Source == ToolSourceDocker

	configFlag := opts.ConfigPath
	if docker && opts.ConfigPath != "" {
		configFlag = plumberConfigMount
	}

	options := []string{"analyze", "--score", "--print=false", "--output", outPath}
	if opts.Provider != "" {
		options = append(options, "--provider", opts.Provider)
	}
	if opts.Branch != "" {
		options = append(options, "--branch", opts.Branch)
	}
	if opts.GitLabURL != "" {
		options = append(options, "--gitlab-url", opts.GitLabURL)
	}
	if opts.ConfigPath != "" {
		options = append(options, "--config", configFlag)
	}

	if docker {
		args := []string{"run", "--rm", "-v", target + ":" + containerScanPath + ":ro"}
		if opts.ConfigPath != "" {
			// Before the image name: everything after it is plumber's own argv.
			args = append(args, "-v", opts.ConfigPath+":"+plumberConfigMount+":ro")
		}
		args = append(args, plumberSafeDirectory...)
		args = append(args, plumberTokenEnv(opts)...)
		args = append(args, "-w", containerScanPath, plumberImage(tool.Image))
		return toolCmd{Name: "docker", Args: append(args, options...)}
	}

	return toolCmd{Name: plumberBinary(tool), Args: options}
}

// plumberTokenEnv passes the context's token to a containerised plumber.
//
// The variable differs by forge, and naming both would send a GitLab token to
// GitHub on a repository that answers to neither. An empty token declares
// nothing rather than an empty variable: plumber treats "set but empty" as an
// attempt, and its diagnostic is then about a rejected credential rather than
// about a missing one.
func plumberTokenEnv(opts PlumberOptions) []string {
	if opts.Token == "" {
		return nil
	}
	name := "GH_TOKEN"
	if opts.Provider == providerGitLab {
		name = "GITLAB_TOKEN"
	}
	return []string{"-e", name + "=" + opts.Token}
}

func plumberImage(image string) string {
	if image == "" {
		return DefaultPlumberImage
	}
	return image
}

func plumberBinary(tool ToolSpec) string {
	if tool.Binary != "" {
		return tool.Binary
	}
	return "plumber"
}

// providerGitLab and providerGitHub are the values --provider takes. They are
// stated here as well as in internal/config because scan must not import a
// forge backend and config must not be the authority on a tool's flags;
// TestThePlumberProviderVocabularyMatchesTheConfig holds the two in step, the
// same arrangement as the registry provider names.
const (
	providerGitLab = "gitlab"
	providerGitHub = "github"
)

// PlumberReport is what one run says about a repository, beyond its findings.
type PlumberReport struct {
	// Score is the letter, empty when it was withheld.
	Score string
	// Points is finalPoints out of 100.
	Points int
	// Withheld reports a run that could not conclude — exit 3, and
	// dataCollectionDegraded in the JSON. Its letter must never be shown: a
	// degraded run of the fixture reads B/79 where the complete run reads E/30,
	// because a control that did not run found nothing.
	Withheld bool
	// Reasons is why, when it was withheld.
	Reasons []string
	// CIMissing reports a repository with no pipeline at all, which is not a
	// bad score.
	CIMissing bool
	// Findings are the issues, with their severity joined on from the score
	// block.
	Findings []Finding
}

// RunPlumber executes plumber and returns what it found.
func RunPlumber(ctx context.Context, target string, tool ToolSpec, opts PlumberOptions, progressFn func(string)) (*PlumberReport, error) {
	if err := checkPlumberConfig(opts.ConfigPath); err != nil {
		return nil, err
	}

	outPath, read, cleanup, err := plumberOutput(tool)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	cmd := plumberArgs(target, tool, opts, outPath)
	if tool.Source != ToolSourceDocker && opts.Token != "" {
		cmd.Env = plumberBinaryEnv(opts)
	}

	stdout, runErr := runner.Run(ctx, cmd, progressFn)

	// Only exit 2 is a failure. 0 and 1 are verdicts — the gate is set at 100
	// points by default, so 1 is the common case — and 3 is plumber saying it
	// could not conclude, which is an answer this package carries rather than
	// an error it raises.
	if runErr != nil {
		var exit *exitError
		if !errors.As(runErr, &exit) || exit.Code == plumberRuntimeError {
			return nil, fmt.Errorf("plumber failed: %w", runErr)
		}
		if exit.Code != plumberGateFailed && exit.Code != plumberScoreWithheld {
			return nil, fmt.Errorf("plumber failed: %w", runErr)
		}
	}

	data, err := read(stdout)
	if err != nil {
		return nil, fmt.Errorf("plumber wrote no report: %w", err)
	}
	return parsePlumberOutput(data)
}

// plumberOutput decides where the JSON goes, and how to get it back.
//
// See plumberArgs: a container writes it to stdout, a native binary to a file,
// because a Windows plumber writes nothing at all to /dev/stdout (measured).
func plumberOutput(tool ToolSpec) (path string, read func([]byte) ([]byte, error), cleanup func(), err error) {
	if tool.Source == ToolSourceDocker {
		return "/dev/stdout", func(stdout []byte) ([]byte, error) {
			if len(stdout) == 0 {
				return nil, errors.New("empty stdout")
			}
			return stdout, nil
		}, func() {}, nil
	}

	f, err := os.CreateTemp("", "devdesk-plumber-*.json")
	if err != nil {
		return "", nil, func() {}, fmt.Errorf("plumber report file: %w", err)
	}
	name := f.Name()
	_ = f.Close()
	return name, func([]byte) ([]byte, error) { return os.ReadFile(name) }, func() { _ = os.Remove(name) }, nil
}

// plumberBinaryEnv hands the token to a plumber running on this machine. Same
// rule as the container's: one variable, chosen by provider.
func plumberBinaryEnv(opts PlumberOptions) []string {
	name := "GH_TOKEN"
	if opts.Provider == providerGitLab {
		name = "GITLAB_TOKEN"
	}
	return append(os.Environ(), name+"="+opts.Token)
}

// checkPlumberConfig refuses a rules file that cannot be read, before anything
// starts — §3.50's guard, for §3.50's reason. It is not there to catch a bad
// configuration: plumber says that itself, loudly, and exits 2. It is there
// because `docker run -v` on a host path that does not exist does not fail, it
// creates a directory at that path and mounts it.
func checkPlumberConfig(path string) error {
	if path == "" {
		return nil
	}
	f, err := os.Open(path) //nolint:gosec // the path is the user's own setting
	if err != nil {
		return fmt.Errorf("plumber config: %w", err)
	}
	return f.Close()
}

// GetPlumberCommand returns the command that would be executed, for display and
// logging. Built by the same builder as the executed one, so the two cannot
// drift apart — and with the token left out, because this string is logged.
func GetPlumberCommand(target string, tool ToolSpec, opts PlumberOptions) string {
	opts.Token = ""
	return plumberArgs(target, tool, opts, "<report>").String()
}
