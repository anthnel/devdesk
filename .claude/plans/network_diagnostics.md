# Implémentation : Gestion et Diagnostic Réseau Avancés

Ce document décrit le plan technique pour l'ajout des fonctionnalités de diagnostic réseau dans DevDesk, particulièrement depuis la vue OCI (`internal/ui/oci_resources`).

## 1. Visualisation Topologique (Network Inspect)
**Objectif :** Permettre au développeur de voir quels conteneurs sont connectés à un réseau spécifique et avec quelles adresses IP.

### Changements proposés :
- **Modifier `internal/ui/oci_resources/update.go`** : 
  - Ajouter la gestion de la touche `enter` dans `handleNetworksKeyMsg`.
  - Sur `enter`, déclencher une action asynchrone pour inspecter le réseau sélectionné via l'API Docker (`docker network inspect`).
- **Créer `internal/ui/oci_resources/network_inspect_form.go`** :
  - Créer un composant BubbleTea (ex: `NetworkInspectForm`) qui affichera un tableau temporaire (ou viewport) par-dessus l'interface principale.
  - Le tableau listera les conteneurs connectés : `Nom du Conteneur`, `IPv4`, `MacAddress`.
  - Gérer la touche `esc` pour fermer cette vue et revenir à la liste des réseaux.
- **Ajouter à `internal/ui/oci_resources/model.go`** :
  - Un pointeur `networkInspectForm *NetworkInspectForm` dans la structure `Model`.
- **Modifier `internal/ui/oci_resources/view.go`** :
  - Dans `View()`, si `networkInspectForm` n'est pas nil, afficher ce composant en priorité (tout comme `launchForm` ou `resourceForm`).

## 2. Ping/Curl Inter-Contenants (Connectivity Test)
**Objectif :** Un utilitaire interactif pour lancer un test réseau depuis un conteneur vers un autre.

### Changements proposés :
- **Créer `internal/ui/oci_resources/connectivity_form.go`** :
  - Un formulaire avec les champs :
    - *Conteneur Source* (le conteneur depuis lequel on fait le test, pré-sélectionné si on vient de la vue Topologie).
    - *Cible* (IP ou nom/alias DNS cible).
    - *Type de test* (Menu déroulant : `Ping (ICMP)`, `HTTP GET (cURL)`, `Port Check (nc)`).
    - *Port* (actif si HTTP ou Port Check).
- **Intégration du formulaire :**
  - Depuis la vue *Topologie* (Network Inspect), l'utilisateur appuie sur `c` (Connectivity) sur un conteneur donné. Le formulaire de connectivité s'ouvre, prenant ce conteneur comme source.
- **Exécution d'un "Conteneur de Diagnostic" (`internal/docker/`)** :
  - Au lieu de s'appuyer sur les outils (potentiellement absents) du conteneur source, nous allons lancer un **conteneur éphémère** utilisant l'image `wbitt/network-multitool`.
  - Ce conteneur sera attaché dynamiquement au **même réseau** que la cible à tester (et supprimé automatiquement après exécution avec `--rm`).
  - Ajouter une méthode dans le package `docker` pour lancer ce conteneur (via l'API Docker) de manière non-interactive, exécuter la commande, récupérer la sortie texte, puis le détruire.
  - Exécuter la commande selon le type :
    - Ping: `ping -c 3 <cible>`
    - Curl: `curl -v -s -m 5 http://<cible>:<port>`
    - Netcat: `nc -zv -w 5 <cible> <port>`
- **Affichage des résultats :**
  - Une fois la commande asynchrone terminée, afficher le résultat `stdout`/`stderr` dans un composant `ReportModal` (qui existe déjà dans les composants partagés).

## Prochaines étapes de développement (Execution)
1. Création de la récupération de la "Topologie" via l'inspection des réseaux.
2. Câblage de l'interface graphique OCI pour afficher l'inspect.
3. Création du formulaire de "Connectivity Test".
4. Implémentation du backend Docker pour exécuter le test de manière asynchrone.
5. Gestion des erreurs (ex: outil `curl` ou `ping` manquant dans le conteneur source).
