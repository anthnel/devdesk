# Brief — Registre Docker Hub par défaut dans un nouveau contexte

> Statut : prêt pour relecture avant création du backlog. Idée cadrée, critique et recherche faites (voir
> `research.md`), décisions validées par l'utilisateur, faisabilité estimée par `dev-team:architect` (efforts
> confirmés) ; stories dans `stories.md`, pas encore de backlog.

## Vision en une phrase

Quand DevDesk crée un nouveau contexte, il y écrit d'emblée le registre Docker Hub (`docker.io`), pour que la
recherche et le pull d'images fonctionnent dès le premier lancement, sans configuration préalable.

## Problème

Aujourd'hui l'utilisateur doit configurer lui-même un registre avant de pouvoir puller ou chercher une
image : la liste de registres d'un contexte neuf est vide, et le premier lancement n'est pas bon.
Hypothèse : à vérifier (ce que l'utilisateur voit exactement avec une liste vide, dans `:oci` et dans la
recherche d'images ; la config d'un contexte neuf vient de `Default()`, voir « Hypothèses à vérifier »).

## Utilisateur

Le mainteneur, utilisateur principal de DevDesk, et tout nouvel utilisateur qui crée un contexte
(`:context new`, `dk setup` ou premier lancement).

## Périmètre MVP

- À la création d'un **nouveau** contexte, la config contient une entrée Docker Hub dans la liste plate
  `RegistryConfig.Registries` (voir `docs/architecture/registries.md`), avec ces valeurs (décision D-E) :
  `url: docker.io`, `slug: docker-io` (écrit explicitement), `kind: registry`, `provider` vide, `username`
  vide, `auth_mode: anonymous`, alias vide.
- Un constructeur unique de l'entrée (ex. `config.DefaultHubRegistry()`), utilisé par `newContextConfig` et
  `buildConfig` : une seule définition, un test de parité entre les deux.
- L'entrée est écrite dans le fichier du contexte comme n'importe quelle autre : modifiable et supprimable
  par l'utilisateur (via `RegistryForm`, `:cfg` ou à la main).
