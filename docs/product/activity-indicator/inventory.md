# Inventaire des vues à opération longue (A-00, DEVDESK-136)

**Validé par l'utilisateur le 2026-11-06.**

Spike du 2026-11-06, sur `main` à `7cd8ce7`, vérifié dans le code (fichier:ligne). Légende des spinners : **[L]** par ligne, remplacé par la pastille ; **[G]** chargement global, reste au footer (Rule 128). « D5 » = frame du routeur (`internal/app/jobs.go:127-243`, via `jobs.ChangedMsg`) ; « locale » = chaîne `spinner.TickMsg` propre à la vue.

## 1. Tableau par vue

| Vue / table | Spinners | Échec mémorisé à la main | 1re colonne | Source d'état | Classement |
|---|---|---|---|---|---|
| `:jobs` runs + cibles | [L] D5 dans le glyphe (`jobsview/state.go:19-27`) ; [G] footer D5 (`update.go:45`) | non (registre) | glyphe d'état, `IconColumnWidth` (`model.go:144`, `:195`) | registre | **migrer** (A-06) : colonne déjà en place. |
| `ws` | [L] D5 dans Scanned (`workspaces/view.go:319`, `:375`) et Git Status (`columns.go:266-282`) ; [G] footer D5 (`jobs.go:53`) | non ; le bilan de sync est dérivé du registre (`jobs.go:205-242`) | icône (`columns.go:140`) | registre (scan, sync, delete) | **migrer** (A-07) : opérations relançables, aucun échec par ligne aujourd'hui. |
| `:sec` inventaire | [L] D5 dans Scanned (`inventory_table.go:236`, frame posée `inventory.go:316-324`) ; [G] locale (`update.go:158-181`) | **oui** : `scanTarget.Failed` | icône (`inventory_table.go:313`) | registre (scan) + copie | **migrer** (A-08). |
| `:sec` onglet remédiation | aucun [L] : texte statique `scanning...` (`remediation.go:300`) ; [G] footer (`remediation.go:558-561`) | non : échec au footer seulement (`:397-400`) | texte | carte locale `scanning` (`:75`), hors registre | **hors lot** : onglet de résultats (hors périmètre de A-08), aucune frame. |
| `:sec` findings | aucun | non | Severity | sans objet | **sans objet** : aucune opération par ligne. |
| OCI images | [L] D5 dans Scanned (`oci_resources/table.go:138`) et ID pendant un pull (`:156`) ; [L] `busy` (`model.go:448`, `busy.go:32`) sur la chaîne locale (`update.go:76-88`) ; [G] chargement | **oui** : `failedScans` | **ID**, sans icône (`table.go:153`) | registre (scan, pull) + `busy` + copie | **migrer** (A-09a), nouvelle colonne (D1). |
| OCI networks, volumes | [L] `busy` (`model.go:455`, `:467`, `busy.go:33-34`) | non | ID / Name, texte | `busy` | **migrer** (A-09b), nouvelle colonne. |
| OCI registries | [L] `busy` (`model.go:483`, `busy.go:35`) ; **[L] `refreshing` des membres d'un groupe** (`table.go:405-408`, carte `refreshingGroups` `model.go:107`) | non : échec au footer (`browser_bridge.go:153-161`) | Alias, texte | `busy` + carte locale | **migrer** (A-09b), avec `refreshing` (écart 1, validé). |
| OCI navigateur de registre (tags) | **[L] frame propre dans C/H/M/L** (`registry_browser.go:155-162`, `browser_view.go:23`, avancée `browser_keys.go:36-38`) ; [G] recherches | non | Registry, texte | registre (scan, `jobs.go:35`) + index de frame local | **migrer** (A-09a), 8e colonne de pastille, décision utilisateur (écart 2). |
| OCI inspection de réseau | aucun | non | Container | sans objet | **sans objet**. |
| Explorer arbre | [L] D5 dans la cellule d'icône, create/delete seulement (`forge/explorer/row.go:59-61`, `:102-107`, `jobs.go:75`) ; [G] locale (`update.go:225-233`, `view.go:139`, `:144`) | non | icône (`columns.go:76`) | registre (create, delete) | **migrer** (A-10a), réduite à create et delete (écart 3, validé). |
| Explorer liste de clone | [L] D5 dans Status (`clone_list.go:132`, `:167-176`) ; [G] footer D5 (`clone.go:192`) | non (items du run) | **Repository**, sans icône (`clone_list.go:105`) | registre (clone, run ouverte) | **migrer** (A-10b), nouvelle colonne. |
| Templates | aucun [L] ; [G] footer D5 (`templates/scan.go:217`, `:252`) | non (footer, `scan.go:230`, `sync.go:118`) | icône (`model.go:146`) | registre (scan, sync) | **migrer** (A-11). |
| Containers | [L] `busy` (`containers/model.go:474`, `update.go:48`) sur la chaîne locale (`update.go:43-61`) ; [G] chargement | non (footer) | glyphe d'état docker (`model.go:179`) | calculé + `busy` | **migrer** (A-14). |
| Netdiag ports | [L] `busy` (`netdiag/ports_model.go:260`) + `portsSpinnerCmd` (`:310-317`) | non (footer) | **Proto**, sans icône (`:185`) | `busy` | **migrer** (A-12), nouvelle colonne. |
| Netdiag checks / interfaces | [G] (`netdiag/update.go:139-143`, `header.go:279` ; `interfaces_model.go:294-302`) | non | Check / Interface | sans objet | **sans objet** : le Verdict est un résultat. |
| Netdiag forwards | aucun | `LastErr` porté par `forward.Forward` (`forward_model.go:151-162`), pas une copie | Local | état observé (live, paused, unbound) | **sans objet** : actions instantanées, pas d'opération longue. |
| `:agent-usage` agents | [G] (`agentusageview/update.go:84-90`, `view.go:274`, `:290`) | non | **Agent**, sans icône (`agents.go:172`) | calculé (State) | **migrer** (A-13), nouvelle colonne. |
| `:agent-usage` sessions | [G] idem | non | Project (`columns.go:27`) | calculé | **hors lot** : liste non rafraîchie en direct, aucun état par ligne. |
| Filebrowser (`:files`, sélecteur de chemin) | aucun [L] ; [G] footer D5 (`filebrowser/jobs.go:37`) | non (footer) | icône (`columns.go:24`) | registre (delete, `jobs.go:15-31`) | **hors lot** : la suppression passe par le registre, mais aucune story ne la couvre (accepté). |
| Dashboard | [G] footer D5 (`dashboard/model.go:240-242`, `view.go:288-293`) | non | pas de table | registre (compteur) | **sans objet** : ne lance rien. |
| Status (moniteurs, certificats) | [G] (`status/update.go:45`, `view.go:117`) | non | Name | résultat de check (`columns.go:60-71`) | **sans objet** : UP/DOWN est un résultat (Rule 121 inchangée hors activité, D9). |
| Viewer (y compris suivi `F`, arbre JSON) | [G] lecture (`viewer/update.go:97-104`) ; le suivi est un `Status` sans spinner (`view.go:78`) | non | document ou arbre | sans objet | **sans objet** : un document, pas des lignes à opération. |
| Setup, auth forge | [G] (`setup/model.go:224` ; `forge/auth/update.go:38`, corps `view.go:141`) | non | formulaires, pas de table | sans objet | **sans objet**. |
| Configuration, about, palette, recherche floue | aucun | non | sans objet | sans objet | **sans objet**. |

