package netdiag

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/docker"
)

const sourcesLogMarker = "ERROR [netdiag/path] sources:"

func loadSourcesMsg(t *testing.T, gen int, timeout time.Duration, e pathEngine) pathSourcesMsg {
	t.Helper()
	msg, ok := loadPathSourcesCmd(gen, timeout, e)().(pathSourcesMsg)
	if !ok {
		t.Fatalf("loadPathSourcesCmd did not return a pathSourcesMsg")
	}
	return msg
}

func TestLoadPathSourcesCarriesTheRequestedGen(t *testing.T) {
	f := &fakeEngine{psRows: []docker.Container{{ID: "aaaaaaaaaaaa", Name: "api", Image: "app", State: "running"}}}

	msg := loadSourcesMsg(t, 7, time.Second, f.engine())

	if msg.gen != 7 {
		t.Fatalf("gen = %d, want 7", msg.gen)
	}
	if len(msg.list.Containers) != 1 || msg.list.Containers[0].Name != "api" {
		t.Fatalf("list = %#v, want the fake's running container", msg.list)
	}
}

func TestLoadPathSourcesReadsUnderADeadline(t *testing.T) {
	f := &fakeEngine{}

	loadSourcesMsg(t, 1, time.Minute, f.engine())

	if f.osCtx == nil {
		t.Fatal("engineOS was never called")
	}
	if _, ok := f.osCtx.Deadline(); !ok {
		t.Fatal("the context handed to the engine reads has no deadline")
	}
}

func TestLoadPathSourcesInterruptsABlockedRead(t *testing.T) {
	logs := captureLog(t)
	f := &fakeEngine{}
	e := f.engine()
	e.engineOS = func(ctx context.Context) (string, error) {
		<-ctx.Done() // a hung `docker info` stops on the context only
		return "", ctx.Err()
	}

	done := make(chan pathSourcesMsg, 1)
	go func() { done <- loadSourcesMsg(t, 3, 50*time.Millisecond, e) }()

	select {
	case msg := <-done:
		if msg.gen != 3 {
			t.Fatalf("gen = %d, want 3", msg.gen)
		}
		if msg.list.Note != noteNoEngine || len(msg.list.Containers) != 0 {
			t.Fatalf("list = %#v, want this machine only with %q", msg.list, noteNoEngine)
		}
	case <-time.After(producerWait):
		t.Fatal("a blocked engine read was not interrupted by the timeout")
	}
	if !strings.Contains(logs.String(), sourcesLogMarker) {
		t.Fatalf("a deadline exceeded on its own must be logged, got %q", logs.String())
	}
}

func TestListPathSourcesCanceledParentLogsNothing(t *testing.T) {
	logs := captureLog(t)
	f := &fakeEngine{osErr: errors.New("context canceled")}
	parent, cancel := context.WithCancel(context.Background())
	cancel()

	listPathSourcesUnder(parent, time.Second, f.engine())

	if strings.Contains(logs.String(), sourcesLogMarker) {
		t.Fatalf("a canceled list must not be logged, got %q", logs.String())
	}
}

func TestListPathSourcesDeadlineAloneIsLogged(t *testing.T) {
	logs := captureLog(t)
	f := &fakeEngine{osErr: errors.New("context deadline exceeded")}

	listPathSourcesUnder(context.Background(), time.Second, f.engine())

	if !strings.Contains(logs.String(), sourcesLogMarker) {
		t.Fatalf("an error with a live parent must be logged, got %q", logs.String())
	}
}
