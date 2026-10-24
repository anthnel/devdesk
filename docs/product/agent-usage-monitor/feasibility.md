# Agent usage monitor : faisabilité (architect, 2026-10-24)

**Verdict : faisable, sans nouvelle dépendance Go**, à trois conditions : (1) dédupliquer par `message.id` en gardant la **dernière** ligne, (2) lire les fichiers par polling incrémental dans un `Cmd`, l'agrégat n'étant modifié que dans `Update`, (3) S-08 ne démarre qu'après une vérification sur l'hôte qui lance `sbx`.
Légende : [O] observé sur cette machine (structure seulement, aucun contenu lu ni rapporté), [E] estimation.

## Observations locales qui changent le plan

- [O] 7 projets, 130 JSONL (178 Mo), 91 sous-agents. Plus gros fichier : 15 Mo. **Plus longue ligne : 1,27 Mo** : le `bufio.Scanner` par défaut (64 Kio) échoue. Il faut un `bufio.Reader` ou un `Scanner.Buffer` plafonné, et compter les lignes ignorées.
- [O] 19 types de lignes (`assistant`, `user`, `attachment`, `api-request-blob`, `cost-state`, `agent-name`…). Versions 2.1.283 à 2.1.302 seulement : les transcripts plus anciens sont purgés à 30 jours, donc la fixture « autre version » devra être **synthétique**.
- [O] **Dédup mesurée** sur tous les transcripts : la somme naïve gonfle les totaux ×1,9 à ×2,5. Garder la *première* ligne sous-estime `output_tokens` de 2,7 %, ce qui dépasse la tolérance de 2 %. La dernière ligne porte toujours le maximum (2968 groupes sur 2968). Il faut aussi exclure le modèle `<synthetic>`.
- [O] Le transcript parent ne contient aucune ligne `isSidechain` ni `agentId` : aucun double comptage entre fichiers. Le `toolUseResult` de l'outil Agent n'a pas de `usage`, mais il contient `prompt` : à ne jamais décoder.
- [O] `subagents/agent-<id>.meta.json` contient `finished: bool` (87 sur 91 ; absent pour un agent en cours ou une ancienne version) et `spawnDepth` (toujours 1 ici, l'arbre a donc 2 niveaux en pratique). Il contient aussi `description`, un texte de tâche, donc du contenu.
- [O] `~/.claude/sessions/<pid>.json` (non documenté) : `sessionId`, `pid`, `status` (`busy` ou `idle`), `updatedAt`. C'est un bon indice pour savoir qu'une session est vivante. Repli : la date de modification du fichier.
- [O] Lignes `cost-state` : `modelUsage` par modèle (`inputTokens`… `costUSD`), écrites par Claude Code lui-même. C'est une **référence possible pour la règle des 2 %**, peut-être meilleure que ccusage.
- [O] Champ `quotaLimits` sur une ligne `assistant` (`status`, `rateLimitType: five_hour`, `resetsAt`). Il n'apparaît qu'au rejet. Cela nuance research.md : signal partiel de quota, à garder pour la v2.
- [O] Python lit les 178 Mo en 0,65 s [E : Go est du même ordre]. Une relecture complète est donc acceptable pour `ctrl+r`, mais jamais dans `Update`.
- [O] Ni `sbx` ni `docker` ne sont installés sur cette machine. Le seul hook configuré est `SessionStart`, il y a une statusline, aucun réglage OTel. Les sessions `sbx` ne sont **pas observables d'ici**.

## 1. Risques techniques (classés)

| # | Risque | P / I | Parade |
|---|---|---|---|
| 1 | Format non contractuel (nouveaux types, champs renommés) | haute / haute | Décoder ligne à ligne dans une struct qui ne porte **que** des champs numériques et des identifiants, ignorer les types inconnus, compteurs `skipped` et `unknownVersion` affichés dans l'en-tête (dégradation visible). Fixtures datées et versionnées. |
| 2 | Dédup | certaine / haute | Clé `message.id` (+ `requestId`), la dernière ligne gagne, carte par session (et non par fichier) : elle couvre un futur parent qui recopierait les lignes des sous-agents. Exclure `<synthetic>`. Fixer en S-01 la référence (ccusage ou `cost-state`) : si ccusage garde la première ligne, l'écart de 2,7 % est structurel. |
| 3 | Lecture incrémentale | moyenne / haute | Un offset par fichier ; une dernière ligne sans `\n` est gardée en reste et relue au tick suivant ; une taille inférieure à l'offset déclenche une relecture complète ; une ligne au-delà du plafond est sautée et comptée. On suppose les fichiers en ajout seul (les `compact_boundary` sont ajoutées, pas réécrites). |
| 4 | Polling ou watcher | moyenne / moyenne | **Polling `tea.Tick` (2 s) + `stat`**, le modèle existant (suivi du viewer, ports, conteneurs). Pas de `fsnotify` : c'est une nouvelle dépendance, peu fiable sur les montages 9p, virtiofs et WSL, précisément ceux de S-08. Ne sonder que la session ouverte et le répertoire `subagents/` (nouveaux agents). Arrêt quand la session est inactive et que tous les `finished` sont à vrai. |
| 5 | Règle 110 / `Update` non bloquant | faible si conçu ainsi / haute | Le `Cmd` reçoit une **copie** des offsets et rend `UsageDeltaMsg{gen, file, from, to, records}`. `Update` n'applique le delta que si `from == offset courant` et `gen` est le dernier, sinon il l'abandonne (même schéma que `toolsGen`). Une seule lecture en vol par session. `mise run test-race` est obligatoire. |
| 6 | Confidentialité | moyenne / haute | Les lignes contiennent prompts, thinking, `wireToolInputs`, `toolUseResult.prompt`. On ne loggue jamais une ligne, et une erreur de décodage ne cite que le fichier et l'offset. On ne compte les blocs `tool_use` que par leur `type`. `description` et `ai-title` ne s'affichent pas (voir Q3). |
| 7 | Intégration au routeur | faible / moyenne | Les tests de contrat attrapent les oublis (`TestEveryViewSuppliesItsHeaderAndHelp`, keymap, `AllViewNames`). Points d'entrée listés en §3. |
| 8 | Entrée non fiable (S-08) | moyenne / moyenne | Un sandbox écrit ces fichiers : plafond de ligne, pas de suivi des liens symboliques hors des racines, pas de décodage de chemin depuis le nom de répertoire (lire `cwd`). |
| 9 | Portabilité | faible / moyenne | `~/.claude` vient de `os.UserHomeDir`, et `CLAUDE_CONFIG_DIR` est honoré s'il est défini (Windows inclus). |

## 2. Effort par story

| Story | Effort | Hypothèse qui le justifie |
|---|---|---|
| S-01 spike | **S** (½ à 1 j) | Ce document en a fait une partie (structure, dédup, `meta.json`). Reste à faire : lancer ccusage sur 3 sessions et noter sa règle de dédup, vérifier `sbx` **sur l'hôte qui le lance**, créer la fixture synthétique « autre version ». |
| S-02 parseur | **M** | Package pur sans TUI ; struct à champs numériques ; dédup ; tolérance ; 5 à 8 fixtures. Le critère des 2 % dépend de la référence choisie en S-01. |
| S-03 liste + arbre | **M**, découpage conseillé | C'est la première vue : enregistrement complet (§3). **S-03a** liste des sessions (projet = `cwd`, date = mtime, triées) : S. **S-03b** `→` vers la table des agents (orchestrateur puis sous-agents, rôle, modèle, tokens) sur le modèle runs → targets de `:jobs` : S. |
| S-04 tri + part | **S** | `.` et `Less` sont fournis par `datatable`. Colonne `%` en `SizingFixed` ; le dernier arrondi est absorbé par la ligne principale pour que la somme fasse 100. Distinguer deux agents du même rôle se fait par l'`agentId` court. |
| S-05 direct | **L**, à redécouper | **S-05a** lecteur incrémental dans le package (offsets, reste de ligne, troncature, plafond), sans TUI : M. **S-05b** polling dans la vue (tick, `gen`, découverte des nouveaux agents, arrêt sur session terminée, `ctrl+r` = relecture complète) : M. |
| S-06 état / activité | **M** | État = `finished`, sinon « en cours » si la session est vivante, sinon « inconnu ». Inactif = aucune ligne depuis plus de 5 min. Compter les `tool_use` par `(message.id, apiBlockIndex)`, car les lignes streamées répètent le message. Durée = premier → dernier `timestamp`. |
| S-07 coût indicatif | **M** | Nouvelle section de configuration (`agent_usage.prices` par modèle, avec les tarifs distincts cache 5 min / 1 h / lecture), `-` si le tarif est inconnu. C'est une carte, pas un scalaire : la vue `:cfg` ne l'édite pas, l'édition passe par le YAML (Q4). |
| S-08 sandbox | **S** (option A) à **L** (option B) | Avec l'option A, DevDesk n'ajoute que des racines de transcripts multiples et une colonne « Source » ; le reste est une procédure `sbx` documentée. À réécrire après S-01. |

## 3. Packages et intégration

- `internal/agentusage` (pur, sans Bubble Tea) : `Discover(roots) []SessionRef`, `type Reader` (offsets et reste, valeur copiable), `ReadDelta(ref, offsets) (Delta, error)`, `type Aggregate` avec `Apply(Delta) Aggregate` (dédup, totaux par agent), `Liveness(ref)` (`sessions/*.json` + mtime). `internal/agentusage/pricing` arrivera avec S-07.
- `internal/ui/agentusage` : `Model` (deux niveaux `datatable`, fil d'Ariane sous la table, Règle 123), `availability.go` (Règle 130), `GetHelpContent` (Règle 114), les 4 méthodes de `HeaderView` (compteur de sessions et d'agents dans `GetHeaderInfo`), et l'état de chargement dans le pied de page (Règles 128 et 139).
- Points d'entrée : `internal/command/parser.go` (`ViewAgentUsage`, table `viewNames`), `internal/app/command_line.go` (`createView`), `docs/architecture/` (nouveau fichier, et une ligne dans le tableau de `.claude/CLAUDE.md`).
- **Commande** : `:usage` / `:us` (Hypothèse : `ag`, `us` et `u` sont libres, à arbitrer en Q1).
- **Clavier : aucune lettre d'action en MVP.** `→`/`enter` descend, `←`/`esc` remonte, `.` trie, `/` filtre, `ctrl+r` relit. Les lettres libres (`Q`, `Z`) ne sont pas consommées. Un éventuel filtre « actives seulement » serait une minuscule déclarée dans `keymap.localToggles`. Plus tard : `Y` (copier l'id de session), déjà dans le vocabulaire.
- Pas de `jobs.Run` : le polling n'est pas un travail lancé par l'utilisateur. On ne l'inscrit au registre que si l'on veut voir la relecture complète dans `:jobs`, ce qui n'est pas nécessaire.

