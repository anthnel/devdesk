package workspaces

import (
	"os"
	"testing"
)

// The worst case the backlog measured when the depth limit came off: a
// workspaces_dir pointed at something that is not a workspace, so nothing
// prunes the walk. 283 ms then, and the bare-repository check adds one stat per
// directory — this is what says whether that is affordable.
func BenchmarkWalkOverALargeTree(b *testing.B) {
	root := os.Getenv("DEVDESK_BENCH_TREE")
	if root == "" {
		b.Skip("set DEVDESK_BENCH_TREE to a large directory")
	}
	for b.Loop() {
		detectSubRepos(root, false)
	}
}
