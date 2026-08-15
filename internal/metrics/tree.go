package metrics

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"
)

// Size walks a directory and adds up what its files occupy.
//
// **C'est le seul appel du package dont le coût grandit avec les données de
// l'utilisateur** : il lit chaque entrée de chaque dossier. Il tourne donc sur
// l'horloge lente, sur la goroutine d'un Cmd, et jamais deux à la fois — voir
// Model.measuringSize, qui est là pour ça et pour rien d'autre.
//
// Les liens symboliques ne sont pas suivis : WalkDir lit le lien et non sa
// cible, ce qui évite d'un même geste les cycles et le double comptage d'un
// dossier déjà dans l'arbre.
//
// Rien n'est exclu du parcours, `.git` compris : un dépôt cloné coûte son
// historique autant que son arbre de travail, et c'est souvent l'historique qui
// pèse. La question posée est « combien ce dossier prend », pas « combien de
// code il contient ».
func Size(path string) TreeSize {
	out := TreeSize{Path: path}
	if path == "" {
		return out
	}

	// La racine est vérifiée à part : sans ça, un chemin illisible rend une
	// mesure « réussie » à zéro octet, ce qui se lit comme un dossier vide.
	if _, err := os.Stat(path); err != nil {
		log.Printf("ERROR [metrics/tree] stat %q: %v", path, err)
		return out
	}

	err := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			// Un dossier refusé ou disparu en cours de route n'arrête pas la
			// mesure : il la rend partielle.
			out.Partial = true
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			out.Partial = true
			return nil
		}
		if size := info.Size(); size > 0 {
			out.Bytes += uint64(size)
		}
		return nil
	})
	if err != nil {
		log.Printf("ERROR [metrics/tree] walking %q: %v", path, err)
		return TreeSize{Path: path}
	}

	out.OK = true
	return out
}
