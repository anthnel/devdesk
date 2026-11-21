# Stories — Registre Docker Hub par défaut dans un nouveau contexte

Brief : `brief.md`. Recherche : `research.md` (2026-11-21). Backlog : créé dans Plane le 2026-11-21, module « Registre par défaut ». Priorités MoSCoW ; efforts S/M/L confirmés par `dev-team:architect` (faisabilité rapide) :
tous S, aucun L.

## Ordre de livraison recommandé

1. **S-04a tout de suite**, en parallèle de S-01 puis S-02 puis S-03, sur un fichier de contexte écrit à la
   main (une entrée `docker.io` anonyme) : si H3 échoue, on le sait tôt. Mesure réelle, bornée à une session.
2. **S-01** (point d'injection `newContextConfig`, constructeur unique, correctif du premier lancement), puis
   **S-02** (`dk setup`, même constructeur), puis **S-03** (non-régression : ses tests s'écrivent avec S-01
   et S-02, elle est fusionnée après elles).
3. **S-04b** (429 lisible, Could) : indépendante de toutes les autres, à prendre quand on veut.
4. **S-05** (documentation) en dernier : elle décrit le comportement réellement livré, résultat de S-04a et
   limites de `anonymous` compris.

Dépendances : S-02 dépend de S-01 (même constructeur d'entrée). S-03 dépend de S-01 et S-02. S-05 dépend de
S-01, S-02 et S-04a. S-04a et S-04b ne dépendent de personne.

## Vue d'ensemble

| ID | Plane | Titre | Priorité | Effort | Dépend de |
|---|---|---|---|---|---|
| S-01 | DEVDESK-663 | Un contexte créé par `:context new` ou au premier lancement contient Docker Hub | Must | S | |
| S-02 | DEVDESK-664 | `dk setup` écrit Docker Hub, écrasement confirmé compris | Must | S | S-01 |
| S-03 | DEVDESK-665 | Non-régression : contextes existants inchangés, suppression durable, aucun doublon | Must | S | S-01, S-02 |
| S-04a | DEVDESK-666 | Vérifier recherche, tags et pull sur l'entrée Docker Hub anonyme (mesure réelle) | Must | S | |
| S-04b | DEVDESK-667 | Rendre lisible un 429 de Docker Hub | Could | S | |
| S-05 | DEVDESK-668 | Documentation du registre par défaut et des limites de `anonymous` | Should | S | S-01, S-02, S-04a |

Hors épic : bug DEVDESK-662 (`dk setup` perd le contenu du contexte après confirmation de l'écrasement),
créé séparément dans Plane.

## Décisions prises (utilisateur)

- **Critique et recherche faites.** Le verdict du critique, « pivoter », est écarté par l'utilisateur : il
  CONTINUE avec l'écriture à la création (variante A).
- **`auth_mode: anonymous`**, choisi par l'utilisateur contre la recommandation `credentials`. Conséquences
  acceptées : la touche `U` (login/logout, `keys.go:129` puis `registries.go:111`) est refusée sur l'entrée
  anonyme (« This registry is marked anonymous »), `E` n'est PAS refusée (elle ouvre le formulaire
  d'édition) ; les limites anonymes de Docker Hub s'appliquent (100 pulls / 6 h / IP, 429 possibles) ; le
  `docker login` de l'utilisateur n'est pas utilisé pour cette entrée. À documenter (S-05).
- **Chemins de création couverts**, et aucun autre : `:context new` (`CreateContext`, via `newContextConfig`),
  premier lancement (`CreateDefaultContextIfMissing`, le contexte `default` compte comme « nouveau »),
  `dk setup` (`buildConfig` puis `WriteNewContext`).
- **Point d'injection** : `newContextConfig` et `buildConfig`, JAMAIS `Default()` ni `applyDefaults` (repli de
  chargement de `main.go`, tests ; et une entrée supprimée reviendrait au rechargement).
- **D-A** : `dk setup` sur un contexte existant dont l'écrasement est CONFIRMÉ ajoute toujours `docker.io` :
  l'écrasement confirmé produit un contexte neuf (tout est reconstruit depuis `Default()`), ce n'est pas du
  rétroactif. Remplace l'ancienne décision « relancer setup ne rajoute pas docker.io ».
- **D-B** : alias de l'entrée VIDE (un alias non vide réécrirait l'affichage de toutes les images
  `docker.io/...` dans `:oci` et `:sec`).
- **D-C** : S-04b (429 lisible), Could, S, indépendante.
- **D-E** : valeurs de l'entrée : `url: docker.io`, `slug: docker-io` (écrit explicitement), `kind: registry`,
  `provider` vide, `username` vide, `auth_mode: anonymous`, alias vide. `index.docker.io` partirait vers
  `_catalog` et échouerait ; `oci.IsDockerHub` ne reconnaît que `docker.io` et `registry-1.docker.io`.
- **Hub bloqué** (proxy, réseau isolé) : entrée inutile et erreur affichée au premier usage ; impact faible,
  accepté.

Conventions des critères (stories de code) : les touches de test se construisent via `internal/ui/testutil` ;
tests table-driven ; `go test -count=2` passe (HOME isolé : `isolateHome(t)` dans `internal/app` ; ailleurs
`t.Setenv("HOME", t.TempDir())` et `USERPROFILE`) ; commentaires et messages de commit en anglais ; aucun
test n'atteint le réseau réel ni le trousseau de la machine (`fakekeyring`).

---

## Epic « Registre par défaut »

### S-01 — Un contexte créé par `:context new` ou au premier lancement contient Docker Hub (DEVDESK-663)
Epic : Registre par défaut — Priorité : Must — Effort : S (confirmé par l'architect)

En tant que nouvel utilisateur de DevDesk, je veux qu'un contexte que je viens de créer (par `:context new` ou
au tout premier lancement) contienne déjà le registre Docker Hub afin de voir un registre dans `:oci` sans
rien configurer, dès ce premier lancement.

**Critères d'acceptation**
1. Étant donné un nom de contexte valide et sans fichier, quand `config.CreateContext(nom)` s'exécute, alors le
   fichier écrit contient exactement une entrée de registre : `url: docker.io`, `slug: docker-io`,
   `kind: registry`, `provider` vide, `username` vide, `auth_mode: anonymous`, alias vide, sans champ ni
   marqueur « intégré ». La config relue par `config.Load` la renvoie telle quelle, sans doublon, et une
   première sauvegarde ne change pas le fichier.
2. Étant donné un poste vierge (pas de `config.yaml`, contexte courant `default`), quand
   `CreateDefaultContextIfMissing()` s'exécute, alors `config.yaml` contient la même entrée.
3. Étant donné un poste vierge, quand `dk` démarre pour la première fois, alors `:oci` affiche Docker Hub sans
   redémarrage, et le premier `config.Save` de la session conserve l'entrée (`ensureDefaultContext` renvoie
   `created` et `main` relit la config dans ce cas ; un test de `main.go` le couvre, y compris le cas
   « contexte déjà présent » où rien n'est relu).
4. Étant donné l'entrée créée, quand l'utilisateur ouvre `:oci` et appuie sur `U` sur cette ligne, alors le
   message « This registry is marked anonymous » s'affiche (comportement existant, non modifié) ; la touche
   `E` ouvre le formulaire d'édition comme pour toute entrée.
5. Étant donné `config.Default()` et `applyDefaults`, quand ils sont appelés, alors la liste de registres
   renvoyée est vide, inchangée : un test échoue si l'injection y est déplacée. Un constructeur unique de
   l'entrée (ex. `config.DefaultHubRegistry()`) sert `newContextConfig` et `buildConfig`, et un test de parité
   vérifie que les deux produisent la même entrée.
6. Étant donné un nom de contexte déjà pris, quand `:context new` s'exécute, alors l'erreur `ErrContextExists`
   est renvoyée comme avant et le fichier existant est intact.

**Hors périmètre** : `dk setup` (S-02) ; les contextes existants (S-03) ; d'autres registres publics ;
tout comportement de recherche ou de pull (S-04a, S-04b) ; le repli de chargement `Default()` de `main.go`
quand `Load` échoue (inchangé).
**Hypothèses** : Hypothèse: `main` n'a besoin de relire la config que lorsque `ensureDefaultContext` a créé
le contexte (cas `created`). Les tests d'`internal/app` et de `main.go` isolent HOME.
**Questions ouvertes** : aucune.

---

### S-02 — `dk setup` écrit Docker Hub, écrasement confirmé compris (DEVDESK-664)
Epic : Registre par défaut — Priorité : Must — Effort : S (confirmé par l'architect) — Dépend de : S-01

En tant que nouvel utilisateur qui configure son contexte par `dk setup`, je veux que Docker Hub y figure aussi
afin d'avoir le même premier lancement que par `:context new`.

**Critères d'acceptation**
1. Étant donné un nom de contexte sans fichier, quand l'assistant `dk setup` va jusqu'à l'écriture
   (`buildConfig` puis `WriteNewContext`), alors le fichier contient la même entrée Docker Hub qu'en S-01
   (mêmes valeurs, une seule fois, issue du même constructeur).
2. Étant donné un contexte existant dont l'écrasement est confirmé, quand l'assistant écrit son résultat,
   alors le fichier a le même contenu qu'un contexte neuf, Docker Hub compris (D-A) : un écrasement confirmé
   produit un contexte neuf, pas un ajout rétroactif.
3. Étant donné `buildConfig(m)` appelé sur un modèle quelconque, quand on lit son résultat, alors l'entrée y
   figure une seule fois et `config.Default()` ne renvoie toujours aucun registre ; un test de parité
   `newContextConfig` / `buildConfig` échoue si les deux entrées divergent.
4. Étant donné une erreur d'écriture, quand `WriteNewContext` échoue, alors l'erreur est remontée comme
   aujourd'hui (`ContextSavedMsg.Err`).

**Hors périmètre** : le comportement de `WriteNewContext` quand le fichier existe (il remplace toujours le
fichier ; inchangé) ; la perte du contenu du contexte après confirmation de l'écrasement (bug
DEVDESK-662, séparé, hors épic) ; l'atomicité de l'écriture (`os.WriteFile` n'est pas atomique,
hors sujet) ; `:context new` et le premier lancement (S-01).
**Hypothèses** : Hypothèse: l'assistant n'ajoute aucune garde de plus contre l'écrasement.
**Questions ouvertes** : aucune.

---

### S-03 — Non-régression : contextes existants inchangés, suppression durable, aucun doublon (DEVDESK-665)
Epic : Registre par défaut — Priorité : Must — Effort : S (confirmé par l'architect) — Dépend de : S-01, S-02

En tant qu'utilisateur d'un contexte existant, je veux qu'aucun de mes contextes ne reçoive Docker Hub à la
mise à jour, et qu'un Docker Hub que j'ai supprimé ne revienne pas, afin de garder la maîtrise de ma
configuration.

**Critères d'acceptation**
1. Étant donné un répertoire de contextes contenant des fichiers de formes variées (liste de registres vide,
   plusieurs registres, ancien champ `registry.url`), quand DevDesk démarre, charge et change de contexte,
   alors aucune entrée de registre n'est ajoutée ; pour les fixtures sans ancien secret, les fichiers sont
   en outre identiques octet pour octet (un fichier avec un ancien secret est réécrit par
   `MigrateLegacySecrets`, ce qui est attendu et hors du critère « octets identiques »).
2. Étant donné un contexte neuf contenant l'entrée Docker Hub, quand l'utilisateur la supprime (via
   `RegistryForm`, `:cfg` ou à la main), la config est sauvegardée, puis rechargée et sauvegardée de nouveau,
   alors l'entrée ne revient à aucun moment (rechargement, `config.Save`, redémarrage).
3. Étant donné un contexte dont la liste de registres est vide et dont `registry.url` est vide, quand il est
   chargé, alors la liste reste vide (ni `normalizeRegistries` ni `applyDefaults` n'ajoutent `docker.io`).
4. Étant donné un fichier contenant l'ancien champ `registry.url` (migration au chargement) ou déjà une entrée
   `docker.io`, quand il est chargé et sauvegardé, alors il y a au plus une entrée par URL et aucune entrée
   Docker Hub supplémentaire n'est créée.
5. Étant donné le repli de chargement de `main.go` et les tests qui s'appuient sur `config.Default()`, quand ils
   s'exécutent, alors leur config n'a toujours aucun registre ; aucun test existant ne suppose une liste vide
   (constat de l'architect), donc aucun n'est modifié pour absorber l'entrée.
6. Étant donné `go test ./...` sur le dépôt, quand il s'exécute, alors il est vert, `-count=2` compris, sans
   réécriture de fixtures de config existantes.

**Hors périmètre** : `dk setup` (S-02) ; une migration qui ajoute Docker Hub aux contextes existants
(non-objectif).
**Hypothèses** : Hypothèse: `config.Save` n'applique pas de défaut de registres (vérifié par le test du
critère 2). Les tests tiennent dans `internal/config` et `internal/app`, sans réseau ni trousseau réel.
**Questions ouvertes** : aucune.

---

### S-04a — Vérifier recherche, tags et pull sur l'entrée Docker Hub anonyme (mesure réelle) (DEVDESK-666)
Epic : Registre par défaut — Priorité : Must — Effort : S (confirmé par l'architect) — Dépend de : aucune

En tant que nouvel utilisateur, je veux que l'entrée Docker Hub par défaut permette réellement de chercher,
de voir les tags et de puller une image afin que le registre affiché ne soit pas un faux « registre
configuré » (H3).

Spike borné à une session, à faire EN PREMIER : sur Docker Hub il n'y a pas de navigation registre puis
dépôts ; on part d'une recherche (`HubSearcher`, GET anonyme `index.docker.io/v1/search`, API non
contractuelle ; tags via `registry-1.docker.io` avec un jeton anonyme `auth.docker.io`). Le compte rendu est
daté, dans `docs/measurements/`. Un correctif n'est écrit que si la vérification échoue, dans une story
distincte (critère 4) ; la story ne bloque pas S-01 à S-03, mais S-05 attend son résultat.

**Critères d'acceptation**
1. Étant donné un contexte dont la seule entrée est Docker Hub en `anonymous` (écrite à la main), quand
   l'utilisateur ouvre `:oci` et lance une recherche d'images (par exemple `nginx`), alors des résultats
   s'affichent, sans identifiants ni configuration préalable ; le chemin de code réellement emprunté
   (`HubSearcher`, `internal/oci/hub.go`, `internal/imagesearch`, `_catalog` ou autre) est consigné.
2. Étant donné un résultat de recherche, quand l'utilisateur ouvre le dépôt (par exemple `library/nginx`),
   alors la liste des tags s'affiche, et le pull d'une image publique réussit sans `docker login`.
3. Étant donné un Hub injoignable (proxy ou réseau isolé), quand l'utilisateur utilise l'entrée, alors une
   erreur est affichée au premier usage, et l'entrée reste en place (impact faible accepté, aucune
   suppression automatique).
4. Étant donné un résultat qui contredit un des critères 1 à 3, quand il est consigné, alors chaque écart est
   décrit (étape, message observé) et une story de correction ou un bug distinct est proposé à
   l'utilisateur, avec la décision à prendre : corriger le code (par exemple un traitement Hub dans la
   recherche), ou revoir `auth_mode`, ou revoir la variante A. Aucune de ces décisions n'est prise par la
   story.
5. Étant donné la vérification, quand elle est faite, alors les orthographes d'URL essayées (`docker.io`,
   `index.docker.io`, `registry-1.docker.io`) sont consignées avec leur résultat, pour confirmer D-E.

