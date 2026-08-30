package cache

import "os"

// A scan cache outlives what it describes. `D` on a repository, a `rm -rf` made
// outside the application, a workspace moved to another root — each leaves an
// entry naming a path that is no longer there, and nothing ever removes it.
//
// So every reader reconciles at load time. That is one rule against several
// callers rather than a deletion cascading from each of them, and one of the
// callers — the outside world — cannot call anything.
//
// The predicate lives here, with the entries it judges, because the readers are
// in three different packages: the `:sec` inventory, the dashboard's posture,
// and the tests of both. It was the inventory's alone until the dashboard was
// found counting the CRITICALs of repositories that the inventory had already
// dropped — the same cache read twice with two different answers.

// RepositoryGone says a repository path has been removed, and only that.
//
// os.IsNotExist and nothing else: a permission error, or a share that answers
// slowly, means the path could not be *read*, which is not the same claim. The
// entry survives anything but a definite absence — showing a target that no
// longer exists is a stale row, hiding one that does is a lie.
func RepositoryGone(path string) bool {
	_, err := os.Stat(path)
	return err != nil && os.IsNotExist(err)
}
