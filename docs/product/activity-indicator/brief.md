# Indicateur d'activité uniforme (pastille en première colonne) — brief

Statut : brouillon du 2026-11-06, idée validée avec l'utilisateur, mis à jour le 2026-11-06 avec ses décisions sur la revue de faisabilité ; les stories sont dans `stories.md` (tableau « Décisions tranchées » en tête), la recherche dans `research.md`, la conception UX dans `ux.md`. Aucun item Plane ni issue GitHub n'est créé à ce stade.
Absorbe : DEVDESK-99 (S-11, « Show agent state and type as icons, and flag costly agents »), à clore à la création du backlog (A-13 reprend son contenu) ; elle n'est pas modifiée dans Plane maintenant.

## Vision (une phrase)

Dans toute vue qui lance une opération longue, la première colonne de chaque ligne dit, d'un coup d'œil et sans lire le footer, si quelque chose s'exécute dessus (pastille qui clignote), si la dernière tentative a échoué (pastille rouge fixe) ou si tout est au repos (l'icône habituelle de la ligne).

## Problème

Aujourd'hui, chaque vue montre l'activité à sa façon :

- des spinners propres à chaque ligne (containers, netdiag, ports, arbre de l'explorer, OCI busy, `:jobs`) qui coûtent chacun leur chaîne de ticks ou leur copie de frame ;
- des états d'échec recopiés à la main, vue par vue (`security.scanTarget.Failed`, `failedScans` dans OCI), donc un échec visible dans une vue est invisible dans l'autre pour la même cible ;
- `ws` n'a aucun état d'échec par ligne ;
- la colonne qui montre l'état change de place et de forme d'une vue à l'autre (`Scanned` pendant un scan, glyphe dans `:jobs`, icône de type ailleurs).

Conséquence : l'utilisateur ne peut pas apprendre un seul vocabulaire visuel, un échec se perd dès qu'on change de vue, et chaque nouvelle vue réinvente son spinner.

Hypothèse: le problème est ressenti surtout avec les opérations longues lancées en lot (scan, sync, clone), où l'on quitte la vue et où l'on revient. À confirmer en usage (pas de mesure disponible).

## Idée

Une **pastille** dans la première colonne (même colonne que l'icône de ligne, `datatable.IconColumnWidth`, Rule 125) :

| État de la ligne | Pastille |
|---|---|
| Opération en cours | `●` / `○` qui clignote (1 Hz), lavande |
| En file | `◴` fixe |
| Dernière tentative en erreur | `⊗` rouge fixe, jusqu'à la prochaine tentative sur cette ligne |
| Au repos | l'icône habituelle de la ligne (image, dossier, dépôt, conteneur `running`) ou cellule vide |

La pastille remplace temporairement l'icône. Elle accepte **deux sources** : un état mémorisé (le registre des jobs et le nouveau mémorisateur d'échecs) ou un état calculé par la vue (état observé docker, état d'un agent). Le vocabulaire complet des glyphes (suspendu, terminé, annulé, sans action, inconnu) est dans `stories.md` et `ux.md` §1.

Un **mémorisateur central** « dernier échec par cible » vit dans le routeur (dans `jobs.Registry`) : clé (type d'opération, cible, contexte), un échec par clé, effacé à la prochaine transition de cette clé ou à la suppression de la cible. Ni `ctrl+r` ni la sortie de la vue ne l'effacent ; il vit le temps de la session, sans disque. Il remplace les copies manuelles. **Chaque vue n'affiche que les échecs des opérations qu'elle peut relancer.**

Un **tick unique, lent (1 Hz)**, fourni par le routeur, anime le clignotement et s'arrête quand plus rien ne clignote. Les spinners par ligne sont remplacés par la pastille ; les spinners de chargement global restent dans le footer (Rule 128) avec leurs propres chaînes.

## Décisions prises (par l'utilisateur, ne pas rouvrir)

1. Pastille dans la première colonne, remplaçant temporairement l'icône de ligne.
2. Opération en cours = clignotante ; erreur = rouge fixe jusqu'à la prochaine tentative sur la ligne ; repos = icône habituelle ou cellule vide.
3. Un glyphe ET une couleur par état, jamais la couleur seule. La couleur dit l'état, pas le type d'opération. Pas de vert pour « terminé ». Le footer détaille ce qui s'exécute ou la cause de l'erreur pour la ligne sélectionnée.
4. Réglage `app.motion: blink | static`, **par contexte seulement** (aucune variable d'environnement globale), défaut `blink` (2026-11-06). En mode `static`, le glyphe « en cours » est `●` fixe.
5. Glyphes (Unicode standard, pas de Nerd Font) (2026-11-06) : `●`/`○` en cours (clignote, seul état animé, `ColorSecondary`) ; `◴` en file (fixe) ; `⊗` échec (rouge, fixe) ; `◐` suspendu (agent idle, conteneur paused, orange) ; `◕` conteneur restarting (orange, fixe) ; `◉` terminé (job done, agent done) uniquement ; `⊘` job cancelled ; `⊖` job skipped ou conteneur exited code 0 ; `◌` unknown ou created (couleur neutre, pas dim). Phase éteinte `○` dans la même couleur. `:agent-usage` garde ses 4 états (running, idle, done, unknown) ; `:jobs` garde ses 4 issues (done, failed, cancelled, skipped) ; containers : états observés par docker, calculés par la vue (exited non nul et dead en rouge, exited code 0 neutre). `●` et `◉` se distinguent dans le terminal de l'utilisateur : pas de repli `✓`.
6. Conteneur `running` : **aucune pastille** (état nominal, Rule 122) ; la ligne montre son icône habituelle. Le footer reste « Running since 3 hr ago », sans glyphe en préfixe (2026-11-06).
7. Couleurs d'état : **alias fermés** du thème (assignés dans `ApplyTheme`, comme `ColorFooter*`), pas de rôles `icon_*` surchargeables. Les rôles `icon_*` ne servent qu'à l'icône de repos (2026-11-06).
8. Mémorisateur central dans le routeur, avec la clé et la durée de vie ci-dessus. Remplace `security.scanTarget.Failed` et `failedScans` d'OCI. Chaque vue n'affiche que les échecs des opérations relançables depuis elle (2026-11-06).
9. **Une nouvelle colonne de pastille est acceptée** pour les 7 tables sans colonne d'icône (4 tables OCI, liste de clone, ports, agents de `:agent-usage`) (2026-11-06).
10. Un seul tick partagé, lent (1 Hz), fourni par le routeur, anime les lignes ; les spinners propres à une ligne (containers, netdiag, ports, arbre explorer, OCI busy) sont remplacés par la pastille ; les spinners de chargement global gardent leurs chaînes dans le footer. À écrire dans `app-shell.md`.
11. Rule 121 : amendement accepté (la colonne d'activité remplace `IconOK` / `IconError` / `IconWarning` pour les états d'activité) ; les Rules 121 et 125 sont amendées avec le composant (2026-11-06).
12. Le critère de coloration du coût d'`:agent-usage` (ex-DEVDESK-99) reste dans la story `:agent-usage` (A-13, M), sans dépendance de code à S-09 (2026-11-06).
13. Libellés du footer et de la légende `?` (`ux.md`) validés, en anglais US ; la phrase « or in :cfg » est à confirmer (voir Questions ouvertes) (2026-11-06).
14. Vues à migrer : `:jobs`, `ws`, `:sec`, OCI, explorer/clone, templates, netdiag, `:agent-usage`, containers. L'aide `?` de chaque vue décrit les glyphes.
15. Livraison en plusieurs PR, une validation précoce du clignotement sur une première vue pilote, puis un second point de contrôle à la livraison de `ws`.

## Périmètre (MVP)

- Composant de pastille (glyphes, alias de couleur fermés), légende d'aide partagée, réglage `app.motion`, tick partagé, mémorisateur central, footer par ligne.
- Migration des neuf vues listées, en 11 stories de migration (plus le garde-fou A-16) : `ws` en une seule, OCI en deux (images ; networks, volumes, registries), l'explorer en deux (arbre ; liste de clone).
- Une colonne de pastille ajoutée aux 7 tables qui n'en ont pas.
- Test garde-fou transverse, documentation (`app-shell.md`, `tui-forms.md` Rule 121, `tui-tables.md` Rule 125).

## Non-objectifs

- Pas de couleur par type d'opération (un scan et un sync en cours ont la même pastille).
- Pas de vert pour « terminé ».
- Pas de persistance disque des échecs.
- Pas de migration des actions instantanées (`docker stop`, `kill`, `remove`) vers le registre : leur échec reste au footer (leur « en cours » montre la pastille).
- Pas d'autre animation que le clignotement de la pastille.
- Pas de nouvelle colonne **autre que la colonne de pastille** des 7 tables citées (décision 9) ; en particulier pas de seconde colonne pour une icône de type d'agent.
- Pas de variable d'environnement globale pour couper le mouvement, ni de touche de bascule à chaud.
- Pas de rôles `icon_*` surchargeables pour les états.
- Hors lot, story ultérieure possible : l'échec d'une action visible sur la ligne.

## Critères de succès

1. La première colonne est soumise à la même règle dans toutes les vues migrées ; un test scanne les sources.
2. Chaque état se lit sans couleur ni mouvement : les glyphes sont tous différents (test) et le footer détaille l'état de la ligne sélectionnée.
3. Avec `app.motion: static`, plus aucun redessin périodique n'est émis pour les pastilles.
4. Un échec de **scan** posé depuis `ws` est visible pour le même dépôt dans `:sec`, un échec de scan d'une image posé depuis `:sec` l'est pour la même image dans OCI, et chacun s'efface à la prochaine tentative (chaque vue n'affiche que les échecs des opérations qu'elle peut relancer ; un dépôt et une image ne sont pas la même cible).
5. **Un seul tick anime les lignes** (test). Les spinners de chargement global du footer gardent leurs chaînes et sont hors du test.
6. La Rule 116 tient à 60 colonnes sur toutes les vues migrées, colonne de pastille ajoutée comprise.

## Solutions existantes (voir `research.md`)

- Fait: oxker (TUI Docker) colore les conteneurs par état, sans clignotement mentionné.
- Fait: k9s expose des couleurs de statut par skin ; rien de vérifié sur un clignotement par ligne.
- Aucune donnée: lazygit, btop, lazydocker, dry, `gh run watch`.
- Fait: GitHub (Primer) note que l'animation rassure mais peut distraire.
- Conclusion de la recherche : couleur = état est courant ; une pastille **animée par ligne** n'est attestée dans aucune source trouvée. Approche plausible mais non éprouvée ailleurs.

## Hypothèses à valider

| # | Hypothèse | Comment la valider |
|---|---|---|
| H1 | Un clignotement lent (1 Hz, un flash par seconde, sous le seuil de 3 par seconde) est perçu comme une aide et non comme un bruit | Pilote (A-06) en usage réel, avant de migrer les autres vues ; premier point de contrôle go/no-go dans la story |
| H2 | Le coût de redessin d'un tick à 1 Hz est négligeable, y compris sur SSH | Mesure avec `TEA_TRACE` (octets émis par seconde avec 1 et 20 lignes clignotantes) dans A-03, consignée au pilote |
| H3 | Un glyphe distinct par état suffit pour distinguer rouge et orange sous daltonisme ou `NO_COLOR` | Test d'unicité des glyphes ; test de contraste (calculs de `ux.md` §2.1 à confirmer) ; revue du ux-designer |
| H4 | La cible d'une même opération se reconnaît d'une vue à l'autre (chemin de dépôt, référence d'image) | Test de clé commune dans A-04 (pas de spike séparé, l'architect lit la clé dans le code) |
| H5 | Remplacer l'icône de ligne pendant l'opération ne gêne pas (perte temporaire du type de ligne) | Second point de contrôle à la livraison de `ws` (A-07) ; le pilote `:jobs` n'a pas d'icône à remplacer ; le type reste lisible par le nom |
| H6 | WCAG 2.2.2 (pause/arrêt au-delà de 5 s) s'applique par analogie ; `app.motion: static` la couvre | Estimation de la recherche, non vérifiée pour un terminal |

## Risques

| Risque | Atténuation |
|---|---|
| Aucun outil comparable ne fait clignoter une pastille par ligne | Valider tôt sur une première vue ; `app.motion: static` ; arrêt possible avant les autres migrations (plan B ouvert, voir `stories.md`) |
| Rouge et orange confondus (daltonisme, `NO_COLOR`) | Glyphes différents (`⊗` contre `◐` / `◕`) ; test d'unicité ; détail au footer |
| `●` et `◉` proches dans certaines polices | Le clignotement les sépare ; le footer écrit l'état ; l'utilisateur les distingue dans son terminal (pas de repli `✓`) ; `◉` ne sert que « terminé » |
| Une erreur permanente reste rouge jusqu'au prochain essai (pas d'expiration) | Décision prise ; le footer en donne la cause ; hors lot : agir depuis la ligne |
| Conteneur `exited` avec code 0 | Tranché : `⊖` neutre (pas rouge) ; `exited` non nul et `dead` : `⊗` rouge |
| Ajouter une colonne de pastille à 7 tables réduit la place (4 cellules rendues) et fait tomber des colonnes `Optional` plus tôt ; les seuils documentés (ex. Breakdown « visible dès 106 colonnes ») bougent | Décision 9 ; tests de largeur et seuils recalés dans les stories concernées (A-09a, A-10b, A-12, A-13), Rule 116 vérifiée à 60 colonnes par `RenderedWidth()` |
| La colonne `Scanned` (`:sec`, OCI) affiche un ancien « 3 days ago » pendant un scan si le spinner la quitte | Critère explicite dans A-07, A-08 et A-09a : texte statique `scanning` en `DimStyle` |
| `ws` n'a aucun état d'échec par ligne aujourd'hui | Inclus dans A-07 (fusionnée) ; dépend du mémorisateur (A-04, livré avant) |
| Plusieurs opérations simultanées sur une ligne | Priorité en cours > en file > échec mémorisé > état observé > repos, une fonction (`indicator.Resolve`), un test dans A-01 |
| Le routeur ne sait pas qu'une vue veut faire clignoter une ligne hors registre (action instantanée, agent running, chargement de groupe) | Interface optionnelle `WantsBlink()` interrogée après chaque `Update` (A-03) ; risque élevé, testé avec `-race` |
| Le footer par ligne masque la progression globale « 3/12 » ou le bilan de sync (Rule 128) | Critère explicite dans A-05 : le détail de ligne est de niveau status, il ne supprime ni la progression ni le bilan |
| Vues non inventoriées (dashboard, status, filebrowser, viewer en suivi) | Spike A-00 d'inventaire avant de figer le périmètre |
| Livraison en plusieurs PR : états hybrides (certaines vues migrées, d'autres non) pendant des semaines | Fondations d'abord ; test garde-fou avec liste d'exceptions qui ne fait que diminuer |
| Couleur invisible sous une ligne colorée en entier (erreur de containers, sévérité) | Le glyphe porte l'information (principe 3) ; sous la sélection normale la couleur est conservée et le clignotement continue |
| Rules 121 et 125 contredites par la colonne d'activité | Amendements acceptés (décision 11), écrits avec le composant (A-01) |

## Questions ouvertes

Voir la section « Questions ouvertes » de `stories.md` : elles y sont toutes, avec une recommandation du PM quand il y en a une. Restent ouvertes : vue pilote et plan B du go/no-go ; identité de la cible et types d'opération mémorisés ; vues hors inventaire ; échec de test réseau de netdiag ; découpage des PR ; moment de la clôture de DEVDESK-99 ; confirmation que `app.motion` est modifiable dans `:cfg` ; test de contraste.
