package imagesearch

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/oci"
)

// These tests pin the batch memo (DEVDESK-311, IS-07): members of one batch
// that reach the same catalog with the same identity share one walk.

// countingLister counts its calls; when gate is set, each call blocks on it
// (after signalling entered) so a test can force concurrency.
type countingLister struct {
	names     []string
	truncated bool
	err       error

	calls   atomic.Int32
	entered chan struct{}
	gate    chan struct{}
	mu      sync.Mutex
	seen    []string // apiURL|user|pass per call
}

func (f *countingLister) list(ctx context.Context, apiURL, user, pass string, _, _ int) ([]string, bool, error) {
	f.calls.Add(1)
	f.mu.Lock()
	f.seen = append(f.seen, apiURL+"|"+user+"|"+pass)
	f.mu.Unlock()
	if f.entered != nil {
		select {
		case f.entered <- struct{}{}:
		default:
		}
	}
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}
	return f.names, f.truncated, f.err
}

func member(key, prefix, user, pass string) Target {
	return Target{EntryKey: key, APIURL: "https://reg.example/", RepoPrefix: prefix, Username: user, Password: pass}
}

type result struct {
	imgs    []Image
	partial bool
	err     error
}

func searchAll(ctx context.Context, f *countingLister, targets []Target, text string) []result {
	out := make([]result, len(targets))
	var wg sync.WaitGroup
	for i, tg := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			imgs, partial, err := CatalogSearcher{List: f.list}.Search(ctx, tg, Query{Text: text})
			out[i] = result{imgs, partial, err}
		}()
	}
	wg.Wait()
	return out
}

func TestSharedWalkParallelMembersOneCall(t *testing.T) {
	f := &countingLister{
		names:   []string{"a/app", "b/app", "c/app", "a/api", "other/app"},
		entered: make(chan struct{}, 8),
		gate:    make(chan struct{}),
	}
	ctx := WithSharedWalks(context.Background())
	targets := []Target{member("k-a", "a", "u", "p"), member("k-b", "b", "u", "p"), member("k-c", "c", "u", "p")}
	done := make(chan []result)
	go func() { done <- searchAll(ctx, f, targets, "app") }()
	<-f.entered
	time.Sleep(100 * time.Millisecond) // let the followers join
	close(f.gate)
	res := <-done
	if got := f.calls.Load(); got != 1 {
		t.Fatalf("walks = %d, want 1 (AC1)", got)
	}
	for i, want := range []struct{ key, repo string }{{"k-a", "a/app"}, {"k-b", "b/app"}, {"k-c", "c/app"}} {
		r := res[i]
		if r.err != nil || len(r.imgs) != 1 {
			t.Fatalf("member %s: %+v", want.key, r)
		}
		if r.imgs[0].EntryKey != want.key || r.imgs[0].Repo != want.repo || r.imgs[0].Name != "app" {
			t.Errorf("member %s got %+v (AC2)", want.key, r.imgs[0])
		}
	}
}

func TestSharedWalkSequentialOneCall(t *testing.T) {
	f := &countingLister{names: []string{"a/app", "b/app", "c/app"}}
	ctx := WithSharedWalks(context.Background())
	for _, p := range []string{"a", "b", "c"} {
		if _, _, err := (CatalogSearcher{List: f.list}).Search(ctx, member("k-"+p, p, "u", "p"), Query{Text: "app"}); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.calls.Load(); got != 1 {
		t.Fatalf("walks = %d, want 1 (memo, not singleflight)", got)
	}
}

func TestSharedWalkFailureSharedAndClassifiedAlike(t *testing.T) {
	f := &countingLister{err: &oci.StatusError{Code: 503}}
	ctx := WithSharedWalks(context.Background())
	var outcomes []Outcome
	for _, p := range []string{"a", "b", "c"} {
		_, partial, err := (CatalogSearcher{List: f.list}).Search(ctx, member("k-"+p, p, "u", "p"), Query{Text: "app"})
		var se *oci.StatusError
		if !errors.As(err, &se) || se.Code != 503 {
			t.Fatalf("member %s err = %v, want 503 (AC3)", p, err)
		}
		outcomes = append(outcomes, Classify(err, 0, partial))
	}
	if f.calls.Load() != 1 {
		t.Fatalf("walks = %d, want 1", f.calls.Load())
	}
	if !reflect.DeepEqual(outcomes[0], outcomes[1]) || !reflect.DeepEqual(outcomes[1], outcomes[2]) {
		t.Errorf("outcomes differ: %+v", outcomes)
	}
}

func TestSharedWalkIdentityAndHostSplit(t *testing.T) {
	other := member("k-x", "", "u", "p")
	other.APIURL = "https://other.example"
	cases := []struct {
		name string
		a, b Target
		want int32
	}{
		{"anonymous vs credentials", member("a", "", "", ""), member("b", "", "u", "p"), 2},
		{"same user two passwords", member("a", "", "u", "p1"), member("b", "", "u", "p2"), 2},
		{"two api urls", member("a", "", "u", "p"), other, 2},
		{"trailing slash ignored", member("a", "", "u", "p"), func() Target { x := member("b", "", "u", "p"); x.APIURL = "https://reg.example"; return x }(), 1},
		{"credentials entry without secret = anonymous", member("a", "", "", ""), member("b", "", "", ""), 1},
		{"user/pass boundary not ambiguous", member("a", "", "ab", "c"), member("b", "", "a", "bc"), 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &countingLister{names: []string{"x/app"}}
			ctx := WithSharedWalks(context.Background())
			for _, tg := range []Target{tc.a, tc.b} {
				if _, _, err := (CatalogSearcher{List: f.list}).Search(ctx, tg, Query{Text: "app"}); err != nil {
					t.Fatal(err)
				}
			}
			if got := f.calls.Load(); got != tc.want {
				t.Errorf("walks = %d, want %d (AC4)", got, tc.want)
			}
		})
	}
}

