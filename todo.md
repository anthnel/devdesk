# Idées de Fonctionnalités (Brainstorming)

## 1. 🕸️ Gestion et Diagnostic Réseau Avancés
- [x] **Visualisation topologique :** Un écran montrant quels conteneurs communiquent sur quels réseaux Docker pré-existants.
- [x] **Ping/Curl inter-conteneurs :** Une interface permettant d'exécuter rapidement un test de connectivité (ex: "Depuis *Conteneur A*, fais une requête HTTP vers *Conteneur B* : port 8080") sans avoir à ouvrir un shell manuellement.
- [ ] **Gestionnaire de "Port Forwarding" :** Un tableau de bord interactif pour gérer les redirections de ports vers la machine hôte.

## 2. 🛡️ Remédiation Sécurité Interactive (DevSecOps)
- [ ] **Assistance à la mise à jour (Auto-Patch) :** Après le scan Trivy, proposer de générer automatiquement un patch ou un `Dockerfile` mis à jour changeant la version de l'image de base pour corriger les CVE critiques.
- [ ] **Scan au niveau du code (SAST local) :** Intégrer des outils de linting de sécurité (comme `gitleaks` pour les secrets ou des linters IaC) directement dans la vue "Workspace".

## 3. 📉 Analyseur de Build et de Cache OCI
- [ ] **Visualisation des calques (Layers) :** Analyser une image locale pour afficher la taille de chaque couche (à la manière de l'outil `dive`).
  - Implémentation intégrée via `google/go-containerregistry` (approche "daemonless" sans dépendance au démon Docker).
  - Interaction directe avec les registres OCI (pull des manifestes et configs sans télécharger l'image entière).
  - Lecture en streaming des calques en mémoire (format `tar`) pour recréer l'arborescence des fichiers système.
  - Calcul de l'espace gaspillé via la détection des fichiers whiteouts (`.wh.`).
- [ ] **Analyse de "Cache Miss" :** Indiquer pourquoi la mise en cache d'un build a échoué (ex: "Le fichier package.json a été modifié, invalidant le cache").

## 4. 🚀 "Injection" d'Environnement (Dev Containers Spontanés)
- [ ] **Injection de configuration :** Permettre d'injecter les dotfiles du développeur (configuration vim, zsh, alias) dynamiquement lors de l'ouverture d'un terminal dans un conteneur.
- [ ] **Montage de volume "à chaud" :** Ajouter la possibilité de monter le répertoire de travail actuel (`pwd`) dans un conteneur existant à la volée pour tester un script local sans avoir à rebuild l'image.

## 5. 📊 Tableau de bord des Ressources (Mini-htop intégré)
- [ ] **Graphiques TUI :** Une vue avec des graphiques en TUI (utilisant des caractères braille) représentant l'utilisation en temps réel du CPU, de la RAM et des I/O réseau pour tous les conteneurs du projet sélectionné.
- [ ] **Alertes visuelles :** Possibilité de définir des alertes visuelles (ex: saturation mémoire).

faire le plan : c:\Users\anthoni\.gemini\antigravity\worktrees\devdesk\ux-feature-audit-improvements\.claude\plans\platform_compatibility_improvements.md