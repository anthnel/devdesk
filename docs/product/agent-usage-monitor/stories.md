# Agent usage monitor — stories (post-feasibility)

Decisions: command `:agent-usage` / `:au`; the 2% reference is the `cost-state` lines, ccusage as cross-check; no task titles (meta.description, ai-title) in the MVP; keep the last line per message.id when deduplicating; read lines with a capped reader (lines up to 1.3 MB); polling tick 2 s.
Delivery order: S-01 -> S-02, S-05a -> S-03a -> S-03b, S-04 -> S-05b -> S-06 -> S-07 -> S-08.
Context: see brief.md and feasibility.md (MVP) and research.md. Effort is left blank until the
architect's feasibility pass. Rules: UI text in English US (Rule 129), numeric
aggregates only (never prompt or tool-output content).

## Epic E1 — Read agent usage from Claude Code transcripts

### S-01 — Spike: confirm the gap and the sandbox constraint
Epic : E1 — Priorité : Must — Effort : S — Plane : DEVDESK-39

En tant que mainteneur de DevDesk, je veux confirmer ce que ccusage ne fait pas et si les transcripts des sessions `sbx` sont lisibles depuis l'hôte, afin de ne pas construire un doublon.

**Critères d'acceptation**
1. Étant donné 3 sessions récentes avec sous-agents, quand ccusage est lancé dessus, alors un tableau note, pour chacune, s'il donne les tokens par sous-agent (oui/non).
2. Étant donné une session lancée dans un sandbox `sbx`, quand on cherche son transcript sur l'hôte, alors le résultat (trouvé / non trouvé, chemin) est consigné.
3. Étant donné des transcripts de deux versions de Claude Code différentes, quand on compare leurs clés `usage` et l'arborescence `subagents/`, alors les différences sont listées.
4. Le livrable tient en une page et se termine par une recommandation go / no-go.

**Hors périmètre** : tout code livré dans DevDesk.
**Questions ouvertes** : aucune.

### S-02 — Parse a session's usage per agent
Epic : E1 — Priorité : Must — Effort : M — Plane : DEVDESK-40

En tant qu'utilisateur de DevDesk, je veux que les tokens d'une session soient agrégés par agent et sous-agent, afin de savoir qui a consommé quoi.

**Critères d'acceptation**
1. Étant donné une session avec un orchestrateur et 3 sous-agents, quand elle est analysée, alors chaque agent a ses totaux in, out, cache-read et cache-write.
2. Étant donné un même `message.id` répété sur plusieurs lignes (streaming), quand il est agrégé, alors il n'est compté qu'une fois.
3. Étant donné un transcript contenant des champs inconnus, quand il est analysé, alors les champs connus sont lus et aucune erreur n'est levée.
4. Étant donné une ligne illisible ou un fichier tronqué, quand il est analysé, alors le reste est lu et l'écart est signalé.
5. Les totaux de 3 sessions de référence diffèrent de ccusage de moins de 2 %.

**Hors périmètre** : coût en dollars, quota, stockage.
**Hypothèses** : Hypothèse: `subagents/agent-*.jsonl` et `meta.json` restent lisibles de la même façon (à valider en S-01).

## Epic E2 — The usage view

### S-03 — Browse sessions and see the agent tree
Epic : E2 — Priorité : Must — Effort : M (à livrer en deux parts) — Plane : S-03a = DEVDESK-41, S-03b = DEVDESK-42

