package metrics

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// write creates a file of n bytes, parent directories included.
func write(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// La somme porte sur toute la profondeur, et `.git` en fait partie : un dépôt
// cloné coûte son historique autant que son arbre de travail, et c'est souvent
// l'historique qui pèse. La question est « combien ce dossier prend ».
func TestSizeAddsUpTheWholeTreeIncludingGit(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "readme.md"), 100)
	write(t, filepath.Join(root, "repo", "main.go"), 250)
	write(t, filepath.Join(root, "repo", ".git", "objects", "pack", "p.pack"), 4000)

	got := Size(root)
	if !got.OK {
		t.Fatal("the walk reported a failure on a readable tree")
	}
	if got.Partial {
		t.Error("the walk reported itself partial with nothing to skip")
	}
	if got.Bytes != 4350 {
		t.Errorf("Size = %d bytes, want 4350 — every file at every depth", got.Bytes)
	}
}

func TestAnEmptyTreeIsMeasuredAtZero(t *testing.T) {
	got := Size(t.TempDir())
	if !got.OK {
		t.Fatal("an empty directory was reported unreadable")
	}
	if got.Bytes != 0 {
		t.Errorf("Size = %d bytes on an empty directory", got.Bytes)
	}
}

// Un chemin illisible ne rend pas une mesure « réussie » à zéro octet : ça se
// lirait comme un dossier vide, et c'est ce que la vérification de la racine
// dans Size existe pour éviter.
func TestAMissingPathIsNotAnEmptyTree(t *testing.T) {
	for _, path := range []string{filepath.Join(t.TempDir(), "nope"), ""} {
		if got := Size(path); got.OK {
			t.Errorf("Size(%q) reported a successful measurement of %d bytes", path, got.Bytes)
		}
	}
}

// Un lien symbolique ne compte pour rien, et c'est deux choses plutôt qu'une.
// Sa cible n'est pas suivie — elle serait comptée deux fois si elle est déjà
// dans l'arbre, et un cycle ne terminerait jamais. Mais son entrée à lui ne
// compte pas non plus : WalkDir en rend la longueur du chemin désigné, donc la
// taille de l'arbre bougerait au gré d'un renommage ailleurs. C'est la seconde
// moitié qui manquait, et elle ne pouvait pas se voir sous Windows, où ce test
// se saute.
func TestSizeDoesNotFollowSymlinks(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "data", "big.bin"), 5000)

	if err := os.Symlink(filepath.Join(root, "data"), filepath.Join(root, "link")); err != nil {
		if runtime.GOOS == "windows" {
			// Créer un lien demande un privilège que le compte de test n'a pas
			// forcément sous Windows.
			t.Skipf("symlinks unavailable: %v", err)
		}
		t.Fatalf("creating the symlink: %v", err)
	}

	if got := Size(root); got.Bytes != 5000 {
		t.Errorf("Size = %d bytes, want 5000 — the link weighed, by its target or by itself", got.Bytes)
	}
}
