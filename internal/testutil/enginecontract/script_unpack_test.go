package enginecontract_test

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// OC-09a3 (DEVDESK-944), review finding 1 and security finding B4: the tar a test VM
// sends back is untrusted. tools/engine-vms/unpack_recorded.sh, which contract.sh
// sources, lists it and refuses it as a whole, then extracts it into a fresh private
// directory. These tests source that file in bash, give it archives built here (Go's
// archive/tar for the booby traps, GNU tar for a sparse member) in a temporary
// directory, and check that every trap is refused and nothing is extracted. No VM, no
// network. Skipped without bash or GNU tar.

const maxArchive = 16 * 1024 * 1024

// unpack sources unpack_recorded.sh with work as the work directory, the archive being
// work/recorded-out, and returns the combined output, the exit code and the directory
// the archive was extracted to (empty when it was refused).
func unpack(t *testing.T, work string) (out string, code int, recorded string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the VM scripts are bash")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash on this machine")
	}
	requireGNUTar(t)
	script := filepath.Join(repoRoot(t), "tools", "engine-vms", "unpack_recorded.sh")
	const driver = `set -euo pipefail
die() { echo "DIE: $*" >&2; exit 1; }
work=$1
. "$2"
unpack_recorded
echo "RECORDED=$recorded"`
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bash, "-c", driver, "bash", work, script) //nolint:gosec // a fixed driver, paths as arguments
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "LC_ALL=C"}
	raw, err := cmd.CombinedOutput()
	out = string(raw)
	var exit *exec.ExitError
	switch {
	case err == nil:
		code = 0
	case errors.As(err, &exit):
		code = exit.ExitCode()
	default:
		t.Fatalf("running the driver: %v", err)
	}
	for line := range strings.SplitSeq(out, "\n") {
		if p, ok := strings.CutPrefix(line, "RECORDED="); ok {
			recorded = p
		}
	}
	return out, code, recorded
}

func requireGNUTar(t *testing.T) {
	t.Helper()
	tarBin, err := exec.LookPath("tar")
	if err != nil {
		t.Skip("no tar on this machine")
	}
	version, err := exec.Command(tarBin, "--version").Output() //nolint:gosec // tar found on PATH
	if err != nil || !strings.Contains(string(version), "GNU tar") {
		t.Skip("GNU tar is needed: the listing the script reads is GNU's")
	}
}

// member is one entry of an archive built here.
type member struct {
	hdr  tar.Header
	body string
}

func file(name, body string) member {
	return member{hdr: tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o644, Size: int64(len(body))}, body: body}
}

func dir(name string) member {
	return member{hdr: tar.Header{Typeflag: tar.TypeDir, Name: name, Mode: 0o755}}
}

