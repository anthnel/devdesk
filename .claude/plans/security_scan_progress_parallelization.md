# Security Scan Progress and Parallelization

Le but est d'améliorer l'expérience utilisateur lors des scans de sécurité en exécutant les étapes en parallèle et en fournissant un retour visuel détaillé pour chaque étape, y compris le téléchargement de la base de données Trivy.

## Contexte

Les scans de sécurité peuvent prendre plusieurs minutes. Actuellement, ils s'exécutent de manière séquentielle et le retour visuel est limité à un spinner et un libellé d'étape globale. L'utilisateur souhaite voir la progression de chaque étape (Trivy vuln, secrets, misconfig, etc.) avec des barres de progression dédiées, et que les étapes indépendantes s'exécutent simultanément.

## Modification proposées

### [internal/scan]

#### [scanner.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/scan/scanner.go)
- Introduire une structure `ProgressUpdate` pour des notifications structurées.
- Remplacer `OnStage func(string)` par `OnProgress func(ProgressUpdate)` dans `ScanOptions`.
- Utiliser `errgroup` dans `Scanner.Scan` pour paralléliser les appels aux différents scanners (Vuln, Secrets, Misconfig, License, SBOM).

#### [trivy.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/scan/trivy.go)
- Refactoriser `RunTrivy` pour capturer la sortie `stderr` afin de suivre la progression du téléchargement des bases de données de vulnérabilités.
- Envoyer des mises à jour de progression régulières via le nouveau callback.

#### [gitleaks.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/scan/gitleaks.go)
- Ajouter le support des notifications de progression (même si Gitleaks est souvent rapide, cela permet une cohérence d'interface).

### [internal/ui/security]

#### [model.go](file:///home/anthoni/projects/gitlab/devsecops/devdesk/internal/ui/security/model.go)
- Étendre l'état pour gérer une liste de barres de progression (via `bubbles/progress`).
- Mettre à jour la vue de scan (`StateScanning`) pour afficher dynamiquement la liste des tâches en cours avec leurs barres respectives.
- Gérer les messages de mise à jour de progression pour rafraîchir le TUI.

## Plan de vérification

### Tests Automatisés
- `go test ./internal/scan/...` pour vérifier que la parallélisation ne casse pas l'agrégation des résultats.
- Tests unitaires pour le parseur de logs Trivy (progression DB).

### Vérification Manuelle
1. Lancer un scan complet sur un dépôt Git.
2. Vérifier que plusieurs barres de progression apparaissent simultanément.
3. Vérifier que si Trivy télécharge sa base de données, la progression est visible.
4. Annuler le scan pendant l'exécution et vérifier que tous les processus parallèles sont proprement terminés.