## 2. Copies manuelles d'échec (recherche exhaustive : champs `Failed`/`err`, cartes `map[string]bool|error` sous `internal/ui`)

| Copie | Emplacement | Remplacée par |
|---|---|---|
| `scanTarget.Failed` | champ `security/inventory_table.go:65` ; posé `inventory.go:283`, effacé `:225`, lu `inventory_table.go:221`, `:237` | **A-08** |
| `failedScans` | `oci_resources/model.go:78` (init `:491`) ; posé et effacé `images.go:164-166` ; projeté dans `imageRow.Failed` (`table.go:37`, `:237`), lu `:124`, `:139` | **A-09a** |

Il n'y en a pas d'autre. Ce qui ressemble à une copie n'en est pas une, et **aucune story n'y touche** (c'est voulu) : `imageupdate` Failed (`internal/imageupdate/imageupdate.go:52`, colonne Update d'OCI et de containers) et le verdict de signature (`sigcol.go:42`) sont des **résultats** ; `forward.LastErr` est un état du domaine ; `remediationState.failed` (`remediation.go:73`) est l'échec de chargement de l'onglet. Les erreurs de groupe OCI et des scans de remédiation ne vont qu'au footer.

## 3. Écarts avec le tableau « Faisabilité » de `stories.md`

1. **OCI registries a un 2e spinner par ligne** (`refreshing`, `table.go:405-408`, état local). A-09b ne cite que `busy`. Sans lui, A-16 (b) échoue sur OCI. **Validé** : ajouté à A-09b (signal `WantsBlink`).
2. **Navigateur de registre : spinner par ligne absent du tableau** (index propre `tagScanSpinnerIdx`). A-09a met la « navigation du registre » hors périmètre. Laissé tel quel, il contredit D2 (« un seul tick anime les lignes »). **Tranché : option (a)**, une 8e colonne de pastille (hors des 7 tables de D1), rattachée à A-09a ; l'option (b), un texte statique `…`, est écartée.
3. **Explorer : aucun spinner par nœud pour le chargement des enfants.** `TreeNode.Loading` (`tree.go:32`, posé `navigation.go:36`) n'est jamais rendu sur une ligne. Le `→` entre aussitôt dans le groupe, qui devient le niveau courant et n'est donc plus une ligne visible. Le chargement est global (`view.go:139`). **Validé** : A-10a se limite à create et delete, sans `WantsBlink`, effort S.
4. **Liste de clone : clones en parallèle** (`forge.pull.parallel_jobs`, `clone.go:110`). Le critère 2 de A-10b est corrigé (plusieurs lignes peuvent clignoter ; les autres sont `◴`), avec la frame D5 déjà présente dans Status.
5. **`:jobs` : un run en file tourne aujourd'hui** (`state.go:22-27`). A-06 le passe à `◴` fixe : conforme à D7, **changement de comportement à annoncer** (reste une note, sans décision).
6. **Tables oubliées** : netdiag forwards, navigateur de registre, inspection de réseau, remédiation et findings de `:sec`, sessions d'`:agent-usage`, recherche floue (toutes classées ci-dessus). L'ancien glyphe U+25D4 (D7) n'existe pas dans le code (`IconPending` = `U+F0150`) : rien à remplacer.

## Liste finale des vues à migrer (validée)

`:jobs` (A-06) · `ws` (A-07) · `:sec` inventaire (A-08) · OCI images, plus navigateur de registre avec sa 8e colonne de pastille (A-09a) · OCI networks, volumes, registries avec `refreshing` (A-09b) · explorer arbre, create et delete seulement, effort S (A-10a) · liste de clone (A-10b) · templates (A-11) · netdiag ports (A-12) · `:agent-usage` agents (A-13) · containers (A-14).

## Hors lot (avec justification)

- **`:sec` onglet remédiation** (accepté) : son état est en texte statique et hors registre, et les onglets de résultats sont hors du périmètre de A-08.
- **Filebrowser** (accepté) : le delete passe par le registre, mais aucune activité ne s'affiche par ligne aujourd'hui. Candidat à une story S ultérieure.
- **`:agent-usage` sessions** (accepté) : aucun état par ligne, et la liste n'est pas en direct.
- Le navigateur de registre n'est plus hors lot (voir ci-dessus). Les vues « sans objet » du tableau sont acceptées.
