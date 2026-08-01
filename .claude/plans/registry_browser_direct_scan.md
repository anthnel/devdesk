# Plan : Scan direct Trivy depuis la vue registry browser (tag)

## Résumé

Ajouter dans la vue tag du registry browser :
- `ctrl+s` → scan **direct Trivy** de l'image distante (sans pull), résultats mis en cache (même pipeline que la vue OCI images).
- `enter` sur un tag avec résultats en cache → naviguer vers la vue security en `StateResults` avec les CVE (même flow `ScanDetailsRequestMsg`).
- Le tableau de tags affiche un spinner dans les colonnes C/H/M/L pendant le scan, puis les compteurs.

## Patterns à répliquer

| Catégorie | Source | Pattern |
|---|---|---|
| Déclenchement scan | `update.go:785` | `batchScanCmd([]imageScanJob{{Name, Target}}, m.defaultScanOpts())` |
| Spinner en cours | `update.go:1252-1261` | `handleImageScanStarting` → `m.scanningImages[name] = true` → `m.updateImageTable()` |
| Mise à jour cache | `update.go:1264-1277` | `handleImageScanFinished` → `m.scanCache[name] = entry` |
| Navigation CVE | `update.go:383` | `ScanDetailsRequestMsg{ImageName: name}` → `app.go:668` charge résultat → security view |
| Cache disque + full result | `commands.go:122-131` | `scanOneImageCmd` sauvegarde via `cache.SaveImageScanResult()` automatiquement |
| Spinner dans cellule table | `update.go:1376` | `m.scanningImages[rawName]` → frame spinner dans cellule de ligne |
| Sync browser ↔ modèle | `update.go:1689` | `m.registryBrowser.SetScanCache(m.scanCache)` existe déjà |

## Fichiers à modifier

| Fichier | Action | Raison |
|---|---|---|
| `internal/ui/oci_resources/registry_browser.go` | UPDATE | Nouveau champ `scanningTags`, méthodes helper, `ctrl+s` → scan-only, `enter` → CVE, spinner dans table |
| `internal/ui/oci_resources/model.go` | UPDATE | Nouveau message `RegistryTagDirectScanMsg` |
| `internal/ui/oci_resources/update.go` | UPDATE | Handler du nouveau message + sync browser dans `handleImageScanStarting` / `handleImageScanFinished` |
| `internal/ui/oci_resources/view.go` | UPDATE | Shortcuts `browserStateTags` : `enter` conditionnel + desc `ctrl+s` → "Scan image" |

## Tâches

### Tâche 1 — Nouveau message (`model.go`)

Ajouter après `RegistryBrowserScanRequestMsg` :

```go
// RegistryTagDirectScanMsg requests a direct remote scan of a registry image tag (no pull).
type RegistryTagDirectScanMsg struct {
    ImageName string
}
```

### Tâche 2 — État scan dans le browser (`registry_browser.go`)

**Struct** : ajouter `scanningTags map[string]bool` (initialisé dans `newRegistryBrowser`).

**Méthodes à ajouter :**

```go
// SetTagScanning marks or clears the in-progress scan state for an image.
func (b *RegistryBrowser) SetTagScanning(imageName string, scanning bool) {
    if scanning {
        b.scanningTags[imageName] = true
    } else {
        delete(b.scanningTags, imageName)
    }
    b.rebuildTagTable()
}

// HasSelectedTagScanResults returns true when the selected tag has cached scan results.
func (b *RegistryBrowser) HasSelectedTagScanResults() bool {
    tag := b.getSelectedTag()
    if tag == "" {
        return false
    }
    _, ok := b.scanCache[b.fullImageName(tag)]
    return ok
}
```

**`rebuildTagTable()`** : quand `b.scanningTags[imageName]` est true, afficher un frame spinner dans les colonnes C/H/M/L au lieu de `-` (même pattern que `update.go:1376`).

### Tâche 3 — Changer `ctrl+s` → scan-only (`registry_browser.go`)

Remplacer l'appel à `scanSelectedTag()` (qui faisait pull+scan + `browserStateStatus`) par :

