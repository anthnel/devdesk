# Remplacement des navigateurs internes de Security par les vues spécialisées

Ce plan détaille comment déléguer la sélection de dossiers et d'images de la vue `security` aux vues `workspaces` et `oci_images` existantes, améliorant ainsi l'expérience utilisateur et simplifiant le code.

## Proposed Changes

### [Component] shared
#### [MODIFY] [state.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/shared/state.go) [NEW]
- Ajouter un état de sélection (`SelectionActive`, `SelectionType`, `SelectionReturnView`) dans le `SharedState` pour suivre si l'application est en train de sélectionner un élément pour une autre vue.

### [Component] security
#### [MODIFY] [model.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/security/model.go)
- Supprimer les états `StateBrowsing` et `StateImageBrowsing`.
- Supprimer le code lié au `filepicker` et à `imageTable`.
- Modifier `openFileBrowser` et `openImageBrowser` pour envoyer des messages de requête de sélection au routeur principal.
- Ajouter un message `SelectionResultMsg` pour recevoir le chemin ou l'image sélectionnée.

### [Component] workspaces
#### [MODIFY] [model.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/workspaces/model.go)
- Ajouter une propriété `isSelectionMode` au modèle.
- Modifier `navigateIn` ou ajouter un nouveau raccourci (ex: `space`) pour confirmer la sélection quand `isSelectionMode` est vrai.
- Envoyer un message `SelectionResultMsg` lors de la confirmation.

### [Component] oci_images
#### [MODIFY] [model.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/oci_images/model.go)
- Similaire à workspaces : ajouter un mode sélection et renvoyer le résultat via un message.

### [Component] app (Router)
#### [MODIFY] [app.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/app/app.go)
- Gérer les nouvelles requêtes de sélection provenant de `security`.
- Transférer l'état de sélection aux vues `workspaces` ou `oci_images`.
- Intercepter le `SelectionResultMsg` pour ramener l'utilisateur à la vue `security` avec les données renseignées.

## Verification Plan

### Manual Verification
1.  Ouvrir la vue **Security**.
2.  Sélectionner le type **Directory**.
3.  Appuyer sur `b` (browse) : vérifier que l'application bascule sur la vue **Workspaces**.
4.  Naviguer dans un dossier et appuyer sur `enter` (ou la touche de sélection définie) : vérifier qu'on revient dans la vue **Security** avec le chemin correct.
5.  Répéter pour le type **Image** : vérifier le passage par la vue **Images** et le retour avec l'image sélectionnée.
6.  S'assurer que le mode normal de Workspaces et Images n'est pas affecté (quand on y accède directement via `:workspaces` ou `:images`).
