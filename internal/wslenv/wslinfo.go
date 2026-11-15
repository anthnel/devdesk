package wslenv

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	// wslinfoTimeout bounds the child process.
	wslinfoTimeout = 2 * time.Second
	// wslinfoMaxOutput bounds what is read from the child and returned.
	wslinfoMaxOutput = 64
	// wslinfoWaitDelay is how long Run waits for I/O after the process is killed.
	wslinfoWaitDelay = 500 * time.Millisecond
)

// wslinfoNetworkingMode runs `wslinfo --networking-mode` and returns its
// cleaned output. The binary is looked up at two fixed absolute paths, never
// through the search path.
func wslinfoNetworkingMode(ctx context.Context) (string, error) {
	path, err := findWslinfo()
	if err != nil {
		return "", err
	}
	return runWslinfo(ctx, path)
}

// boundedBuffer keeps the first wslinfoMaxOutput bytes written and drops the
// rest, so a chatty child can neither grow memory nor block on a full pipe.
type boundedBuffer struct{ buf []byte }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if room := wslinfoMaxOutput - len(b.buf); room > 0 {
		b.buf = append(b.buf, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

// runWslinfo runs the binary at path with an empty environment, a 2 s deadline
// and bounded output. A done context takes precedence over the process error.
func runWslinfo(ctx context.Context, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, wslinfoTimeout)
	defer cancel()
	out := &boundedBuffer{}
	cmd := exec.CommandContext(ctx, path, "--networking-mode")
	cmd.Env = []string{}
	cmd.Stdout = out
	cmd.WaitDelay = wslinfoWaitDelay
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", err
	}
	return parseWslinfoOutput(out.buf), nil
}

// findWslinfo returns the first wslinfo binary that exists.
func findWslinfo() (string, error) {
	for _, p := range []string{"/usr/bin/wslinfo", "/usr/sbin/wslinfo"} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", errors.New("wslinfo not found")
}

// parseWslinfoOutput bounds and cleans the output of `wslinfo --networking-mode`.
func parseWslinfoOutput(out []byte) string {
	s := strings.TrimSpace(string(out))
	if len(s) > wslinfoMaxOutput {
		s = s[:wslinfoMaxOutput]
	}
	return s
}
