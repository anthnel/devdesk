# Plan Technique : Refonte et Unification de la vue Workspace

Ce document constitue la source unique de vérité pour l'unification de la vue **Workspace** avec l'expérience de la vue **Explorer**. Il combine les objectifs de design, l'architecture technique et le plan d'implémentation.

## 1. Principes de Design et Navigation

- **Mode de Navigation** : `navigate-in` (identique à l'Explorer).
- **Navigation (Breadcrumbs)** : Une ligne de "tabs" en bas de l'écran affichant le chemin depuis la racine (ex: `󰋜 home  >  󰉋 repos  >  󰊢 devdesk`).
- **Esthétique** : 
    - **Aucune icône de dossier ou de projet** dans la table (priorité à la clarté textuelle).
    - Usage exclusif de **Nerd Fonts - Code Point Unique**.
    - Format de date **Relatif** (`time.Since`) : ex: "1 hour ago".

## 2. Structure de la Table et Iconographie

| Colonne | Format / Icône | Description | Priorité |
| :--- | :--- | :--- | :--- |
| **Name** | `filename` | Nom de l'entrée (sans icône de dossier). | Haute |
| **Git Status** | ` branch` `icons` | Branche active et indicateurs de synchro/modifs. | Haute |
| **Security** | `󰕥` score / `󰕥` | Résultats de scan (Secrets, Vulns) et indicateur de cache. | Haute |
| **Type** | `Go`, `Node`, `Python` | Texte décrivant le type de projet/langage. | Moyenne |
| **Modified** | `1 hour ago` | Date relative de dernière modification. | Moyenne |
| **Size** | `1.2 MB` | Taille (fichiers uniquement). | Basse |
| **Permissions** | `rwxr-xr-x` | Autorisations Unix. | Basse |

### Référentiel des Icônes (internal/ui/theme/icons.go)
| Clé | Icône | Code Point | Usage |
| :--- | :--- | :--- | :--- |
| `IconGitBranch` | `` | `\ue725` | Branche Git active |
| `IconGitUntracked` | `` | `\ueeb1` | Fichiers non suivis |
| `IconGitUnpushed` | `` | `\uf403` | Commits non poussés |
| `IconGitUnpulled` | `` | `\ueb40` | Commits non récupérés |
| `IconGitModified` | `` | `\uf040` | Fichiers modifiés |
| `IconSecurity` | `󰕥` | `\uebc1` | Secrets / Alertes de sécurité |

## 3. Architecture Technique

### Modèle de Données (internal/ui/workspaces/model.go)
- **Struct `Entry`** :
    - `GitBranch`, `GitStatus` (Modified, Untracked)
    - `GitUnpushed`, `GitUnpulled` (counts)
    - `HasSecrets`, `SecurityStale` (bool)
    - `ProjectType` (string)
    - `RelativeTime` (string)

### Logiques de Détection (`loadEntries`)
1. **Git** : 
    - `git status --porcelain` pour l'état local.
    - `git rev-list --count @{u}..HEAD` et `HEAD..@{u}` pour la synchro.
2. **Sécurité & Cache** :
    - Manager de cache persistant dans `~/.config/devdesk/security_cache/`.
    - **Staleness** : `ModTime` fichier vs `ScanTime` cache.
    - **Shortcut `A`** : Scan récursif complet (All) d'un workspace en arrière-plan.
3. **Type de Projet** : Recherche de signatures (`go.mod`, `package.json`, `.git`).

## 4. Plan de Vérification

### Tests Automatisés
- Logiciel de calcul de date relative.
- Parsing des statuts Git.
- Logique de "Staleness" du cache de sécurité.

### Vérification Manuelle
1. Naviguer et vérifier les breadcrumbs en bas.
2. Vérifier l'apparition des icônes Git lors d'une modification.
3. Vérifier le changement de couleur/icône du bouclier si un fichier est modifié après un scan.
4. Lancer un scan complet avec `A` et vérifier la persistance après redémarrage.
