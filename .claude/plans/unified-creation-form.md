# Plan : Formulaire de création unifié Groupe/Projet

## Contexte

Actuellement deux raccourcis distincts : `g` → nouveau groupe, `ctrl+n` → nouveau projet.
Objectif : un seul `ctrl+n` ouvre un formulaire unifié avec un champ cycle **Type** (Group/Project),
template masqué pour Group, valeur par défaut = **Group**.

---

## Fichiers critiques

| Fichier | Rôle |
|---------|------|
| `internal/ui/components/creation_form.go` | Composant formulaire — changement majeur |
| `internal/ui/gitlab/explorer/model.go` | Suppression de `handleCreateGroup`, cleanup mode |
| `internal/ui/gitlab/explorer/view.go` | Shortcuts + aide |

---

## Phase 1 — `creation_form.go`

### 1.1 Nouveau champ `resourceType` + slice

```go
var resourceTypes = []string{"Group", "Project"}

type CreationForm struct {
    resourceType    int   // 0=Group, 1=Project (nouveau)
    formType        FormType  // maintenu en sync avec resourceType
    parentName      string
    parentID        int64
    nameInput       textinput.Model
    descInput       WrappedInput
    visibility      int
    visibilities    []string
    templates       []string
    templateIdx     int
    templateScroll  int
    templateWarning string
    focusedField    int   // 0=resourceType, 1=name, 2=desc, 3=visibility, 4=template(project), 5=submit
    width           int
    height          int
    err             string
}
```

### 1.2 Index des champs

| Index | Champ | Condition |
|-------|-------|-----------|
| 0 | resourceType (cycle ←→) | toujours |
| 1 | name | toujours |
| 2 | description | toujours |
| 3 | visibility (cycle ←→) | toujours |
| 4 | template (cycle list) | `resourceType==1 && len(templates)>0` |
| 4 ou 5 | submit | toujours (index dynamique) |

### 1.3 Méthode `maxField()`

```go
func (f *CreationForm) maxField() int {
    if f.resourceType == 1 && len(f.templates) > 0 {
        return 5
    }
    return 4
}
```

### 1.4 Nouveau constructeur

```go
// NewCreationForm crée le formulaire unifié.
// defaultResourceType: 0=Group, 1=Project
func NewCreationForm(
    defaultResourceType int,
    parentName string,
    parentID int64,
    defaultVisibility string,
    templates []string,
) *CreationForm
```

- `focusedField` démarre à **1** (name) — le type est visible mais le curseur est sur le nom
- `formType` initialisé via `formTypeFromResourceType(defaultResourceType)`

Helper :
```go
func formTypeFromResourceType(rt int) FormType {
    if rt == 1 { return FormTypeProject }
    return FormTypeGroup
}
```

### 1.5 Mise à jour `updateFocus()`

```go
func (f *CreationForm) updateFocus() {
    f.nameInput.Blur(); f.descInput.Blur()
    switch f.focusedField {
    case 1: f.nameInput.Focus()
    case 2: f.descInput.Focus()
    }
}
```

### 1.6 `handleKeyMsg()` — clés critiques

**`←/→` sur field 0 (resourceType) :**
```go
case "left", "right":
    if f.focusedField == 0 {
        if msg.String() == "left" {
            f.resourceType = (f.resourceType - 1 + len(resourceTypes)) % len(resourceTypes)
        } else {
            f.resourceType = (f.resourceType + 1) % len(resourceTypes)
        }
        f.formType = formTypeFromResourceType(f.resourceType)
        // clamp si le focus était sur template et on revient à Group
        if f.focusedField > f.maxField() {
            f.focusedField = f.maxField()
        }
        return f, nil
    }
    if f.focusedField == 3 { // visibility
        // logique existante cycling visibilities
    }
```

**`↑/↓` :** remplacer `maxField` local par `f.maxField()`

**`isOnTemplateField()` :** `f.focusedField == 4 && f.resourceType == 1 && len(f.templates) > 0`

### 1.7 Nouveau `renderResourceTypeField()` (Rule 132)

```go
func (f *CreationForm) renderResourceTypeField() string {
    label := "Type " + theme.IconSelect + " "
    value := lipgloss.NewStyle().Background(theme.ColorBackground).Foreground(theme.ColorText).
        Render(resourceTypes[f.resourceType])
    if f.focusedField == 0 {
        return theme.KeyStyle.Render(theme.IconCircleSmall+" "+label+theme.IconChevronRight+" ") + value
    }
    return theme.Bg("  "+label+theme.IconChevronRight+" ") + value
}
```

