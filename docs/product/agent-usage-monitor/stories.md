# Agent usage monitor — stories (post-feasibility)

Decisions: command `:agent-usage` / `:au`; the 2% reference is ccusage at the session level (`ccusage claude session --id <uuid>`; the `cost-state` lines are unusable as a reference: +19 to 22 %, per model, written at close only, see spike-s01.md); no task titles (meta.description, ai-title) in the MVP; keep the last line per message.id when deduplicating; read lines with a capped reader (lines up to 1.3 MB); polling tick 2 s.
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
5. Étant donné 3 sessions de référence terminées avec sous-agents, quand le total de chaque session (somme des agents : in + out + cache-read + cache-write) est comparé à `ccusage claude session --id <uuid>`, alors l'écart est inférieur à 2 % (S-01 : 0,00 % sur les 3 sessions testées). Les lignes `cost-state` ne servent pas de référence.
6. Étant donné un message de sous-agent dont la dernière ligne pour son `message.id` a `stop_reason` null, quand il est agrégé, alors son `output_tokens` est compté tel quel et l'agent est marqué « sortie partielle » : la sortie d'un sous-agent est une borne basse (S-01 : 94 % des dernières lignes sur 2.1.302, médiane de 8 tokens).
7. Étant donné une ligne dont des champs `usage` nullables (`output_tokens_details.thinking_tokens`, `service_tier`, `speed`, `inference_geo`, `iterations`, `fallback_credit`) sont absents ou null, quand elle est analysée, alors ils sont lus comme optionnels et les totaux sont inchangés.
8. Étant donné une fixture synthétique « autre version » (champ `usage` renommé ou absent, type de ligne inconnu, `meta.json` sans `finished`), quand elle est analysée, alors aucune erreur n'est levée, les champs connus sont lus et l'état de l'agent est « inconnu ».
9. Étant donné une session dont le dossier contient un sous-dossier `tool-results/` à côté de `subagents/`, quand les agents sont découverts, alors `tool-results/` est ignoré et seuls les `subagents/agent-*.jsonl` sont lus.

**Hors périmètre** : coût en dollars, quota, stockage.
**Hypothèses** : validée par S-01 : `subagents/agent-*.jsonl` et `meta.json` sont restés lisibles de la même façon entre 2.1.283 et 2.1.302 (changements additifs uniquement). Le corpus ne couvre qu'une seule version mineure : la fixture « autre version » du critère 8 est donc synthétique.

## Epic E2 — The usage view

### S-03 — Browse sessions and see the agent tree
Epic : E2 — Priorité : Must — Effort : M (à livrer en deux parts) — Plane : S-03a = DEVDESK-41, S-03b = DEVDESK-42

> Redécoupage faisabilité : **S-03a** = critères 1 et 3 (liste des sessions, effort S) ; **S-03b** = critères 2 et 4 (table des agents d'une session, effort S).

En tant qu'utilisateur de DevDesk, je veux ouvrir une vue qui liste mes sessions Claude Code et affiche l'arbre des agents de la session choisie, afin de voir l'orchestration d'un coup d'œil.

**Critères d'acceptation**
1. Étant donné des sessions dans `~/.claude/projects`, quand j'ouvre la vue, alors les sessions sont listées de la plus récente à la plus ancienne avec projet et date.
2. Étant donné une session choisie, quand je la sélectionne, alors l'arbre orchestrateur → sous-agents s'affiche avec rôle, modèle et tokens par agent. Étant donné un sous-agent marqué « sortie partielle » (S-02, critère 6), alors sa valeur Out est préfixée `≥` (ex. `≥ 8`) ; celle de l'orchestrateur ne l'est pas.
3. Étant donné aucune session sur la machine, quand j'ouvre la vue, alors le tableau reste affiché avec ses en-têtes et zéro ligne (Rule 139), et le pied de page indique l'absence de session.
4. La vue respecte les règles TUI du projet : raccourcis grisés (Rule 130), aide `?` (Rule 114), redimensionnement (Rule 108).

**Hors périmètre** : tri, direct, coût.

### S-04 — Spot the most expensive agent
Epic : E2 — Priorité : Must — Effort : S — Plane : DEVDESK-43

En tant qu'utilisateur, je veux trier les agents par tokens et voir leur part du total, afin d'identifier celui qui consomme le plus.

**Critères d'acceptation**
1. Étant donné l'arbre d'une session, quand je trie par tokens, alors l'agent le plus gros est en tête. Le tri porte sur les tokens totaux (in + out + cache) : la sortie partielle des sous-agents (S-02, critère 6, ≤ 0,3 % du total) ne change pas le classement.
2. Chaque agent affiche sa part du total de la session en pourcentage, et la somme fait 100 % (± arrondi). La part est calculée sur les tokens totaux ; la valeur Out d'un sous-agent à sortie partielle reste préfixée `≥` (S-03b).
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
4. Étant donné un sous-agent dont le transcript contient 5 blocs `tool_use` distincts, quand il est affiché, alors son nombre d'appels d'outils est 5, compté depuis le transcript et non depuis la notification de fin (`queued_command.usage.toolUses`, non exploitable : S-01).

### S-07 — Indicative cost per agent
Epic : E2 — Priorité : Could — Effort : M — Plane : DEVDESK-47

En tant qu'utilisateur, je veux un coût estimé par agent, afin de situer l'ordre de grandeur.

**Critères d'acceptation**
1. Étant donné un agent et son modèle, quand le coût est affiché, alors il est marqué comme estimation (tokens × tarif du modèle).
2. Étant donné un modèle au tarif inconnu, quand il est affiché, alors la cellule indique `-` et non zéro.
3. La table de tarifs est modifiable sans recompiler (configuration).
4. Le coût par agent est annoncé comme sous-estimé pour les sous-agents (leur sortie est une borne basse, S-02 critère 6, et une partie des appels API n'est pas dans les transcripts : `cache_read` 24 à 32 % plus haut dans `cost-state`). La cellule garde le marqueur d'estimation du critère 1.
5. Étant donné une session terminée dont `cost-state` est disponible, quand le coût est affiché, alors le total facturé de la session est montré avec une ligne « Unattributed » égale au total facturé moins la somme des coûts par agent (ex. total 10,00 $, agents 8,20 $ → Unattributed 1,80 $).
6. Étant donné une session en cours (pas de `cost-state`), quand le coût est affiché, alors seuls les coûts estimés par agent apparaissent, sans total facturé ni ligne « Unattributed ».

**Hors périmètre** : attribution exacte de la facture par agent, quota.

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

Spike S-01 (DEVDESK-39) : voir spike-s01.md