**Hors périmètre** : les autres registres publics ; l'affichage du quota restant (`ratelimitpreview`) ; la
lisibilité d'un 429 (S-04b) ; la correction d'un défaut trouvé (story distincte, critère 4) ; le pull sur le
vrai Docker Hub dans la suite de tests automatisée (les tests restent hors réseau).
**Hypothèses** : Hypothèse: la vérification se fait une fois sur le vrai Hub, hors CI, et son résultat est
daté (l'API de recherche n'est pas contractuelle, son comportement peut changer).
**Questions ouvertes** : aucune (la décision en cas d'échec est remise à l'utilisateur, au critère 4).

---

### S-04b — Rendre lisible un 429 de Docker Hub (DEVDESK-667)
Epic : Registre par défaut — Priorité : Could — Effort : S (confirmé par l'architect) — Dépend de : aucune

En tant qu'utilisateur anonyme de Docker Hub, je veux qu'un 429 (limite de débit) soit expliqué afin de savoir
que j'ai atteint la limite anonyme, au lieu de lire « Failed ».

**Critères d'acceptation**
1. Étant donné un faux serveur de test (jamais le vrai Hub) qui répond 429, quand une vérification ou une
   requête de l'entrée passe par `registryFailureReason`
   (`internal/ui/oci_resources/registry_failure.go`, lignes 18 à 31 environ), alors la raison affichée dans le
   pied de page (niveau Erreur, règle 128) mentionne la limite de débit (rate limit), en anglais US, et non
   plus « Failed ».
2. Étant donné la même réponse 429, quand elle est reçue, alors DevDesk ne plante pas, ne boucle pas en
   réessais et ne vide pas la table (règle 139).
3. Étant donné les autres statuts déjà connus de `registryFailureReason`, quand le test table-driven
   s'exécute, alors leurs raisons sont inchangées.

**Hors périmètre** : l'affichage du quota restant ; un réessai automatique ou une attente (`Retry-After`) ;
le chemin du pull s'il ne passe pas par `registryFailureReason` (à constater, story distincte si besoin).
**Hypothèses** : Hypothèse: `registryFailureReason` reçoit assez d'information (statut HTTP typé,
`StatusError`) pour reconnaître un 429 sans changer la couche `internal/oci`.
**Questions ouvertes** : aucune.

