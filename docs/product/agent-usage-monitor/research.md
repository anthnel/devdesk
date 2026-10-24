# Recherche : moniteur d'usage des agents Claude Code dans DevDesk

Date de consultation de toutes les sources : 2026-10-24. Légende : [F] fait sourcé, [O] observé sur la machine locale, [E] estimation, [ND] pas de donnée trouvée.

## Question de recherche

Une vue DevDesk (TUI Go) qui suit en direct la consommation des sessions Claude Code locales (orchestrateur + sous-agents) : tokens in/out/cache, coût estimé, quota (fenêtre 5h, hebdo), activité (outils, durée, état), avec historique comparable entre sessions. Existe-t-il déjà un outil qui couvre le besoin, quelles données sont réellement disponibles, le problème est-il réel ?

## Synthèse

1. Aucun outil trouvé ne combine tout : vue live multi-sessions, attribution par sous-agent, quota officiel 5h/hebdo et historique comparable. Chacun couvre une partie (voir tableau). Le plus proche sur le papier est ccboard (Rust, TUI, arbre de sous-agents, hooks), mais son quota est une estimation locale, pas une lecture officielle. [F]
2. Le quota officiel (`rate_limits.five_hour` / `seven_day`, `used_percentage` + `resets_at`) n'est exposé que par le JSON de la statusline, uniquement pour les abonnés Pro/Max, après la première réponse API de la session. Ni les transcripts, ni les hooks, ni l'OTel ne le donnent. [F]
3. L'attribution par sous-agent existe sur trois sources : transcripts (`subagents/agent-<id>.jsonl`, avec `usage`), OTel (`query_source`, `agent.name` sur les métriques ; `agent_id` sur les événements) et hooks (`SubagentStop` donne `agent_transcript_path`). La statusline ne la donne pas : « Claude Code doesn't count subagent requests in these statistics » (pour l'objet cache) et l'issue #32842 montre que les compteurs de tokens de la statusline omettent les sous-agents. [F]
4. Le meilleur candidat de conception est un modèle hybride : transcripts (tokens, coût recalculé, historique, sous-agents) + hooks (état et activité live) + statusline (quota officiel, via un script qui écrit un fichier d'état). L'OTel exige une infrastructure (collecteur) mal adaptée à une TUI locale. [E, déduction des faits ci-dessous]
5. Le problème est réel (plusieurs issues anthropics/claude-code et ~8,8k étoiles pour un moniteur d'usage), mais plusieurs demandes ont été fermées « not planned » ; Anthropic ne comble donc pas le trou côté produit à ce jour. Les interfaces de données évoluent vite (33 événements de hooks, champs OTel conditionnés par version) : le risque de rupture de format est élevé. [F]

## 1. Outils existants

| Outil | Cible / proposition | Prix | Licence | Maintenance | Forces | Faiblesses / limites |
|---|---|---|---|---|---|---|
| ccusage (ryoppippi) | Rapports CLI jour/semaine/mois/session, blocs 5h, statusline (beta), multi-agents (Claude Code, Codex, etc.) | Gratuit | MIT | Très actif : release v20.0.43 le 2026-10-24 ; ~19,2k étoiles [F] | Référence, lit les JSONL locaux, prix via LiteLLM, JSON, mode offline | Le mode `blocks --live` a été supprimé en v18.0.0 (disponible seulement en v17.x) au profit de la statusline ; la doc des blocs ne mentionne pas les sous-agents ; pas de quota officiel dans la doc consultée ; pas de TUI live. [F] |
| Claude-Code-Usage-Monitor (Maciek-roboblog) | Moniteur terminal temps réel, barres, prévisions, alertes ; v4.0.0 | Gratuit | MIT | ~8,8k étoiles ; dernier push 2026-07-05 ; 39 issues ouvertes (API GitHub) [F] | Utilise `rate_limits` de la statusline quand frais, étiquette la provenance (`official`, `local_estimate`…), entrepôt local d'historique au-delà de 30 jours | Limites des plans Pro/Max/Team = estimations locales ; README incohérent sur la limite Pro (44 000 vs 19 000) ; Python 3.9+ ; pas de vue par sous-agent dans la doc consultée ; pas de donnée sur le dernier release. [F] |
| claude-powerline (Owloops) | Statusline Claude Code (segments git, coût, contexte, bloc 5h, hebdo, « pace ») | Gratuit | MIT | Actif : release v1.32.5 le 2026-10-24 ; ~1,2k étoiles [F] | Lit `rate_limits` natifs, affichage dans la barre native | Ce n'est pas une vue ; bloc/hebdo seulement pour Pro/Max ; texte tronqué possible ; Node 18+. Pas de vue sous-agents. [F] |
| ccboard | TUI Rust + web : sessions, coûts, arbre de sous-agents, config, hooks | Gratuit | Apache-2.0 ou MIT | Version 0.24.0 sur docs.rs ; autres métadonnées [ND] | Statut live via hooks (Running/WaitingInput/Stopped), cache SQLite, historique FTS5 | Quota = estimation locale par plan/budget mensuel ; la doc renvoie vers `:usage` pour les vraies limites. Installe des hooks dans `~/.claude/settings.json`. [F] |
| ccmon (PyPI) | Moniteur terminal des sous-agents (Python/Textual) | [ND] | [ND] | [ND] | Rafraîchit toutes les 0,5 s, lit `~/.claude/sessions` et les logs de sous-agents | Centré statut, pas coût/quota d'après la description. [F] |
| clorch, claude-viz, claude-vigie, atm, aiwatch | TUI divers (Go : clorch, claude-viz, claude-vigie ; Rust : atm, aiwatch) | [ND] | [ND] | [ND] | clorch : coût et burn rate depuis les transcripts ; atm : hooks ; claude-vigie traite les sous-agents comme faisant partie de la session parente | Vérification individuelle non faite (résultat de recherche seulement). [F pour la description, ND pour le reste] |
| Tableaux de bord OTel (ColeMurray/claude-code-otel, rockdarko/claude-code-metrics-prometheus, dashboards Grafana 25255, 25052, etc.) | Équipes : coût, tokens, activité via OTLP + Prometheus/Grafana | Gratuit (infra à héberger) | [ND] | [ND] | Métriques officielles, attribution `query_source`/`agent.name` | Exige collecteur + stockage ; les métriques sont exportées toutes les 60 s par défaut ; pas de quota. [F] |
| `/usage`, `/cost`, statusline natifs | Intégré à Claude Code | Inclus | n/a | Anthropic | Quota officiel | `/cost` = session courante seulement ; pas d'historique comparable ni de vue par sous-agent (issues #22625, #56824). [F] |
| Ne rien faire | | | | | | Risque décrit dans l'issue #76133 : limite atteinte en pleine orchestration sans visibilité du budget. [F] |

Lequel couvre déjà le besoin ? Aucun entièrement. Les outils combinant live + quota officiel + sous-agents (ccboard, Usage-Monitor) n'ont pas, d'après leur documentation, d'attribution coût/quota par sous-agent avec historique comparable. Je n'ai pas testé ces outils : écart à confirmer par un essai. [E]

## 2. Sources de données

### 2.1 Transcripts JSONL (`~/.claude/projects/<cwd-encodé>/<session>.jsonl`)

- Format non documenté officiellement : la structure vient de sources tierces (oh-my-opencode, référence « Claude Session Stores ») et de mon inspection locale. [F/O]
- [O] Les sous-agents sont dans `<session>/subagents/agent-<agentId>.jsonl` avec un `agent-<agentId>.meta.json` (champs vus : `agentType`, `description`, `toolUseId`, `spawnDepth`, `requestShape`). Chaque ligne porte `isSidechain:true`, `agentId`, et `sessionId` = session parente.
- [O] Les lignes `assistant` portent `message.usage` : `input_tokens`, `cache_creation_input_tokens`, `cache_read_input_tokens`, `cache_creation.{ephemeral_5m,ephemeral_1h}_input_tokens`, `output_tokens`, plus `message.id`, `requestId`, `stop_reason`.
- [O] Piège de déduplication : le même `message.id`/`requestId` apparaît sur plusieurs lignes (streaming : `stop_reason:null` puis `tool_use`, avec `output_tokens` absent sur la première). Il faut dédupliquer par `message.id`/`requestId` et garder la dernière. Un rapporteur de l'issue #55121 parle aussi de « 239 deduplicated API calls ».
- Le coût n'est pas dans le transcript : il faut recalculer à partir d'une table de prix (ccusage utilise LiteLLM). [F] Un commentaire Hashnode signale que sommer lectures et écritures de cache à valeur faciale gonfle les totaux par rapport au coût. [F, niveau faible]
- Quota : aucun champ. Attribution par sous-agent : oui, de bonne qualité (fichier par agent). Live : il suffit de surveiller les fichiers (inotify/fsnotify), latence proche du temps réel. Les transcripts sont purgés par défaut après 30 jours (`cleanupPeriodDays`, défaut 30, minimum 1) : un historique long demande une copie propre à DevDesk (le Usage-Monitor propose un « warehouse » pour cela). [F]
- Stabilité : élevée en pratique mais non contractuelle ; risque de rupture moyen à élevé (nouveaux répertoires `workflows/`, `journal.jsonl` qui n'est pas une session). [F + E]

### 2.2 Hooks (https://code.claude.com/docs/en/hooks)

- 33 événements dont `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `PostToolUseFailure`, `SubagentStart`, `SubagentStop`, `Stop`, `StopFailure`, `PreCompact`, `SessionEnd`, `PreModelSwitch`, `TeammateIdle`… [F]
- Champs communs : `session_id`, `prompt_id`, `transcript_path`, `cwd`, `permission_mode`, `effort`, `hook_event_name` ; `agent_id` et `agent_type` quand le hook se déclenche dans un sous-agent. [F]
- `SubagentStart` : `agent_id`, `agent_type`. `SubagentStop` : `agent_id`, `agent_type`, `agent_transcript_path`, `last_assistant_message`. Aucun champ `usage` ni tokens documenté sur ces événements. [F] Il faut donc lire le transcript pointé pour chiffrer.
- Fournit : états et activité (début/fin d'outil, sous-agent lancé/terminé) avec latence faible ; pas de quota ; pas de coût. Je n'ai pas lu les sections `Stop`/`StopFailure` (page tronquée) : pas de donnée trouvée sur un champ de limite dans ces entrées. [ND]
- Coût d'intégration : modifier `~/.claude/settings.json` de l'utilisateur (comme ccboard le fait avec `ccboard setup`). Stabilité : documentée, donc meilleure que les transcripts, mais l'ensemble évolue vite. [F + E]

### 2.3 OpenTelemetry (https://code.claude.com/docs/en/monitoring-usage)

- Métriques : `claude_code.session.count`, `cost.usage`, `token.usage` (type `input|output|cacheRead|cacheCreation`), `lines_of_code.count`, `commit.count`, `pull_request.count`, `code_edit_tool.decision`, `active_time.total`. [F]
- Attribution : `query_source` (`main|subagent|auxiliary`) et `agent.name` sur coût/tokens (les noms d'agents personnalisés deviennent `"custom"` sauf `OTEL_LOG_TOOL_DETAILS=1`). `agent_id` / `parent_agent_id` sur événements et spans seulement (v2.1.310+), pas sur les métriques. [F]
- Événements (export logs) : `api_request` (coût, tokens, durée), `tool_result` (`duration_ms`, `success`), `tool_decision`, `api_error`, `subagent_completed`, etc. [F]
- Cadence : métriques toutes les 60 s, logs toutes les 5 s par défaut. [F] Les traces sont en bêta (`CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1`). [F]
- Quota : aucune métrique ni événement de quota/limite dans la partie lue (100 000 premiers caractères sur 172 000). Le reste n'a pas été lu. [ND partiel]
- Coût d'intégration : DevDesk devrait embarquer un récepteur OTLP local, et l'utilisateur doit définir des variables d'environnement avant de lancer Claude Code (par session, non rétroactif). Stabilité : champs conditionnés par version, risque de rupture modéré mais bien documenté. [F + E]

### 2.4 Statusline JSON (https://code.claude.com/docs/en/statusline)

- Entrée : JSON sur stdin d'une commande configurée, déclenchée sur événements ; `refreshInterval` (minimum 1 s) pour relancer par minuteur ; relancée aussi quand une fenêtre atteint `resets_at`. [F]
- `cost.total_cost_usd` (estimation côté client au prix catalogue, « may differ from your actual bill », remis à 0 par `/clear`), `cost.total_duration_ms`, `cost.total_api_duration_ms`. [F]
- `context_window.used_percentage` (tokens d'entrée seulement) et `current_usage`. [F]
- Quota : `rate_limits.five_hour.{used_percentage,resets_at}`, `rate_limits.seven_day.{...}`, `rate_limits.spend_limit.*` derrière une passerelle. Présent seulement pour les abonnés claude.ai Pro/Max (ou passerelle), seulement après la première réponse API, chaque fenêtre peut être absente. [F]
- Attribution sous-agent : non. Les stats de cache de l'objet dédié ne comptent pas les requêtes de sous-agents ; la demande #32842 relève que les totaux de tokens de la statusline ne reflètent que le fil principal alors que `cost.total_cost_usd` semble inclure les agents (constat du demandeur, non vérifié par moi). [F]
- Limite structurelle : la statusline est exécutée par session ; pour un moniteur externe il faut un script qui écrit un fichier d'état (approche déjà prise par Usage-Monitor `--statusline`/`--write-state`). Une session inactive ne met pas le quota à jour. [F + E]

### Récapitulatif

| Besoin | Transcripts | Hooks | OTel | Statusline |
|---|---|---|---|---|
| Tokens in/out/cache | Oui (dédup. requise) | Non | Oui | Partiel (contexte) |
| Coût | À recalculer | Non | Oui (`cost.usage`) | Oui (session) |
| Quota 5h/hebdo officiel | Non | ND | Non (partie lue) | Oui (Pro/Max) |
| Par sous-agent | Oui | Identité + chemin du transcript | Oui (métriques par `agent.name`, événements par `agent_id`) | Non |
| Activité / état | Oui (tool_use, a posteriori) | Oui | Oui (`tool_result`) | Non |
| Latence live | Fichier : ~immédiate | Immédiate | 5 s (logs) / 60 s (métriques) | Sur événement ou `refreshInterval` |
| Historique | 30 jours par défaut | À stocker soi-même | Backend externe | À stocker soi-même |
| Risque de rupture | Moyen à élevé (non documenté) | Faible à moyen | Moyen (versions, bêta traces) | Faible à moyen |

## 3. Preuves que le problème existe

Pour :
- anthropics/claude-code#22625, « Per-Subagent Token Usage Tracking » (ouverte 2026-02-02, fermée 2026-03-16, « not planned ») : https://claudeissues.com/issue/22625-feature-per-subagent-token-usage-tracking
- #55121, le compteur de tokens omet la consommation des sous-agents, jusqu'à 10× de sous-estimation (ouverte 2026-04-30, fermée « not planned » 2026-06-16) : https://claudeissues.com/issue/55121-bug-token-counter-appears-to-omit-sub-agent-consumption-causing-up-to-10-underco
- #32842, les tokens de la statusline devraient inclure les sous-agents (mars 2026, fermée) : https://claudeissues.com/issue/32842-statusline-token-counts-should-include-subagent-tool-agent-token-usage
- #76133, abonné Max 20x limité en plein travail avec plusieurs sous-agents Opus en parallèle, demande un « budget remaining » et la conso par sous-agent (juillet 2026, encore ouverte à la date de la recherche) : https://claudeissues.com/issue/76133-max-20x-subscriber-rate-limited-within-usage-limits-during-claude-code-subagent
- #43149, exposer limites et minuteur de reset dans Claude Code (avril 2026, fermée) : https://claudeissues.com/issue/43149-feature-request-expose-api-usage-limits-and-rate-limit-reset-timer-to-claude-cod
- #56824, `/usage` sur plusieurs sessions (« the only option is /cost which only covers the current session ») ; #35947, #6958 (2025-09, dupliquée) : https://claudeissues.com/issue/56824-feature-request-usage-command-to-show-token-consumption-across-sessions , https://claudeissues.com/issue/35947-claude-code-cannot-report-its-own-token-quota-consumption , https://claudeissues.com/issue/6958-feature-request-built-in-usage-monitoring-command
- Traction : ccusage ~19,2k étoiles, Usage-Monitor ~8,8k, plus une dizaine de TUI récentes (ccboard, ccmon, clorch, atm, aiwatch…). Anthropic a ajouté `rate_limits` à la statusline, ce qui reconnaît le besoin du quota. [F]

Contre / nuances :
- Le quota 5h/hebdo est déjà visible via `/usage` et la statusline native ; plusieurs demandes sont fermées comme « résolues » (une partie du besoin a été livrée). [F]
- Beaucoup d'outils existent déjà et ccusage (très actif) couvre coût/tokens historiques ; le marché est encombré. [F]
- Les issues ci-dessus viennent d'un miroir (claudeissues.com), je n'ai pas relu les fils GitHub d'origine ni les commentaires des mainteneurs ; les statuts « fermée » reposent sur ce miroir. [limite]
- Aucune donnée trouvée sur le nombre d'utilisateurs qui lancent des orchestrateurs multi-agents comme ici, ni sur la demande spécifique à une TUI DevSecOps. [ND]
- Aucun test utilisateur ni mesure de fréquence du besoin pour l'utilisateur de DevDesk lui-même. [ND]

## Points incertains (à vérifier avant décision)

1. Essai pratique de ccboard et Claude-Code-Usage-Monitor pour confirmer l'absence d'attribution par sous-agent et d'historique comparable (non testés).
2. Fin des pages hooks (fichiers `Stop`/`StopFailure`) et OTel (72 000 derniers caractères) non lues : un champ de quota y est possible mais aucun trouvé.
3. Le statut des issues vient d'un miroir tiers.
4. Format du transcript non contractuel ; table de prix à maintenir (LiteLLM ou équivalent) ; écart entre coût recalculé et `cost.total_cost_usd`.
5. Licences et maintenance de ccmon, clorch, claude-viz, claude-vigie, atm, aiwatch : non vérifiées.

## Sources

- https://github.com/ryoppippi/ccusage (consulté le 2026-10-24) ; https://api.github.com/repos/ryoppippi/ccusage/releases/latest ; https://ccusage.com/guide/blocks-reports
- https://github.com/Maciek-roboblog/Claude-Code-Usage-Monitor ; https://api.github.com/repos/Maciek-roboblog/Claude-Code-Usage-Monitor
- https://github.com/Owloops/claude-powerline ; https://api.github.com/repos/Owloops/claude-powerline/releases/latest
- https://docs.rs/ccboard ; https://pypi.org/project/ccmon/ ; https://pkg.go.dev/github.com/lazypower/clorch ; https://pkg.go.dev/github.com/TypicalDefender/claude-viz ; https://pkg.go.dev/github.com/haribo/claude-vigie ; https://docs.rs/crate/atm/0.1.3 ; https://lib.rs/crates/aiwatch
- https://github.com/ColeMurray/claude-code-otel ; https://github.com/rockdarko/claude-code-metrics-prometheus ; https://grafana.com/grafana/dashboards/25255 ; https://grafana.com/grafana/dashboards/25052/
- https://code.claude.com/docs/en/statusline ; https://code.claude.com/docs/en/monitoring-usage ; https://code.claude.com/docs/en/hooks
- https://cdn.jsdelivr.net/npm/oh-my-opencode@4.18.2/dist/skills/coding-agent-sessions/references/claude.md (structure des sessions, tierce)
- Issues : voir la section 3 (URL complètes).
- Inspection locale (2026-10-24) de `~/.claude/projects/-home-anthoni-projects-workspace-entire-anthnel-devdesk/b660114e-.../subagents/agent-a635807a1994bf4c1.jsonl` et du fichier `.meta.json` associé.
