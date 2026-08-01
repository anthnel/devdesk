# TUI — Comportement & Architecture Bubble Tea

### Rule 109 : Convention de nommage des messages Bubble Tea

- Format : `[ComponentName][Action]Msg`
- Exemples : `ComponentFormSubmitMsg`, `ConfirmModalYesMsg`
- Toujours documenter avec un commentaire
- Grouper les messages liés ensemble dans le fichier

### Rule 110 : Ne JAMAIS modifier le modèle dans un Cmd ⚠️

**Principe fondamental de Bubble Tea (architecture Elm).**

`Update()`, `View()` et les `Cmd` s'exécutent en parallèle. Modifier le modèle dans un Cmd crée des race conditions détectables avec `go run -race`.

| Composant | Rôle | Peut modifier le modèle ? |
|-----------|------|---------------------------|
| `Update()` | Traite les messages | ✅ OUI (seul endroit autorisé) |
| `View()` | Affiche l'UI | ❌ NON (lecture seule) |
| `Cmd` | Opérations I/O async | ❌ NON (retourne des messages) |

```go
// ❌ INTERDIT — race condition
func (m *Model) logout() tea.Cmd {
    return func() tea.Msg {
        m.authenticated = false  // DANGER
        return nil
    }
}

// ✅ CORRECT — message + Update()
type LogoutCompleteMsg struct{}

func (m *Model) logout() tea.Cmd {
    url := m.urlInput.Value()  // copier les données nécessaires
    return func() tea.Msg {
        auth := gitlabpkg.NewAuth(m.storage)
        _ = auth.Logout(url)
        return LogoutCompleteMsg{}
    }
}

case LogoutCompleteMsg:
    m.authenticated = false  // thread-safe dans Update()
    m.user = nil
    return m, nil
```

**Règle d'or** : Les Cmds font le "travail sale" (I/O), puis envoient un message à `Update()`.

### Rule 126 : Comportement du cache de scan (Images & Workspaces)

| Action | Cache disque | Cache mémoire | Scan lancé |
|--------|-------------|---------------|------------|
| `Enter` (item scanné) | Lu | Lu | Non |
| `Enter` (non scanné) | — | — | Non |
| `ctrl+s` (en cours) | Bloqué | Bloqué | Non |
| `ctrl+s` (disponible) | Écrasé | Mis à jour | Oui (item seul) |
| `A` (non scannés) | Non modifié | Non modifié | Oui (non scannés) |
| `ctrl+a` (tous) | **Purgé** | **Purgé** | Oui (tous) |

**`ctrl+a` doit vider le cache avant de relancer les scans :**
```go
// ✅ CORRECT
func (m Model) requestScanAll() (tea.Model, tea.Cmd) {
    var keys []string
    for _, img := range m.images {
        delete(m.scanCache, name)  // purge in-memory dans Update()
        keys = append(keys, name)
    }
    return m, tea.Batch(deleteScanCacheCmd(keys), batchScanCmd(keys, m.defaultScanOpts()))
}
```

### Rule 128 : Messages footer — log + timer 3s, jamais dans le viewport

**Tout message dans le footer (erreur ou info) disparaît automatiquement après 3 secondes.**

#### Types de messages

| Type | Champ | Couleur | Usage |
|------|-------|---------|-------|
| Erreur | `footerError` / `errorMsg` | `ColorError` + `StatusErrorStyle` | Échec d'opération, action bloquée |
| Info | `footerInfo` / `infoMsg` | `ColorHighlight` | Feedback non-critique (scan déjà en cours, etc.) |

#### Pattern obligatoire

```go
// 1. Déclarer le message de nettoyage dans le package
type clearFooterMsgMsg struct{}

// 2. Déclarer la commande timer
func clearFooterMsgCmd() tea.Cmd {
    return tea.Tick(3*time.Second, func(time.Time) tea.Msg {
        return clearFooterMsgMsg{}
    })
}

// 3. Dans Update() — handler du message de nettoyage
case clearFooterMsgMsg:
    m.footerError = ""
    m.footerInfo = ""

// 4. À chaque set de message footer — toujours retourner le timer
case SomeErrorMsg:
    m.footerError = "Failed to load data — check logs"
    return m, clearFooterMsgCmd()

case ScanAlreadyRunningMsg:
    m.footerInfo = "Scan already in progress"
    return m, clearFooterMsgCmd()
```

#### Rendu dans RenderFooter()

```go
infoLine := theme.EmptyLineBg(width)
if m.footerError != "" {
    infoLine = theme.PadWithBg(theme.StatusErrorStyle.Render(m.footerError), width)
} else if m.footerInfo != "" {
    infoLine = lipgloss.NewStyle().
        Foreground(theme.ColorHighlight).
        Background(theme.ColorBackground).
        Width(width).
        Align(lipgloss.Center).
        Render(m.footerInfo)
}
```

Format de log obligatoire : `log.Printf("ERROR [package/view] action: %v", err)`

Interdit :
- ❌ `return m, nil` après avoir set un message footer (pas de timer → message permanent)
- ❌ `err.Error()` directement dans l'UI
- ❌ Remplacer la vue par un écran d'erreur (sauf erreurs fatales d'initialisation)
- ❌ Ignorer une erreur sans `log.Printf`
- ❌ Messages footer sans limite de durée