### 1.8 Mettre à jour les index dans les méthodes existantes

| Méthode | Changement |
|---------|-----------|
| `renderDescriptionField()` | `focusedField == 1` → `== 2` |
| `renderVisibilityField()` | `focusedField == 2` → `== 3` |
| `renderTemplateList()` | `focusedField == 3` → `== 4` |
| `View()` | Ajouter `renderResourceTypeField()` en premier + condition template `resourceType==1` |
| `submit()` | Pas de changement — `formType` déjà sync |
| `GetTitle()` | Utiliser `resourceTypes[f.resourceType]` |
| `Update()` routing | `case 1:` nameInput, `case 2:` descInput |

---

## Phase 2 — `model.go`

### Supprimer
- Méthode `handleCreateGroup()`
- Constante `ModeCreatingGroup = 3`
- Case `"g":` dans le key handler normal mode

### Renumber modes (après suppression de ModeCreatingGroup)
```go
const (
    ModeNormal           ViewMode = iota // 0
    ModePulling                          // 1
    ModeShowingReport                    // 2
    ModeLoadingTemplates                 // 3
    ModeCreatingProject                  // 4
    ModeConfirmingDelete                 // 5
)
```
Vérifier tous les usages de `ModeCreatingGroup` dans le fichier (switch/case, if blocks).

### Mettre à jour `handleTemplatesLoaded()`

```go
m.creationForm = components.NewCreationForm(
    0,   // defaultResourceType = Group (exigence)
    m.creationParentName,
    m.creationParentID,
    m.config.GitLab.DefaultVisibility,
    names,
)
```

### Renommer (optionnel mais recommandé)
`handleCreateProject` → `handleCreateResource` pour refléter le rôle unifié.

---

## Phase 3 — `view.go`

### `GetShortcuts()` mode normal

```go
return []shortcut.Shortcut{
    {Key: "←→",    Description: "Open/Back"},
    {Key: "ctrl+n", Description: "New"},       // "New project" → "New"
    // supprimer {Key: "g", Description: "New group"}
    {Key: "ctrl+d", Description: "Delete"},
    ...
}
```

### `GetShortcuts()` mode création

```go
case ModeCreatingProject:  // plus ModeCreatingGroup
    return []shortcut.Shortcut{
        {Key: "tab",  Description: "Next field"},
        {Key: "←→",   Description: "Select option"},
        {Key: "enter", Description: "Submit"},
        {Key: "esc",   Description: "Cancel"},
    }
```

### `GetHelpContent()` — si présent

Remplacer les deux entrées `g` et `n` par une seule entrée `ctrl+n` :
> Create a new group or project under the current context. Use ←→ to select the type.

---

## Séquence d'implémentation

1. `creation_form.go` — struct + `resourceTypes` + `maxField()`
2. `creation_form.go` — constructeur (`defaultResourceType int` au lieu de `FormType`)
3. `creation_form.go` — `updateFocus`, `isOnTextField`, `isOnTemplateField`
4. `creation_form.go` — `renderResourceTypeField()` + mise à jour `View()`
5. `creation_form.go` — `handleKeyMsg` (←→ field 0, clamp) + `Update()` routing
6. `creation_form.go` — `GetTitle()` + `submit()`
7. `model.go` — supprimer `handleCreateGroup`, case `"g"`, `ModeCreatingGroup`
8. `model.go` — renumber modes, update all references
9. `model.go` — `handleTemplatesLoaded` avec `defaultResourceType=0`
10. `view.go` — shortcuts + helpContent

---

## Vérification

```bash
go build ./...
go test ./internal/ui/... ./internal/command/...
```

- Tester `ctrl+n` → formulaire s'ouvre sur Group par défaut
- Tester `←/→` sur field 0 : bascule Group ↔ Project, template apparaît/disparaît
- Tester submit en mode Group → groupe créé (pas de template)
- Tester submit en mode Project → projet créé (avec ou sans template)
- Vérifier que `g` ne fait plus rien (ou n'est plus intercepté)
- Vérifier clamping : focus sur field 4 (template) + switch vers Group → focus passe à submit
