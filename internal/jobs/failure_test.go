package jobs

import (
	"runtime"
	"testing"
	"time"
)

// failure builds a remembered failure with the key already normalized, the
// way the registry stores it.
func failure(kind Kind, target, contextName string, at time.Time) Failure {
	return Failure{
		FailureKey: FailureKey{Kind: kind, Target: target, Context: contextName},
		Detail:     "boom",
		At:         at,
	}
}

func TestTargetKeyCleansAbsolutePaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path spellings; the Windows case is TestTargetKeyCleansWindowsPaths")
	}
	tests := []struct {
		name, in, want string
	}{
		{"unchanged", "/w/app", "/w/app"},
		{"trailing slash", "/w/app/", "/w/app"},
		{"dot segment", "/w/./app", "/w/app"},
		{"parent segment", "/w/x/../app", "/w/app"},
		{"doubled separator", "/w//app", "/w/app"},
		{"root", "/", "/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TargetKey(tt.in); got != tt.want {
				t.Errorf("TargetKey(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestTargetKeyCleansWindowsPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("drive-letter paths are only absolute on Windows")
	}
	if got, want := TargetKey(`C:\w\app\`), `C:\w\app`; got != want {
		t.Errorf("TargetKey = %q, want %q", got, want)
	}
}

func TestTargetKeyLeavesReferencesAlone(t *testing.T) {
	tests := []struct {
		name, in string
	}{
		{"tagged image", "nginx:1.27"},
		{"bare image", "nginx"},
		{"qualified image", "docker.io/library/nginx"},
		{"image with digest", "nginx@sha256:0123456789abcdef"},
		{"forge path", "grp/sub/proj"},
		{"template slug", "go-service"},
		{"relative path", "w/app/"},
		{"empty", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TargetKey(tt.in); got != tt.in {
				t.Errorf("TargetKey(%q) = %q, want it unchanged", tt.in, got)
			}
		})
	}
}

func TestTwoSpellingsOfAnImageAreTwoTargets(t *testing.T) {
	tests := []struct {
		name, a, b string
	}{
		{"bare versus qualified", "nginx", "docker.io/library/nginx"},
		{"untagged versus latest", "nginx", "nginx:latest"},
		{"qualified versus tagged qualified", "docker.io/library/nginx", "docker.io/library/nginx:latest"},
		{"image versus repository path", "nginx", "/w/nginx"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if TargetKey(tt.a) == TargetKey(tt.b) {
				t.Errorf("TargetKey(%q) == TargetKey(%q) = %q, want two distinct keys", tt.a, tt.b, TargetKey(tt.a))
			}
		})
	}
}

// TestTargetKeyIsStable guards the contract the views rely on: two views that
// name the same target produce the same key, and keying twice changes nothing.
func TestTargetKeyIsStable(t *testing.T) {
	for _, in := range []string{"/w/app", "/w/app/", "nginx", "nginx:1.27", "grp/sub/proj"} {
		once := TargetKey(in)
		if twice := TargetKey(once); twice != once {
			t.Errorf("TargetKey is not idempotent on %q: %q then %q", in, once, twice)
		}
	}
	if TargetKey("/w/app/") != TargetKey("/w/app") {
		t.Error("two spellings of one repository path must share a key")
	}
}

func TestMaxFailuresIs500(t *testing.T) {
	if MaxFailures != 500 {
		t.Errorf("MaxFailures = %d, want 500", MaxFailures)
	}
}

func TestFailureForFindsByKind(t *testing.T) {
	failures := []Failure{
		failure(KindScan, "/w/app", "a", noon),
		failure(KindSync, "/w/app", "a", noon.Add(time.Minute)),
		failure(KindScan, "nginx", "a", noon.Add(2*time.Minute)),
	}
	tests := []struct {
		name        string
		kind        Kind
		target, ctx string
		wantFound   bool
		wantAt      time.Time
	}{
		{"scan on the repository", KindScan, "/w/app", "a", true, noon},
		{"sync on the repository", KindSync, "/w/app", "a", true, noon.Add(time.Minute)},
		{"scan on the image", KindScan, "nginx", "a", true, noon.Add(2 * time.Minute)},
		{"kind never failed there", KindPull, "/w/app", "a", false, time.Time{}},
		{"unknown target", KindScan, "/w/other", "a", false, time.Time{}},
		{"image is not the repository", KindScan, "/w/nginx", "a", false, time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := FailureFor(failures, tt.kind, tt.target, tt.ctx)
			if ok != tt.wantFound {
				t.Fatalf("found = %v, want %v", ok, tt.wantFound)
			}
			if ok && (!got.At.Equal(tt.wantAt) || got.Kind != tt.kind || got.Detail != "boom") {
				t.Errorf("got %+v, want kind %q at %v", got, tt.kind, tt.wantAt)
			}
		})
	}
}

func TestFailureForOnNothing(t *testing.T) {
	if _, ok := FailureFor(nil, KindScan, "/w/app", "a"); ok {
		t.Error("nil slice: found a failure")
	}
}

func TestAFailureIsReadInItsOwnContextOnly(t *testing.T) {
	failures := []Failure{failure(KindScan, "/w/app", "a", noon)}
	tests := []struct {
		ctx  string
		want bool
	}{
		{"a", true},
		{"b", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run("context "+tt.ctx, func(t *testing.T) {
			if _, ok := FailureFor(failures, KindScan, "/w/app", tt.ctx); ok != tt.want {
				t.Errorf("FailureFor(ctx=%q) found = %v, want %v", tt.ctx, ok, tt.want)
			}
			if got := FailuresOn(failures, tt.ctx, "/w/app"); (len(got) == 1) != tt.want {
				t.Errorf("FailuresOn(ctx=%q) = %d failures, want found = %v", tt.ctx, len(got), tt.want)
			}
		})
	}
}

func TestFailureForCleansTheTargetItLooksUp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path spellings")
	}
	failures := []Failure{failure(KindScan, "/w/app", "a", noon)}
	for _, lookup := range []string{"/w/app", "/w/app/", "/w/./app", "/w/x/../app"} {
		t.Run(lookup, func(t *testing.T) {
			if _, ok := FailureFor(failures, KindScan, lookup, "a"); !ok {
				t.Errorf("FailureFor(%q) did not find the failure stored under /w/app", lookup)
			}
			if got := FailuresOn(failures, "a", lookup); len(got) != 1 {
				t.Errorf("FailuresOn(%q) = %d failures, want 1", lookup, len(got))
			}
		})
	}
}

func TestFailuresOnKeepsOnlyTheKindsAsked(t *testing.T) {
	failures := []Failure{
		failure(KindScan, "/w/app", "a", noon),
		failure(KindSync, "/w/app", "a", noon.Add(time.Minute)),
		failure(KindDelete, "/w/app", "a", noon.Add(2*time.Minute)),
		failure(KindScan, "/w/other", "a", noon.Add(3*time.Minute)),
		failure(KindScan, "/w/app", "b", noon.Add(4*time.Minute)),
	}
	tests := []struct {
		name  string
		kinds []Kind
		want  []Kind
	}{
		{"no kinds means all, oldest first", nil, []Kind{KindScan, KindSync, KindDelete}},
		{"scan only", []Kind{KindScan}, []Kind{KindScan}},
		{"sync only", []Kind{KindSync}, []Kind{KindSync}},
		{"two kinds keep the stored order", []Kind{KindDelete, KindScan}, []Kind{KindScan, KindDelete}},
		{"a kind with no failure", []Kind{KindPull}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FailuresOn(failures, "a", "/w/app", tt.kinds...)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d failures %+v, want kinds %v", len(got), got, tt.want)
			}
			for i, f := range got {
				if f.Kind != tt.want[i] {
					t.Errorf("failure %d kind = %q, want %q", i, f.Kind, tt.want[i])
				}
				if f.Target != "/w/app" || f.Context != "a" {
					t.Errorf("failure %d leaked from %q in %q", i, f.Target, f.Context)
				}
			}
		})
	}
}

func TestFailuresOnDoesNotAliasItsInput(t *testing.T) {
	failures := []Failure{failure(KindScan, "/w/app", "a", noon)}
	got := FailuresOn(failures, "a", "/w/app")
	if len(got) != 1 {
		t.Fatalf("got %d failures, want 1", len(got))
	}
	got[0].Detail = "changed"
	if failures[0].Detail != "boom" {
		t.Error("mutating the result changed the caller's slice")
	}
}

func TestFailuresOnNothing(t *testing.T) {
	if got := FailuresOn(nil, "a", "/w/app", KindScan); len(got) != 0 {
		t.Errorf("nil slice: got %d failures", len(got))
	}
}