func TestNoSharedWalksKeepsOneCallPerMember(t *testing.T) {
	f := &countingLister{names: []string{"a/app"}}
	for _, p := range []string{"a", "b", "c"} {
		_, _, _ = CatalogSearcher{List: f.list}.Search(context.Background(), member("k", p, "u", "p"), Query{Text: "app"})
	}
	if got := f.calls.Load(); got != 3 {
		t.Fatalf("walks = %d, want 3", got)
	}
}

func TestTwoBatchesTwoWalks(t *testing.T) {
	f := &countingLister{names: []string{"a/app"}}
	for range 2 {
		ctx := WithSharedWalks(context.Background())
		_, _, _ = CatalogSearcher{List: f.list}.Search(ctx, member("k", "a", "u", "p"), Query{Text: "app"})
	}
	if got := f.calls.Load(); got != 2 {
		t.Fatalf("walks = %d, want 2", got)
	}
}

func TestSharedWalkFollowerCancelledLeaderContinues(t *testing.T) {
	f := &countingLister{
		names:   []string{"a/app", "b/app"},
		entered: make(chan struct{}, 4),
		gate:    make(chan struct{}),
	}
	base := WithSharedWalks(context.Background())
	leader := make(chan result, 1)
	go func() {
		imgs, p, err := CatalogSearcher{List: f.list}.Search(base, member("k-a", "a", "u", "p"), Query{Text: "app"})
		leader <- result{imgs, p, err}
	}()
	<-f.entered
	fctx, cancel := context.WithCancel(base)
	follower := make(chan error, 1)
	go func() {
		_, _, err := CatalogSearcher{List: f.list}.Search(fctx, member("k-b", "b", "u", "p"), Query{Text: "app"})
		follower <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-follower:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("follower err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("follower did not return while the leader was blocked")
	}
	close(f.gate)
	r := <-leader
	if r.err != nil || len(r.imgs) != 1 || r.imgs[0].EntryKey != "k-a" {
		t.Fatalf("leader = %+v", r)
	}
	if f.calls.Load() != 1 {
		t.Errorf("walks = %d, want 1", f.calls.Load())
	}
}

func TestSharedWalkTruncatedIsPartialPerMember(t *testing.T) {
	f := &countingLister{names: []string{"a/app", "b/app"}, truncated: true}
	ctx := WithSharedWalks(context.Background())
	for _, p := range []string{"a", "b"} {
		imgs, partial, err := CatalogSearcher{List: f.list}.Search(ctx, member("k-"+p, p, "u", "p"), Query{Text: "app"})
		if err != nil || len(imgs) != 1 || !partial {
			t.Fatalf("member %s: imgs=%v partial=%v err=%v", p, imgs, partial, err)
		}
	}
	if f.calls.Load() != 1 {
		t.Errorf("walks = %d, want 1", f.calls.Load())
	}
}

func TestSharedWalkShortQueryZeroCalls(t *testing.T) {
	f := &countingLister{names: []string{"a/app"}}
	ctx := WithSharedWalks(context.Background())
	_, _, err := CatalogSearcher{List: f.list}.Search(ctx, member("k", "a", "u", "p"), Query{Text: "a"})
	if !errors.Is(err, ErrQueryTooShort) {
		t.Fatalf("err = %v", err)
	}
	if f.calls.Load() != 0 {
		t.Errorf("walks = %d, want 0", f.calls.Load())
	}
}

func TestWalkKeyNeverFormatsCredentials(t *testing.T) {
	const user, pass = "alice-user", "hunter2-secret"
	for _, name := range []string{"random secret", "zero secret"} {
		t.Run(name, func(t *testing.T) {
			s := &walkShare{}
			if name == "random secret" {
				if _, err := rand.Read(s.secret[:]); err != nil {
					t.Fatal(err)
				}
				if s.secret == ([32]byte{}) {
					t.Fatal("secret is zero")
				}
			}
			key := s.keyFor(member("k", "", user, pass))
			outs := map[string]string{"String()": key.String()}
			for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
				outs[verb+" key"] = fmt.Sprintf(verb, key)
				outs[verb+" &key"] = fmt.Sprintf(verb, &key)
				outs[verb+" keyFor"] = fmt.Sprintf(verb, s.keyFor(member("k", "", user, pass)))
			}
			for label, out := range outs {
				for _, leak := range []string{user, pass} {
					if strings.Contains(out, leak) {
						t.Errorf("%s leaks %q: %s", label, leak, out)
					}
				}
			}
		})
	}
}
