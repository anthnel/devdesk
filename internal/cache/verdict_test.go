package cache

import (
	"os"
	"path/filepath"
	"testing"
)

// Le verdict « secrets » est passé du booléen au pointeur, et ce fichier tient
// les deux bouts de ce que ça change sur des fichiers déjà écrits : ce qui doit
// survivre survit, et ce qui n'a jamais été écrit se lit « inconnu » plutôt que
// « propre ».

// writeRaw writes a cache file verbatim, pour décrire un fichier tel qu'une
// version précédente l'a laissé — ce qu'un round-trip par la structure actuelle
// ne saurait pas produire.
func writeRaw(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

// L'ancien champ s'écrivait toujours — `json:"sensitive"`, sans omitempty —
// donc les deux verdicts qu'une version précédente a pu enregistrer se relisent
// tels quels. C'est ce qui rend la migration gratuite : il n'y en a pas.
func TestALegacyWorkspaceVerdictSurvivesTheChangeOfType(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"a repository that carried a secret", `true`, true},
		{"one that was looked at and was clean", `false`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "workspace-scans.json")
			writeRaw(t, path, `{"version":1,"contexts":{"work":{"/home/u/repo":{
				"repo_path":"/home/u/repo","critical":1,"sensitive":`+tt.raw+`,
				"scanned_at":"2026-08-01T12:00:00Z"}}}}`)

			got := openWorkspaceCache(t, path, "work").Get("/home/u/repo")

			if got == nil {
				t.Fatal("the entry was dropped")
			}
			if got.Sensitive == nil {
				t.Fatalf("Sensitive = nil, want the verdict %v the file records", tt.want)
			}
			if *got.Sensitive != tt.want {
				t.Errorf("Sensitive = %v, want %v", *got.Sensitive, tt.want)
			}
		})
	}
}

// Une entrée d'image écrite avant que le scan d'image ait une étape secrets n'a
// pas de clé du tout. Elle doit se lire « personne n'a cherché » : un false
// serait une icône verte apposée à un scan qui n'a rien regardé, ce qui est le
// seul mensonge que ce champ pouvait produire.
func TestAnImageEntryWrittenBeforeTheSecretStageHasNoVerdict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image-scans.json")
	writeRaw(t, path, `{"version":1,"contexts":{"work":{"api:v1":{
		"image_id":"sha256:abc","critical":2,"scanned_at":"2026-08-01T12:00:00Z"}}}}`)

	got := openImageCache(t, path).Get("api:v1")

	if got == nil {
		t.Fatal("the entry was dropped")
	}
	if got.Critical != 2 {
		t.Errorf("Critical = %d, want the counts to come through untouched", got.Critical)
	}
	if got.Sensitive != nil {
		t.Errorf("Sensitive = %v, want nil — that scan had no secret stage at all", *got.Sensitive)
	}
}

// Le verdict fait l'aller-retour par le fichier, dans ses trois valeurs.
func TestTheThreeVerdictsSurviveARoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		write *bool
	}{
		{"found", secretsFound()},
		{"clean", secretsClean()},
		{"nobody looked", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "image-scans.json")
			c := openImageCache(t, path)
			if err := c.Set("api:v1", ImageScanEntry{Sensitive: tt.write}); err != nil {
				t.Fatalf("Set: %v", err)
			}

			// Relu depuis le fichier, pas depuis la mémoire de l'instance qui
			// vient de l'écrire : c'est la sérialisation qui est en cause.
			got := openImageCache(t, path).Get("api:v1")

			if got == nil {
				t.Fatal("the entry did not come back")
			}
			switch {
			case tt.write == nil && got.Sensitive != nil:
				t.Errorf("Sensitive = %v, want nil", *got.Sensitive)
			case tt.write != nil && got.Sensitive == nil:
				t.Errorf("Sensitive = nil, want %v", *tt.write)
			case tt.write != nil && *got.Sensitive != *tt.write:
				t.Errorf("Sensitive = %v, want %v", *got.Sensitive, *tt.write)
			}
		})
	}
}
