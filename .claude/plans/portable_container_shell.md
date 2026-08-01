# Plan: Amélioration de la Portabilité du Shell de Conteneur

## Description de l'objectif
Améliorer la fonctionnalité "Ouvrir un shell" (`s`) dans la vue des conteneurs.
Actuellement, la commande lancée est fixée à `/bin/sh`. L'objectif est de tenter intelligemment d'ouvrir `/bin/bash` par défaut, car il offre une meilleure expérience utilisateur (complétion, historique, etc.), et de basculer (fallback) sur `/bin/sh` uniquement si `bash` n'est pas disponible dans le conteneur cible.

## Changements proposés

### `internal/ui/containers/update.go`
- **Modification de la fonction `shellSelectedContainer()`** :
  - **Étape 1 : Vérification de la disponibilité de `bash`**. Avant d'appeler `tea.ExecProcess`, utiliser `exec.Command` pour exécuter silencieusement et de manière asynchrone (ou synchrone rapide) la commande `docker exec <id> command -v bash` ou `docker exec <id> which bash`.
  - **Étape 2 : Détermination de l'exécutable**. Si la commande retourne un succès (code de sortie `0`), configurer la commande du shell interactif avec `/bin/bash`. Sinon, utiliser `/bin/sh`.
  - **Étape 3 : Lancement du processus PTY**. Passer la commande déterminée à `tea.ExecProcess` avec les arguments `-it` habituels.

*Note d'implémentation* : Exécuter la vérification `which bash` prend environ 20-50ms car le conteneur est déjà en cours d'exécution. C'est synchrone, très rapide et invisible pour l'utilisateur, évitant de polluer l'intérieur du PTY avec un script conditionnel du style `sh -c "bash || sh"` qui pourrait causer des erreurs de signaux ou d'historique bash.

## Plan de vérification

### Vérification manuelle
1. Démarrer un conteneur basé sur une image classique connue pour contenir `bash` (ex: `ubuntu` ou `debian`).
2. Sélectionner ce conteneur et appuyer sur `s`.
3. Constater que c'est bien le shell `bash` qui s'ouvre (vérifier l'invite ou taper `echo $0`).
4. Démarrer un conteneur basé sur `alpine` (qui ne contient par défaut que `sh`).
5. Sélectionner ce conteneur et appuyer sur `s`.
6. Constater que le terminal bascule bien sur `sh` sans crasher.
7. Constater que la reprise du TUI à la fermeture de n'importe quel shell (`exit`) fonctionne correctement.
