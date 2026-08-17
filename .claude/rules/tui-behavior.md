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

### Rule 128 : Messages footer — trois niveaux, centrés, un seul composant

**`components.FooterMessage` est la seule implémentation.** Une vue ne rend pas
son propre message : elle en déclare un, le pose depuis `Update()`, et le rend
par `m.footer.View(width, status)`.

Il y en avait huit, une par vue, chacune avec ses champs, sa minuterie et son
bloc lipgloss. C'est ce qui a produit le défaut que ce composant supprime :
personne n'a jamais centré la branche d'erreur, dans aucune des huit, donc les
erreurs étaient alignées à gauche partout et les notices centrées.

#### Les trois niveaux

Ils sont définis par **ce qui s'est passé**, pas par le ressenti :

| Niveau | Sens | Couleur |
|--------|------|---------|
| `Error` | une opération a échoué, ou le système l'a refusée | `ColorFooterError` = le rouge des CVE **CRITICAL** |
| `Warn` | l'action ne peut pas être honorée telle que demandée, mais rien n'a échoué : précondition non remplie, déjà en cours, sans objet ici | `ColorFooterWarn` = l'orange des CVE **MEDIUM** |
| `Info` | un fait neutre, ou une opération réussie | `ColorFooterInfo` = `ColorText`, la couleur de texte ordinaire |

Info n'est pas en gras, les deux autres le sont : la hiérarchie passe par la
graisse autant que par la teinte, et une info neutre en gras redeviendrait une
alerte.

Les couleurs sont des **alias sémantiques** assignés dans `ApplyTheme`
(`colors.go`), comme les couleurs de syntaxe du viewer : aucun fichier de thème
ne gagne de clé. Elles visent les noms **severity** et non `ColorError` /
`ColorWarn` — le thème par défaut les fait coïncider, donc le choix est
invisible aujourd'hui ; il cesse de l'être dans un thème qui les sépare.

#### Ce qui est interdit

- ❌ `theme.StatusErrorStyle`, `StatusOKStyle`, `StatusWarningStyle` ou
  `ColorHighlight` dans un `RenderFooter`, un `renderInfoLine` ou un
  `renderInfoText` — `TestNoViewStylesItsOwnFooterMessage` parcourt les sources
  et échoue en nommant fichier, ligne et fonction.
- ❌ Le **vert** dans un footer. Il est réservé aux icônes de statut (Rule 121).
- ❌ Un message **aligné à gauche**. `View` centre, toujours, sur toute la
  largeur.
- ❌ Une minuterie locale (`clearFooterCmd`, `clearInfoMsgCmd`, …) : le
  composant la porte.

#### Pattern obligatoire

```go
// 1. Le modèle déclare un champ.
type Model struct {
    footer sharedcomponents.FooterMessage
}

// 2. Update() pose le message et retourne sa minuterie. Un message posé sans
//    sa minuterie ne disparaît jamais.
case SomeErrorMsg:
    log.Printf("ERROR [package/view] action: %v", msg.Err)
    return m, m.footer.Error("Failed to load data — check logs")

case ScanAlreadyRunningMsg:
    return m, m.footer.Warn("Scan already in progress")

// 3. Update() offre les messages non traités au composant, qui consomme
//    l'expiration qui lui est adressée.
m.footer.Handle(msg)
return m, nil

// 4. RenderFooter() rend la ligne, vide comprise — Rule 124 la budgète.
func (m Model) RenderFooter(width int) string {
    return theme.EmptyLineBg(width) + "
" + m.footer.View(width, m.status())
}
```

**L'expiration est identifiée** (`ClearFooterMsg{ID}`) : une minuterie périmée
n'efface pas le message qui a pris la place du sien. C'est ce qui rend le type
partageable entre paquets, et ça corrige au passage un défaut que les huit
implémentations avaient toutes — un message posé à t+2,9 s était effacé à t+3 s
par la minuterie du précédent.

#### `Status` — ce qui n'a pas de minuterie

Une progression, un hint, un chargement : ce sont des **états**, pas des
événements. Ils sont dérivés à chaque frame et passés en second argument de
`View`, jamais posés comme message — une ligne posée quand le premier dépôt
démarre s'effacerait pendant que le dixième tourne encore.

```go
func (m Model) status() sharedcomponents.Status {
    if m.loading && len(m.table.Items()) == 0 {
        return sharedcomponents.Status{Text: "Loading images...", Spinner: true}
    }
    return sharedcomponents.Status{Text: m.actionLine()}
}
```

Précédence dans `View` : **erreur → warning → info → status**. Un échec que
l'utilisateur n'a pas lu prime sur la progression de ce qui tourne encore.

`Spinner: true` préfixe la frame courante, que la vue pousse depuis son handler
`spinner.TickMsg` par `m.footer.SetSpinnerFrame(m.spinner.View())`. C'est le
spinner **rendu** et non une frame brute : chaque vue donne déjà à son spinner
le style `theme.SpinnerStyle()`, et le restyler imbriquerait une séquence dans
une autre. La mesure passe par `lipgloss.Width`, qui ignore les échappements —
c'est l'inverse de la règle d'une cellule de table (Rule 122), et la différence
tient à qui mesure.

#### Le chargement d'une table appartient au footer

**Quand un `datatable` charge, la table reste à l'écran** et le chargement est
un message d'info avec spinner, dans le footer uniquement. Un corps qui se
remplace par un spinner perd son en-tête et ses colonnes le temps de chaque
`ctrl+r`, puis les retrouve : un saut de mise en page à chaque rafraîchissement.

Conséquence obligatoire : le message vide (« No images found ») est **conditionné
à la fin du chargement**, sinon la table annonce l'absence de ce qu'elle est en
train de chercher.

`TestNoTableViewRendersALoadingBody` refuse un `theme.SpinnerMessage` dans les
vues concernées. Les exceptions — un écran d'opération sans table derrière —
sont **déclarées** dans le test, à la manière de `keymap.DeclaredExceptions()`.

#### Tests — la minuterie dort pour de vrai

`tea.Tick` bloque sa durée entière, et `testutil.Msgs` exécute tout le lot qu'on
lui donne : un test qui inspecte un `Cmd` portant la minuterie paie les trois
secondes en entier.

- Pour vérifier qu'un message **disparaît**, construire l'expiration plutôt que
  d'exécuter le `Cmd` :
  `feed(t, m, components.ClearFooterMsg{ID: m.footer.ID()})`.
- Pour un test qui doit drainer le `Cmd` (parce qu'il en cherche un autre
  dedans), raccourcir la minuterie :
  `testutil.FastTimers(t, &components.FooterMsgDuration)`.

`FooterMsgDuration` est un `var` exporté pour cette seule raison ; sa valeur de
production est fixée par `TestAMessageGetsThreeSeconds`.

#### Autres interdits

Format de log obligatoire : `log.Printf("ERROR [package/view] action: %v", err)`

- ❌ `err.Error()` directement dans l'UI (sauf message de validation déjà écrit
  pour être lu)
- ❌ Remplacer la vue par un écran d'erreur (sauf erreurs fatales d'initialisation)
- ❌ Ignorer une erreur sans `log.Printf`