```go
func (b *RegistryBrowser) requestDirectScan() (*RegistryBrowser, tea.Cmd) {
    tag := b.getSelectedTag()
    if tag == "" {
        return b, nil
    }
    imageName := b.fullImageName(tag)
    return b, func() tea.Msg { return RegistryTagDirectScanMsg{ImageName: imageName} }
}
```

Dans `handleTagsKeyMsg`, `case "ctrl+s"` appelle `b.requestDirectScan()`.

### Tâche 4 — `enter` → CVE details (`registry_browser.go`)

Dans `handleTagsKeyMsg`, ajouter :

```go
case "enter":
    return b.openTagScanDetails()
```

```go
func (b *RegistryBrowser) openTagScanDetails() (*RegistryBrowser, tea.Cmd) {
    tag := b.getSelectedTag()
    if tag == "" {
        return b, nil
    }
    imageName := b.fullImageName(tag)
    if _, ok := b.scanCache[imageName]; !ok {
        return b, nil
    }
    return b, func() tea.Msg { return ScanDetailsRequestMsg{ImageName: imageName} }
}
```

### Tâche 5 — Handler modèle + sync browser (`update.go`)

**Nouveau case dans `Update()`** :

```go
case RegistryTagDirectScanMsg:
    return m.handleRegistryTagDirectScan(msg)
```

**Nouveau handler** :

```go
func (m Model) handleRegistryTagDirectScan(msg RegistryTagDirectScanMsg) (tea.Model, tea.Cmd) {
    if m.scanningImages[msg.ImageName] {
        m.infoMsg = "Scan already in progress"
        return m, clearInfoMsgCmd()
    }
    job := imageScanJob{Name: msg.ImageName, Target: msg.ImageName}
    return m, batchScanCmd([]imageScanJob{job}, m.defaultScanOpts())
}
```

**`handleImageScanStarting`** — ajouter après `m.updateImageTable()` :

```go
if m.registryBrowser != nil {
    m.registryBrowser.SetTagScanning(msg.ImageName, true)
}
```

**`handleImageScanFinished`** — ajouter après `m.updateImageTable()` :

```go
if m.registryBrowser != nil {
    m.registryBrowser.SetTagScanning(msg.ImageName, false)
    m.registryBrowser.SetScanCache(m.scanCache)
}
```

### Tâche 6 — Shortcuts (`view.go`)

Dans `GetShortcuts()`, remplacer la branche `browserStateTags` (hors filtre actif) :

```go
var shortcuts []shortcut.Shortcut
if m.registryBrowser.HasSelectedTagScanResults() {
    shortcuts = append(shortcuts, shortcut.Shortcut{Key: "enter", Description: "View CVE details"})
}
shortcuts = append(shortcuts,
    shortcut.Shortcut{Key: "ctrl+s", Description: "Scan image"},
    shortcut.Shortcut{Key: "p", Description: "Pull image"},
    shortcut.Shortcut{Key: "/", Description: "Filter"},
    shortcut.Shortcut{Key: ".", Description: "Sort"},
    shortcut.Shortcut{Key: "esc", Description: "Go back"},
)
return shortcuts
```

## Risques

| Risque | Probabilité | Mitigation |
|---|---|---|
| Trivy ne peut pas scanner une image privée sans auth | Moyen | `tokenInput` existe déjà dans le browser — passer le token comme `TRIVY_PASSWORD` env var dans `scanOneImageCmd` si besoin (hors scope initial) |
| `ScanDetailsRequestMsg` catchable par `app.go` depuis le browser | Faible | Vérifier que `app.go` intercepte bien ce message peu importe la vue active |
| Double scan (tag déjà en cours) | Faible | Guard `m.scanningImages[imageName]` déjà en place |
| `handleImageScanFinished` appelé pour un scan OCI images → sync browser inutile | Faible | Guard `m.registryBrowser != nil` évite tout effet de bord |

## Validation

```bash
go build ./...
go test ./internal/... -count=1
```
