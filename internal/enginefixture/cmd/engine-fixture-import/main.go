// Command engine-fixture-import is the workstation half of
// `mise run engine-contract:record` (OC-09a3, DEVDESK-944): it imports the
// directory the recorder wrote in a test VM into internal/enginefixture/fixtures,
// after checking it again, because nothing the VM produced is trusted. It reads
// files and writes files; it starts no process and reaches no engine.
//
//	engine-fixture-import --from <dir> [--into <dir>] [--versions <file>] [--require-denylist]
//
// --into defaults to the fixtures directory of the source tree, --versions to
// tools/engine-vms/versions.env. The external deny list is the file named by
// DEVDESK_FIXTURE_DENYLIST, and the identities of this machine are applied too.
// --require-denylist (what contract.sh record passes) refuses the import when that
// variable is unset or names a file with no active term: the names of an instance,
// its domains, projects and customers, are known to nobody but you.
// Exit codes: 0 done, 1 refused or failed (nothing written), 2 usage.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/fixtureleak"
)

const usage = "usage: engine-fixture-import --from <dir> [--into <dir>] [--versions <file>] [--require-denylist]"

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

// run is the command: it returns the exit code and writes nothing on stdout unless
// the import succeeded.
func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("engine-fixture-import", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	from := fs.String("from", "", "the directory the recorder wrote")
	into := fs.String("into", "", "the fixtures directory to write (default: the source tree's)")
	versions := fs.String("versions", "", "versions.env (default: the source tree's)")
	require := fs.Bool("require-denylist", false, "refuse the import without an external deny list with at least one term")
	if err := fs.Parse(args); err != nil {
		return usageError(stderr, err.Error())
	}
	if *from == "" || fs.NArg() > 0 {
		return usageError(stderr, "--from is required and takes a directory; no other argument is accepted")
	}

	supported, err := readSupported(*versions)
	if err != nil {
		return fail(stderr, err)
	}
	deny, note, err := fixtureleak.ReadDenylist(getenv)
	if err != nil {
		return fail(stderr, err)
	}
	if *require && len(deny) == 0 {
		return fail(stderr, errors.New("--require-denylist: "+fixtureleak.DenylistEnv+" must name a file with at least one term "+
			"(domains, project and customer names); "+note))
	}
	report, err := enginefixture.Import(enginefixture.ImportOptions{
		From: *from, Into: *into, Supported: supported, Deny: deny, Local: enginefixture.MachineIdentities(),
	})
	if err != nil {
		return fail(stderr, err)
	}

	_, _ = fmt.Fprintf(stdout, "engine-contract:record: %s: %d written, %d unchanged, %d kept, %d not produced by this run\n",
		report.Engine, len(report.Written), len(report.Unchanged), len(report.Kept), len(report.NotProduced))
	for _, list := range []struct {
		title string
		ids   []string
	}{
		{"written", report.Written}, {"unchanged", report.Unchanged}, {"kept", report.Kept}, {"not produced", report.NotProduced},
	} {
		for _, id := range list.ids {
			_, _ = fmt.Fprintf(stdout, "  %s: %s\n", list.title, id)
		}
	}
	_, _ = fmt.Fprintf(stdout, "deny list: %s\n", note)
	return 0
}

// readSupported reads versions.env: the file given, or the one of the source tree.
func readSupported(path string) (enginefixture.Supported, error) {
	if path == "" {
		var err error
		if path, err = enginefixture.VersionsSourceFile(); err != nil {
			return nil, fmt.Errorf("%w (pass --versions)", err)
		}
	}
	data, err := os.ReadFile(path) //nolint:gosec // the file the caller named, or the one of the source tree
	if err != nil {
		return nil, fmt.Errorf("versions file: %w", err)
	}
	return enginefixture.ParseVersions(data)
}

func fail(stderr io.Writer, err error) int {
	_, _ = fmt.Fprintf(stderr, "engine-fixture-import: %v\n", err)
	return 1
}

func usageError(stderr io.Writer, why string) int {
	_, _ = fmt.Fprintf(stderr, "engine-fixture-import: %s\n%s\n", why, usage)
	return 2
}