## 4. Sandbox `sbx` : transport

D'après des sources tierces, les transcripts d'un `sbx` vivent dans un **volume géré par le kit** et ne sont pas sur l'hôte ; `sbx create` accepte des montages supplémentaires ([sbx/claude-mixin](https://hub.docker.com/r/sbx/claude-mixin), [DEV Community](https://dev.to/thr0n/claude-code-in-docker-sandboxes-239g)). Non vérifié : `sbx` est absent de cette machine.

| Option | Avantages | Risques |
|---|---|---|
| **A. Monter un répertoire hôte dédié** (`~/.devdesk/agent-usage/sbx/<nom>`) comme `~/.claude/projects` du sandbox ; DevDesk le déclare comme racine supplémentaire | Aucun code de transport ; même parseur ; quasi temps réel ; survit à la suppression du sandbox | Il faut recréer le sandbox (`sbx create`) ; le sandbox écrit sur l'hôte. **Ne jamais monter tout `~/.claude`** : le sandbox pourrait écrire `settings.json`/hooks exécutés par l'hôte (évasion). Montages 9p/virtiofs : d'où le polling. |
| B. Lire le volume via `sbx exec … tail -c +offset` | Rien à reconfigurer côté sandbox | CLI expérimentale, un processus par tick, latence, dépend de l'auth `sbx` ; effort L |
| C. Hook dans le sandbox (`Stop`, `SubagentStop`) qui copie le transcript dans le dépôt monté (`.worktrees/…`) | Utilise le montage existant | Les hooks ne portent pas de tokens (il faut copier le fichier), écrit dans l'arbre du dépôt (gitignore), installe une config dans chaque sandbox, latence par événement |
| D. OTel : le sandbox exporte en OTLP vers un récepteur DevDesk | Attribution officielle (`agent_id` sur les événements) | Hors MVP (brief), récepteur à embarquer, sortie réseau du sandbox vers l'hôte via le proxy `sbx`, 5 s/60 s, variables d'environnement par session |