- Trois chemins de création couverts, et aucun autre (pas de création par MCP, pas d'import) : `:context new`
  (`CreateContext`, via `newContextConfig`), premier lancement (`CreateDefaultContextIfMissing`, le contexte
  `default` compte comme « nouveau »), `dk setup` (`buildConfig` puis `WriteNewContext`).
- Correctif rattaché au premier lancement (S-01) : `ensureDefaultContext` renvoie `created` et `main` relit la
  config dans ce cas (voir Risques).

## Non-objectifs

- Aucun ajout rétroactif : un contexte existant n'est jamais modifié. Cas particulier : un écrasement confirmé
  dans `dk setup` produit un contexte neuf (décision D-A), ce n'est pas du rétroactif.
- Pas de registre « intégré » ou non supprimable.
- Pas d'autres registres publics dans le MVP (ghcr.io, quay.io, registry.k8s.io, mcr.microsoft.com…).
- Pas d'écran de suggestions au premier lancement (variante C, écartée).
- Pas d'injection dans `Default()` ni `applyDefaults`.
- Hors épic : le bug de `dk setup` (perte du contenu du contexte après confirmation de l'écrasement), bug
  DEVDESK-662, créé séparément dans Plane.

## Décisions

1. MVP = Docker Hub seul (variante A).
2. Uniquement pour les nouveaux contextes, jamais pour les existants.
3. Entrée ordinaire de la config du contexte, pas un registre spécial.
4. Plus tard, éventuellement d'autres registres publics sans authentification, hors MVP, à valider un par
   un (certains n'exposent pas `_catalog`).
5. Écran de suggestions au premier lancement : écarté pour le MVP.
6. **Critique et recherche** : le verdict du critique, « pivoter », est écarté par l'utilisateur. Il CONTINUE
   avec l'écriture à la création (variante A).
7. **`auth_mode: anonymous`**, choisi par l'utilisateur contre la recommandation `credentials`. Conséquences
   acceptées, à documenter : la touche `U` (login/logout, `keys.go:129` puis `registries.go:111`) est refusée
   sur cette entrée anonyme (« This registry is marked anonymous »), alors que `E` ouvre le formulaire
   d'édition sans refus ; les limites anonymes de Docker Hub s'appliquent (100 pulls / 6 h / IP, 429
   possibles) ; le `docker login` de l'utilisateur n'est pas utilisé pour cette entrée.
8. **Chemins de création couverts** : `:context new` (`CreateContext` via `newContextConfig`), premier
   lancement (`CreateDefaultContextIfMissing`, le contexte `default` compte comme « nouveau »), `dk setup`
   (`buildConfig` puis `WriteNewContext`). Aucun autre chemin (architect).
9. **Point d'injection** : `newContextConfig` et `buildConfig`, JAMAIS `Default()` ni `applyDefaults` (repli de
   chargement de `main.go`, tests ; et une entrée supprimée reviendrait).
10. ~~Relancer `dk setup` sur un nom de contexte déjà existant ne rajoute pas `docker.io`.~~ **Remplacée par
    D-A.**
11. **Orthographe de l'URL** : tranchée, voir D-E (`docker.io`).
12. **Hub bloqué** (proxy ou réseau isolé) : entrée inutile et erreur affichée au premier usage ; impact
    faible, accepté.
13. **H3** (ce que la recherche et la navigation donnent sur une entrée `docker.io` anonyme) : à vérifier à
    la conception, pas avant (story S-04a). Le comportement sur 429 devient la story S-04b (D-C).

Décisions de l'utilisateur après la faisabilité :

- **D-A** : `dk setup` sur un contexte existant dont l'écrasement est CONFIRMÉ ajoute toujours `docker.io`. Un
  écrasement confirmé produit un contexte neuf : l'assistant reconstruit tout depuis `Default()`, anciens
  registres, moniteurs et forge sont perdus de toute façon ; ce n'est donc pas du rétroactif. Cela remplace
  l'ancienne décision 10. Le bug séparé de `dk setup` (perte du contenu après confirmation) est hors épic.
- **D-B** : alias de l'entrée VIDE. Un alias non vide réécrirait l'affichage de toutes les images
  `docker.io/...` dans `:oci` et `:sec`.
- **D-C** : S-04b ajoutée, priorité Could, effort S : rendre un 429 de Docker Hub lisible (ajouter une raison
  dans `registryFailureReason`, qui ne connaît pas 429 et affiche « Failed »), avec un faux serveur de test ;
  indépendante des autres stories.
- **D-E** (valeurs de l'entrée, issues de la faisabilité) : `url: docker.io` est la seule bonne orthographe
  (`index.docker.io` partirait vers `_catalog` et échouerait ; `oci.IsDockerHub` ne reconnaît que `docker.io`
  et `registry-1.docker.io`) ; `slug: docker-io` écrit explicitement (sinon il est dérivé au chargement et le
  fichier change à la première sauvegarde) ; `kind: registry`, `provider` vide, `username` vide,
  `auth_mode: anonymous`, alias vide (D-B).

## Hypothèses à vérifier

- **H1** : une liste vide est bien ce qui dégrade le premier lancement (constat à reproduire sur un
  contexte neuf : que montrent `:oci` et la recherche d'images ?). La recherche n'a trouvé aucune preuve
  directe de friction chez des outils comparables (`research.md`) ; à constater sur DevDesk même. Le constat
  du premier lancement (config en mémoire sans registre, voir Risques) en est déjà une illustration.
- **H2** : un seul point de création suffit. **Tranchée** : non, trois chemins, deux points d'injection
  (`newContextConfig` pour `CreateContext` et `CreateDefaultContextIfMissing`, `buildConfig` pour
  `dk setup`) ; décisions 8 et 9. Critères dans S-01 et S-02.
- **H3** : sur une entrée `docker.io` anonyme, Docker Hub n'a pas de navigation registre puis dépôts : on part
  d'une recherche (`HubSearcher`, GET anonyme `index.docker.io/v1/search`, API non contractuelle ; tags via
  `registry-1.docker.io` avec un jeton anonyme `auth.docker.io`). À vérifier en vrai, avec mesure datée :
  recherche, dépôt, tags, pull. Story S-04a, à faire en premier ; si elle échoue, elle devient un bug ou
  correctif distinct.
- **H4** : la migration au chargement (`normalizeRegistries`, ancien champ `registry.url`) ne recrée ni
  ne duplique l'entrée, et un contexte dont l'utilisateur a supprimé `docker.io` ne la voit pas revenir.
  Couverte par S-03.
- **H5** : `anonymous` est acceptable pour l'utilisateur au premier lancement (limite de 100 pulls / 6 h / IP,
  `U` refusée, login Docker non utilisé). Décision 7 ; Hypothèse: à confirmer à l'usage.

## Questions ouvertes

- Si H3 échoue (S-04a) : corriger la recherche, revoir `auth_mode` ou revoir la variante A (décision remise à
  l'utilisateur, avec le résultat de la mesure en main).
- Entrée de décision dans `docs/backlog.md` pour `anonymous` contre `credentials` ?

Questions closes : chemins de création (décision 8) ; le contexte `default` du premier lancement compte comme
nouveau (décision 8) ; orthographe de l'URL, slug et alias (D-E, D-B) ; garde contre l'écrasement dans
`dk setup` (D-A, bug séparé hors épic) ; efforts (confirmés par l'architect).

## Métriques de succès (observables)

- Un contexte créé par `:context new`, `dk setup` (fichier inexistant, ou écrasement confirmé) et au premier
  lancement contient `docker.io` en `auth_mode: anonymous` sans aucune action de l'utilisateur.
- Aucun contexte existant n'est modifié par la mise à jour : aucune entrée de registre ajoutée au chargement,
  au démarrage ni au changement de contexte (pour les fichiers sans ancien secret, octets identiques ;
  `MigrateLegacySecrets` réécrit un fichier qui contient un ancien secret).
- L'entrée peut être supprimée, et elle ne revient pas au rechargement ni à la sauvegarde.
- Un premier lancement sur un poste vierge affiche Docker Hub dans `:oci` sans redémarrage, et le premier
  `config.Save` de la session conserve l'entrée.
- Sur un contexte neuf, la recherche, la liste des tags et le pull passent par l'entrée (H3, S-04a).

## Risques

- **Élevé : premier lancement.** `main.go:73` appelle `config.Load()` avant `ensureDefaultContext()`
  (`main.go:98`). La config en mémoire vient donc de `Default()`, sans registre : `:oci` n'affiche pas Docker
  Hub avant un redémarrage, et le premier `config.Save` de la session réécrit `config.yaml` SANS l'entrée.
  Correctif S rattaché à S-01 : `ensureDefaultContext` renvoie `created`, `main` relit la config dans ce
  cas ; critère Given/When/Then dédié et test de `main.go`.
- Une entrée Docker Hub qui ne se comporte pas comme attendu en recherche (H3) donnerait un faux
  « registre configuré » : à valider (S-04a) avant la fusion de la documentation. L'API de recherche
  (`index.docker.io/v1/search`) n'est pas contractuelle : la mesure est datée et peut vieillir.
- Un écrasement ou un doublon à la migration ou à la sauvegarde (H4).
- Limites anonymes de Docker Hub : 100 pulls / 6 h par IP, quota partagé derrière un proxy ou NAT, 429
  possibles (affichés « Failed » tant que S-04b n'est pas faite) ; Docker peut durcir ces limites (préavis de
  6 mois promis). Accepté (décision 7).
- Hub inaccessible (proxy, réseau isolé) : entrée inutile et erreur au premier usage ; accepté (décision 12).
- Écrire un slug dérivé ou une autre orthographe d'URL ferait changer le fichier à la première sauvegarde ou
  casserait la recherche : évité par D-E et le constructeur unique.
