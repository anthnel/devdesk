package imagesearch

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

// Review fixes for the batch memo (DEVDESK-311, step 1).

// A panicking leader must still propagate its panic, and nobody else may
// read that as "success, empty catalog" (now or later in the batch).
func TestSharedWalkLeaderPanicIsAnErrorForOthers(t *testing.T) {
	gate := make(chan struct{})
	entered := make(chan struct{}, 1)
	panicking := func(context.Context, string, string, string, int, int) ([]string, bool, error) {
		entered <- struct{}{}
		<-gate
		panic("boom")
	}
	ctx := WithSharedWalks(context.Background())
	search := func(key string) ([]Image, bool, error) {
		return CatalogSearcher{List: panicking}.Search(ctx, member(key, "", "u", "p"), Query{Text: "app"})
	}

	leaderPanic := make(chan any, 1)
	go func() {
		defer func() { leaderPanic <- recover() }()
		_, _, _ = search("leader")
	}()
	<-entered

	type res struct {
		err error
		rec any
	}
	follower := make(chan res, 1)
	go func() {
		var r res
		defer func() { r.rec = recover(); follower <- r }()
		_, _, r.err = search("follower")
	}()
	close(gate)

	if got := <-leaderPanic; got != "boom" {
		t.Fatalf("leader recover() = %v, want the original panic to propagate", got)
	}
	fr := <-follower
	if fr.rec != nil {
		t.Fatalf("follower panicked: %v", fr.rec)
	}
	if fr.err == nil {
		t.Fatal("follower err = nil: a leader panic was shared as success")
	}
	if v := Classify(fr.err, 0, false).Verdict; v != VerdictFailed {
		t.Errorf("follower verdict = %v, want failed", v)
	}

	// A member arriving after the panic must not see a memoized empty success.
	var later res
	func() {
		defer func() { later.rec = recover() }()
		_, _, later.err = search("later")
	}()
	if later.rec == nil && later.err == nil {
		t.Fatal("later member got success after the leader panicked")
	}
	if later.rec == nil {
		if v := Classify(later.err, 0, false).Verdict; v != VerdictFailed {
			t.Errorf("later verdict = %v, want failed", v)
		}
	}
}

// A leader's own cancellation is not a fact about the catalog: it must not be
// memoized for live members of the batch.
func TestSharedWalkLeaderCancelNotMemoized(t *testing.T) {
	var calls atomic.Int32
	entered := make(chan struct{}, 1)
	list := func(ctx context.Context, _, _, _ string, _, _ int) ([]string, bool, error) {
		if calls.Add(1) == 1 {
			entered <- struct{}{}
			<-ctx.Done()
			return nil, false, ctx.Err()
		}
		return []string{"app/one"}, false, nil
	}
	batch := WithSharedWalks(context.Background())
	leaderCtx, cancel := context.WithCancel(batch)

	done := make(chan error, 1)
	go func() {
		_, _, err := CatalogSearcher{List: list}.Search(leaderCtx, member("leader", "", "u", "p"), Query{Text: "app"})
		done <- err
	}()
	<-entered
	cancel()
	if err := <-done; err == nil {
		t.Fatal("leader err = nil, want its context error")
	}

	imgs, _, err := CatalogSearcher{List: list}.Search(batch, member("later", "", "u", "p"), Query{Text: "app"})
	if err != nil {
		t.Fatalf("live later member err = %v, want its own successful walk", err)
	}
	if len(imgs) != 1 {
		t.Errorf("live later member images = %d, want 1", len(imgs))
	}
	if calls.Load() != 2 {
		t.Errorf("walks = %d, want 2 (the cancelled one is not reused)", calls.Load())
	}
}

// A userinfo in the API URL is a credential: it must not appear in any
// formatting of the key, yet two userinfos must still be two keys.
func TestWalkKeyNeverFormatsURLUserinfo(t *testing.T) {
	const user, pass = "alice-user", "hunter2-secret"
	s := &walkShare{}
	mk := func(u string) Target {
		tg := member("k", "", "", "")
		tg.APIURL = u
		return tg
	}
	key := s.keyFor(mk("https://" + user + ":" + pass + "@reg.example/"))
	outs := map[string]string{"String()": key.String(), "GoString()": key.GoString()}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		outs[verb+" key"] = fmt.Sprintf(verb, key)
		outs[verb+" &key"] = fmt.Sprintf(verb, &key)
	}
	for label, out := range outs {
		for _, leak := range []string{user, pass} {
			if strings.Contains(out, leak) {
				t.Errorf("%s leaks %q: %s", label, leak, out)
			}
		}
	}

	a := s.keyFor(mk("https://alice:one@reg.example/"))
	b := s.keyFor(mk("https://alice:two@reg.example/"))
	c := s.keyFor(mk("https://bob:one@reg.example/"))
	if a == b || a == c {
		t.Error("URLs with different userinfo must give different keys")
	}
	if a != s.keyFor(mk("https://alice:one@reg.example")) {
		t.Error("trailing slash must still be ignored")
	}
}