---

### S-05 — Documentation du registre par défaut et des limites de `anonymous` (DEVDESK-668)
Epic : Registre par défaut — Priorité : Should — Effort : S (confirmé par l'architect) — Dépend de : S-01, S-02, S-04a

En tant que nouvel utilisateur (et agent IA qui lit les notes d'architecture), je veux que la documentation dise
quels contextes reçoivent Docker Hub, pourquoi, et ce que `anonymous` implique afin de ne pas être surpris par
un refus de `U`, un 429 ou un login ignoré.

**Critères d'acceptation**
1. Étant donné `docs/architecture/registries.md`, quand on le lit, alors il décrit l'entrée par défaut : les
   trois chemins de création couverts (`:context new`, premier lancement, `dk setup`, écrasement confirmé
   compris), le point d'injection (`newContextConfig` et `buildConfig`, constructeur unique, jamais
   `Default()` ni `applyDefaults`) et la raison, la règle « un contexte existant n'est jamais modifié »,
   les valeurs de l'entrée (D-E, alias vide et pourquoi) avec la raison de `docker.io`, la relecture de la
   config au premier lancement, et le résultat de la vérification de S-04a.
2. Étant donné `docs/architecture/configuration.md`, quand on le lit, alors il précise que la liste de registres
   d'un contexte neuf contient l'entrée Docker Hub, que les contextes existants ne sont pas migrés et que
   l'entrée supprimée ne revient pas.
3. Étant donné la page utilisateur (le `README.md`, section configuration et registres ; Hypothèse: il n'existe
   pas d'autre page utilisateur), quand on la lit, alors elle indique : l'entrée est ordinaire et supprimable ;
   la touche `U` (login/logout) est refusée sur une entrée `anonymous` (« This registry is marked
   anonymous ») ; les limites anonymes de Docker Hub s'appliquent (100 pulls / 6 h / IP, avec un 429
   possible, quota partagé derrière un proxy ou NAT) ; le `docker login` de l'utilisateur n'est pas utilisé
   pour cette entrée ; comment passer à des identifiants (modifier l'entrée avec `E` pour `auth_mode:
   credentials`, ou la recréer).
4. Étant donné le texte documenté, quand il est comparé à la recherche (`research.md`), alors les chiffres
   cités portent leur date de consultation (2026-11-21) et ne sont pas présentés comme garantis (Docker peut
   les durcir avec 6 mois de préavis).
5. Étant donné les règles du dépôt, quand les changements sont livrés, alors le texte de l'interface reste en
   anglais US et les notes sous `docs/architecture/` sont mises à jour dans le même commit que le code qu'elles
   décrivent (S-01 et S-02) ; seule la partie qui dépend de S-04a (et de S-04b si elle est livrée) voyage
   avec S-05.

**Hors périmètre** : un changement du comportement de `U` ou `E` sur une entrée `anonymous` ; un affichage du
quota restant ; la documentation d'autres registres publics ; le bug `dk setup` DEVDESK-662.
**Hypothèses** : Hypothèse: la mise à jour des notes d'architecture suit le code (même commit), et S-05 couvre
ce qui reste, la page utilisateur et les limites. Hypothèse: aucune entrée de décision `docs/backlog.md` n'est
requise ; si l'utilisateur en veut une pour `anonymous` contre `credentials`, elle s'ajoute ici.
**Questions ouvertes** : une entrée de décision dans `docs/backlog.md` pour le choix `anonymous` ?

---

## Questions ouvertes (à trancher par l'utilisateur)

1. **Si S-04a échoue** (H3) : corriger la recherche, revoir `auth_mode` ou revoir la variante A. La décision est
   à prendre avec la mesure en main.
2. **Décision `docs/backlog.md`** pour `anonymous` contre la recommandation `credentials` (voir S-05).

Tranchées et retirées : orthographe de l'URL, slug et alias (D-E, D-B) ; garde contre l'écrasement dans
`dk setup` (D-A ; bug séparé DEVDESK-662, hors épic) ; efforts (confirmés par l'architect).
