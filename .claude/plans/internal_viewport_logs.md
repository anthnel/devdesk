# Plan: Intégration d'un Viewport interne pour les logs

## Description de l'objectif
Remplacer le système actuel qui utilise un pager externe (`less`) lors de la consultation des logs des conteneurs par un composant **Viewport interne** de Bubble Tea (`bubbles/viewport`). 
Afin de préserver les habitudes des "power users", une option sera ajoutée pour basculer vers un pager externe depuis la vue interne. Cela garantit une meilleure portabilité (Windows) et évite de casser l'immersion dans l'interface de l'application (TUI) tout en gardant des options avancées.

## Changements proposés

### `internal/ui/containers/` (Modèle & État)
- **Ajout d'états de vue** : Introduire une notion d'état dans le modèle (ex: `stateTable`, `stateLogs`) pour savoir ce qui doit être rendu et quels raccourcis doivent s'appliquer.
- **Ajout du Viewport** : Intégrer un `viewport.Model` dans la structure `Model` principale. Ce composant est responsable du rendu du texte scrollable.
- **Nouveau Msg de chargement** : Créer une commande asynchrone pour exécuter `docker logs` silencieusement, capturer la sortie (stdout/stderr) et envoyer un message personnalisé (ex: `ContainerLogsLoadedMsg`) avec le contenu texte.

### `internal/ui/containers/update.go`
- **Modification de `logsSelectedContainer()`** :
  - Au lieu de retourner un `tea.ExecProcess` avec `less`, cette fonction va basculer l'état sur `stateLogs`.
  - Elle déclenchera la commande asynchrone de récupération des logs.
- **Nouveau gestionnaire de saisie `handleLogsKeyMsg`** :
  - Gérer les touches de navigation basiques `up`, `down`, `pgup`, `pgdown` en les passant au composant viewport.
  - `<Esc>` : Quitter la vue des logs et revenir à la vue table (`stateTable`).
  - `e` ou `p` (Export/Pager) : Déclencher un fallback vers le pager externe.
- **Logique du fallback externe** :
  - Lorsque la touche `e` est pressée, appeler une fonction semblable à l'ancien comportement qui lance `tea.ExecProcess("sh", "-c", "docker logs ... | ${PAGER:-less} -R")`.
  - Au retour du `tea.ExecProcess` (via `PagerExitMsg`), s'assurer que l'application reste dans la vue interne des logs de manière transparente.

### `internal/ui/containers/view.go`
- Modifier la fonction `View()` pour qu'elle vérifie l'état courant. 
- Si `stateLogs`, dessiner un header (ex: `Logs: <nom_du_conteneur>`), le contenu du viewport `m.viewport.View()`, et un footer mentionnant les raccourcis (ex: `[Esc] Retour • [e] Pager externe • [↑/↓] Défiler`).

## Plan de vérification

### Vérification manuelle
1. Lancer l'application et naviguer dans la vue des conteneurs.
2. Sélectionner un conteneur en cours d'exécution et appuyer sur `l`.
3. **Validation interne** : S'assurer que les logs s'affichent correctement dans l'interface avec la prise en charge des couleurs ANSI, et que les touches de défilement (flèches, page up/down) fonctionnent.
4. **Validation du retour** : S'assurer que la touche `<Esc>` permet bien de revenir immédiatement à la liste des conteneurs, au même endroit.
5. **Validation Pager Externe** : Depuis la vue des logs internes, appuyer sur `e`. S'assurer que l'interface se suspend complètement au profit de `less` (ou le `$PAGER` du système).
6. Quitter le pager externe (`q` dans less) et s'assurer que le TUI de l'application reprend son état correctement sans artefact d'affichage.