> Redécoupage faisabilité : **S-03a** = critères 1 et 3 (liste des sessions, effort S) ; **S-03b** = critères 2 et 4 (table des agents d'une session, effort S).

En tant qu'utilisateur de DevDesk, je veux ouvrir une vue qui liste mes sessions Claude Code et affiche l'arbre des agents de la session choisie, afin de voir l'orchestration d'un coup d'œil.

**Critères d'acceptation**
1. Étant donné des sessions dans `~/.claude/projects`, quand j'ouvre la vue, alors les sessions sont listées de la plus récente à la plus ancienne avec projet et date.
2. Étant donné une session choisie, quand je la sélectionne, alors l'arbre orchestrateur → sous-agents s'affiche avec rôle, modèle et tokens par agent.
3. Étant donné aucune session sur la machine, quand j'ouvre la vue, alors le tableau reste affiché avec ses en-têtes et zéro ligne (Rule 139), et le pied de page indique l'absence de session.
4. La vue respecte les règles TUI du projet : raccourcis grisés (Rule 130), aide `?` (Rule 114), redimensionnement (Rule 108).

**Hors périmètre** : tri, direct, coût.

### S-04 — Spot the most expensive agent
Epic : E2 — Priorité : Must — Effort : S — Plane : DEVDESK-43

En tant qu'utilisateur, je veux trier les agents par tokens et voir leur part du total, afin d'identifier celui qui consomme le plus.

**Critères d'acceptation**
1. Étant donné l'arbre d'une session, quand je trie par tokens, alors l'agent le plus gros est en tête.
2. Chaque agent affiche sa part du total de la session en pourcentage, et la somme fait 100 % (± arrondi).
3. Étant donné deux agents du même rôle (même `agentType`), quand ils sont affichés, alors ils restent distincts.
4. Le tri utilise la touche `.` (Rule 111).

### S-05 — Follow an active session live
Epic : E2 — Priorité : Must — Effort : L (à livrer en deux parts) — Plane : S-05a = DEVDESK-44, S-05b = DEVDESK-45

> Redécoupage faisabilité : **S-05a** = lecteur incrémental sans TUI (critères 3 et 4, effort M) ; **S-05b** = polling dans la vue (critères 1, 2 et 5, effort M).

En tant qu'utilisateur, je veux que la vue se mette à jour pendant qu'une session tourne, afin de suivre la consommation sans relancer de commande.

**Critères d'acceptation**
1. Étant donné une session active, quand un agent produit un nouveau message, alors ses tokens à l'écran sont à jour en moins de 3 secondes.
2. Étant donné un nouveau sous-agent lancé, quand il apparaît, alors il s'ajoute à l'arbre sans quitter la vue.
3. Étant donné une session terminée, quand je la regarde, alors plus aucune lecture périodique n'est faite sur ses fichiers.
4. La lecture est incrémentale : un transcript de plusieurs Mo n'est pas relu en entier à chaque mise à jour.
5. `ctrl+r` force une relecture complète.

**Hypothèses** : Hypothèse: le polling (tick 2 s + stat) ne dégrade pas la réactivité de la TUI (à valider en faisabilité).

### S-06 — See each agent's activity and state
Epic : E2 — Priorité : Should — Effort : M — Plane : DEVDESK-46

En tant qu'utilisateur, je veux voir l'état, le nombre d'appels d'outils et la durée de chaque agent, afin de repérer un agent bloqué ou emballé.

**Critères d'acceptation**
1. Étant donné un agent, quand il est affiché, alors on voit son état (en cours / terminé), son nombre d'appels d'outils et sa durée.
2. Étant donné un agent sans nouvelle activité depuis plus de 5 minutes alors qu'il est « en cours », quand la vue se met à jour, alors il est signalé comme inactif.
3. Seuls des compteurs sont affichés, jamais le contenu des prompts ni des sorties d'outils.

### S-07 — Indicative cost per agent
Epic : E2 — Priorité : Could — Effort : M — Plane : DEVDESK-47

En tant qu'utilisateur, je veux un coût estimé par agent, afin de situer l'ordre de grandeur.

**Critères d'acceptation**
1. Étant donné un agent et son modèle, quand le coût est affiché, alors il est marqué comme estimation (tokens × tarif du modèle).
2. Étant donné un modèle au tarif inconnu, quand il est affiché, alors la cellule indique `-` et non zéro.
3. La table de tarifs est modifiable sans recompiler (configuration).

**Hors périmètre** : facture réelle, quota.

## Epic E3 — Reach sandbox sessions

### S-08 — See `sbx` sandbox sessions
Epic : E3 — Priorité : Should — Effort : S (option A) ou L (option C/B) — dépend de S-01 — Plane : DEVDESK-48

En tant qu'utilisateur travaillant en sandbox, je veux voir aussi les sessions lancées dans `sbx`, afin que la vue couvre mes longues orchestrations.

**Critères d'acceptation** : à écrire après S-01 (la solution de transport n'est pas choisie).
**Questions ouvertes** : montage de `~/.claude`, hook, ou export OpenTelemetry ?

## Won't (cette fois)
- Quota 5 h/hebdo (nécessite une statusline concurrente ; `/usage` le couvre).
- Historique persisté et comparaison par feature (pas de lien session ↔ story).
- Autres CLI d'agents, sessions cloud, OpenTelemetry.

## Backlog Plane (créé le 2026-10-24)

Projet DEVDESK, état Backlog, label `story`. Les relations « blocked by » n'ont pas pu être créées (l'API de relations répond 404 sur cette édition) : l'ordre de livraison est écrit dans chaque description.

| Story | Identifiant | Module (epic) | Priorité Plane | Effort |
|---|---|---|---|---|
| S-01 | DEVDESK-39 | E1 | high | S |
| S-02 | DEVDESK-40 | E1 | high | M |
| S-03a | DEVDESK-41 | E2 | high | S |
| S-03b | DEVDESK-42 | E2 | high | S |
| S-04 | DEVDESK-43 | E2 | high | S |
| S-05a | DEVDESK-44 | E2 | high | M |
| S-05b | DEVDESK-45 | E2 | high | M |
| S-06 | DEVDESK-46 | E2 | medium | M |
| S-07 | DEVDESK-47 | E2 | low | M |
| S-08 | DEVDESK-48 | E3 | medium | S à L |

Modules : E1 « Read agent usage from Claude Code transcripts », E2 « The usage view », E3 « Reach sandbox sessions ».
