## Instructions pour l'édition de code

### Rule 201 : Le code doit être propre, concis, bien structuré pour être maintenable

#### Extraction de fonctions dans Update()
- **Limite stricte**: Si un case dans `Update(msg tea.Msg)` dépasse **5 lignes**, extraire dans une fonction séparée
- **Nommage**: `handle[MessageType]` (ex: `handleContextSwitch()`, `handleKeyPress()`)
- **Pattern**: La fonction doit retourner `(tea.Model, tea.Cmd)`

**Avant (mauvais):**
```go
case ContextSwitchCompleteMsg:
    a.config = msg.Config
    a.currentContext = msg.ContextName
    a.reinitializeViews(msg.Config)
    return a, func() tea.Msg {
        return tea.WindowSizeMsg{Width: a.width, Height: a.height}
    }
```

**Après (bon):**
```go
case ContextSwitchCompleteMsg:
    return a.handleContextSwitch(msg)

// Plus bas dans le fichier
func (a *App) handleContextSwitch(msg ContextSwitchCompleteMsg) (tea.Model, tea.Cmd) {
    a.config = msg.Config
    a.currentContext = msg.ContextName
    a.reinitializeViews(msg.Config)
    return a, func() tea.Msg {
        return tea.WindowSizeMsg{Width: a.width, Height: a.height}
    }
}
```

#### Réutilisation de code
- **Zero duplication**: Si un bloc de code apparaît 2+ fois, créer une fonction
- **Modules**: Regrouper les fonctions liées (ex: tout le context switching dans un même fichier)
- **Constantes**: Extraire les valeurs magiques dans des constantes nommées

#### Gestion d'erreurs
- **Pattern Bubble Tea**: Erreurs = messages (ex: `ContextSwitchErrorMsg`)
- **Toujours logger** en mode DEBUG: `log.Printf("ERROR: %v", err)`
- **Ne jamais ignorer**: Si erreur non-critique, documenter pourquoi

#### Commentaires
- **Obligatoires pour**:
  - Fonctions publiques (exported)
  - Logique métier complexe
  - Workarounds ou décisions non-évidentes
- **Format**: `// functionName does X and returns Y`
- **Éviter**: Commentaires qui répètent le code

#### Complexité
- **Maximum 3 niveaux d'indentation** dans une fonction
- **Fonctions > 50 lignes**: refactorer en fonctions plus petites
- **Switch > 10 cases**: envisager un pattern table-driven