**Recommandation : A**, avec un montage **dédié et limité aux transcripts**, sous réserve que S-01 confirme sur l'hôte `sbx` que (a) le montage peut remplacer `~/.claude/projects` sans casser `claude -c`, et (b) les fichiers sont visibles et ajoutés au fil de l'eau côté hôte. Sinon, se replier sur C.

## 5. Ordre de livraison

1. S-01 (Go / no-go, dédup de référence, constat `sbx`).
2. S-02 puis S-05a (même package, en TDD, sans TUI ; parallélisable une fois l'API `Delta`/`Aggregate` figée).
3. S-03a, puis S-03b et S-04 (S-04 parallélisable avec S-03b).
4. S-05b (critère de succès « en direct < 3 s »).
5. S-06, puis S-07 (indépendante, peut glisser), puis S-08 selon S-01.

## Questions ouvertes (pour l'utilisateur)

1. **Nom de la commande** : `:usage`/`:us` (recommandé), `:agents`/`:ag`, ou `:agent-usage`/`:au` ?
2. **Référence des 2 %** : ccusage (externe, règle de dédup à vérifier) ou les lignes `cost-state` de Claude Code (interne, par modèle et non par agent) ? Recommandation : `cost-state` pour le total de session, ccusage en contrôle secondaire.
3. **Afficher `meta.description` / `ai-title`** pour distinguer les agents et nommer les sessions ? Ce sont des textes de tâche, contraires à « jamais de contenu ». Recommandation : non en MVP (agent = rôle + `agentId` court, session = projet + date).
4. **Tarifs (S-07)** : édition YAML seule acceptable, ou faut-il un éditeur dans `:cfg` (ce qui ajoute M) ?
5. **Où tourne `sbx`** : sur l'hôte Windows évoqué dans `.claude/CLAUDE.md` ? DevDesk lit-il alors des chemins Windows (`%USERPROFILE%\.claude`) ? Cela conditionne S-01 et S-08.
6. **Recréer les sandboxes** avec un montage supplémentaire (option A) : acceptable ?