// workWith writes the members as work/recorded-out and returns the work directory.
func workWith(t *testing.T, members ...member) string {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	for _, m := range members {
		h := m.hdr
		if err := w.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(m.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return workWithBytes(t, buf.Bytes())
}

func workWithBytes(t *testing.T, data []byte) string {
	t.Helper()
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "recorded-out"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return work
}

// extractedNothing checks that no directory was made for the extraction, and that
// nothing but the archive is in the work directory.
func extractedNothing(t *testing.T, work string) {
	t.Helper()
	entries, err := os.ReadDir(work)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "recorded-out" {
			t.Errorf("%s was made in the work directory although the archive was refused", e.Name())
		}
	}
}

func TestUnpackRecordedExtractsAGoodArchiveIntoAPrivateDirectory(t *testing.T) {
	work := workWith(t,
		dir("./"), dir("./containers/"),
		file("./containers/rm-running--podman-6.2.1-rootless.cli", "hello\n"),
		file("./networks/ls--podman-6.2.1-rootless.cli", "x\n"),
	)

	out, code, recorded := unpack(t, work)

	if code != 0 || recorded == "" {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}
	if filepath.Dir(recorded) != work {
		t.Errorf("extracted to %s, want a new directory under the work directory %s", recorded, work)
	}
	data, err := os.ReadFile(filepath.Join(recorded, "containers", "rm-running--podman-6.2.1-rootless.cli"))
	if err != nil || string(data) != "hello\n" {
		t.Errorf("the file was not extracted: %q, %v", data, err)
	}
	err = filepath.WalkDir(recorded, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if info, _ := d.Info(); info.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s has mode %v: group and others must have no access", p, info.Mode())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUnpackRecordedRefusesAnArchiveWithATrap(t *testing.T) {
	good := file("./containers/a--podman-6.2.1-rootless.cli", "x\n")
	tests := []struct {
		name    string
		members []member
		want    string
	}{
		{"a symbolic link", []member{good, {hdr: tar.Header{Typeflag: tar.TypeSymlink, Name: "./containers/l.cli", Linkname: "/etc/passwd"}}}, "link"},
		{"a hard link", []member{good, {hdr: tar.Header{Typeflag: tar.TypeLink, Name: "./containers/h.cli", Linkname: "./containers/a--podman-6.2.1-rootless.cli"}}}, "link"},
		{"a FIFO", []member{good, {hdr: tar.Header{Typeflag: tar.TypeFifo, Name: "./containers/f.cli", Mode: 0o644}}}, "special"},
		{"a character device", []member{good, {hdr: tar.Header{Typeflag: tar.TypeChar, Name: "./containers/c.cli", Mode: 0o644}}}, "special"},
		{"a name that climbs", []member{file("../escape/a--podman-6.2.1-rootless.cli", "x\n")}, "not <campaign>"},
		{"a name that climbs in the middle", []member{file("./containers/../../x.cli", "x\n")}, "not <campaign>"},
		{"an absolute path", []member{file("/tmp/escaped/a--podman-6.2.1-rootless.cli", "x\n")}, "not <campaign>"},
		{"a name with a line break", []member{file("./containers/a\nb.cli", "x\n")}, "not <campaign>"},
		{"a file that is not a fixture", []member{good, file("./containers/x.txt", "hello\n")}, "not <campaign>"},
		{"a file at the root", []member{file("./a--podman-6.2.1-rootless.cli", "x\n")}, "not <campaign>"},
		{"a directory in a campaign", []member{good, dir("./containers/sub/")}, "not <campaign>"},
		{"an upper-case campaign", []member{file("./Containers/a--podman-6.2.1-rootless.cli", "x\n")}, "not <campaign>"},
		{"a space in a name", []member{file("./containers/a b.cli", "x\n")}, "not <campaign>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			work := workWith(t, tt.members...)

			out, code, recorded := unpack(t, work)

			if code == 0 || recorded != "" || !strings.Contains(out, "refused") || !strings.Contains(out, tt.want) {
				t.Errorf("exit %d, recorded %q, output:\n%s\nwant a refusal that mentions %q", code, recorded, out, tt.want)
			}
			extractedNothing(t, work)
			for _, escaped := range []string{filepath.Join(work, "..", "escape"), "/tmp/escaped"} {
				if _, err := os.Lstat(escaped); err == nil {
					t.Errorf("%s exists: a member was extracted outside the directory", escaped)
				}
			}
		})
	}
}

func TestUnpackRecordedRefusesAnArchiveThatIsTooLarge(t *testing.T) {
	// A regular file of just over 16 MiB: the archive itself is over the cap.
	work := workWith(t, file("./containers/big--podman-6.2.1-rootless.cli", strings.Repeat("x", maxArchive+1)))

	out, code, recorded := unpack(t, work)

	if code == 0 || recorded != "" || !strings.Contains(out, "larger than") {
		t.Errorf("exit %d, recorded %q, output:\n%s\nwant a refusal for the size of the archive", code, recorded, out)
	}
	extractedNothing(t, work)
}

// A sparse member is a few kilobytes in the archive and gigabytes once extracted: the
// size the listing announces is what counts.
func TestUnpackRecordedRefusesASparseMemberThatAnnouncesMoreThanTheCap(t *testing.T) {
	requireGNUTar(t)
	for name, sizes := range map[string][]int64{
		"one member of 40 MiB":            {40 << 20},
		"just over the cap":               {maxArchive + 1},
		"two members that add up to more": {9 << 20, 9 << 20},
	} {
		t.Run(name, func(t *testing.T) {
			src := t.TempDir()
			if err := os.MkdirAll(filepath.Join(src, "containers"), 0o755); err != nil {
				t.Fatal(err)
			}
			for i, size := range sizes {
				p := filepath.Join(src, "containers", "s"+string(rune('a'+i))+"--podman-6.2.1-rootless.cli")
				if err := os.WriteFile(p, nil, 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Truncate(p, size); err != nil {
					t.Fatal(err)
				}
			}
			archive, err := exec.Command("tar", "--sparse", "-C", src, "-cf", "-", ".").Output() //nolint:gosec // fixed arguments
			if err != nil {
				t.Fatalf("tar --sparse: %v", err)
			}
			if len(archive) > 1<<20 {
				t.Skipf("this file system does not keep the holes: the archive is %d bytes", len(archive))
			}
			work := workWithBytes(t, archive)

			out, code, recorded := unpack(t, work)

			if code == 0 || recorded != "" || !strings.Contains(out, "announces more than") {
				t.Errorf("exit %d, recorded %q, output:\n%s\nwant a refusal for the announced size", code, recorded, out)
			}
			extractedNothing(t, work)
		})
	}
}

// A sparse member under the cap, and the cap itself, are accepted.
func TestUnpackRecordedAcceptsASparseMemberUnderTheCap(t *testing.T) {
	requireGNUTar(t)
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "containers"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(src, "containers", "s--podman-6.2.1-rootless.cli")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(p, maxArchive); err != nil {
		t.Fatal(err)
	}
	archive, err := exec.Command("tar", "--sparse", "-C", src, "-cf", "-", ".").Output() //nolint:gosec // fixed arguments
	if err != nil {
		t.Fatal(err)
	}

	out, code, recorded := unpack(t, workWithBytes(t, archive))

	if code != 0 || recorded == "" {
		t.Errorf("exit %d, output:\n%s\nwant the archive accepted: exactly the cap is allowed", code, out)
	}
}

func TestUnpackRecordedRefusesWhatIsNotAnArchive(t *testing.T) {
	work := workWithBytes(t, []byte("this is not a tar archive\n"))

	out, code, recorded := unpack(t, work)

	if code == 0 || recorded != "" || !strings.Contains(out, "cannot be read") {
		t.Errorf("exit %d, recorded %q, output:\n%s", code, recorded, out)
	}
	extractedNothing(t, work)
}
