# Indicateur d'activité uniforme (pastille en première colonne) — recherche

Date : 2026-11-06. Légende : **[fait]** sourcé, **[estimation]** déduction signalée, **[aucune donnée]** non trouvé.
Limite : recherche web rapide (une passe). Plusieurs points sur les outils concurrents n'ont pas pu être vérifiés ; ils sont marqués.

## Synthèse

1. Aucune source trouvée ne montre un outil TUI avec une pastille **clignotante** par ligne. Les outils trouvés signalent l'état par la **couleur** (oxker) ou un **spinner** ; pour les autres (k9s, lazygit, btop, lazydocker, dry, `gh`), **[aucune donnée]** vérifiée.
2. Le clignotement automatique de plus de 5 s est une exigence d'accessibilité WCAG 2.2.2 (niveau A) : il faut un moyen de le mettre en pause/arrêter. WCAG vise le web, mais c'est la référence documentée la plus proche. L'application à un terminal est une **[estimation]**.
3. Le clignotement matériel (SGR 5) est volontairement ignoré ou désactivable dans beaucoup de terminaux ; on ne peut pas compter dessus.
4. La couleur seule ne doit pas porter l'information (WCAG 1.4.1) : la pastille rouge/orange doit être doublée (glyphe différent ou texte dans le footer).
5. Conclusion : approche **plausible mais non éprouvée ailleurs** ; le risque vient surtout du clignotement et de la couleur seule, pas de la pastille elle-même (voir §4).

## 1. Comment les autres montrent l'activité par ligne

| Outil | Ce qui est établi | Source |
|---|---|---|
| oxker (TUI Docker) | **[fait]** Couleur par état de conteneur : mort/sorti rouge, en pause jaune, redémarrage vert clair, running sain vert, running non sain orange (#FFB224). Pas de clignotement mentionné. | docs.rs oxker 0.13.0 config.toml |
| k9s | **[fait]** Les skins définissent des couleurs de statut (new/modify/add/error, pending/completed). **[aucune donnée]** sur un clignotement ou une pulsation par ligne. | k9scli.io/topics/skins |
| lazygit, btop, lazydocker, dry, `gh run watch` | **[aucune donnée trouvée]** (les recherches n'ont rien renvoyé d'exploitable ; à vérifier dans le code ou la doc de chacun). | — |
| Claude Code | **[fait]** Des issues demandent un compteur / des états par sous-agent (running, idle, completed) dans la status line, et un bug rapporte un sous-agent terminé affiché « running » indéfiniment. **[aucune donnée]** confirmant une pastille native par agent. Les outils tiers claude-hud et Fleet (tmux) montrent des pastilles de couleur par agent (vert prêt, bleu idle). | github.com/anthropics/claude-code/issues/27916 ; claudeissues.com/issue/32858 ; claudeissues.com/issue/40928 ; awesome.ecosyste.ms (nicknisi/fleet) |
| GitHub (web) | **[fait]** Primer : l'animation rassure sur l'activité mais peut distraire ; les indicateurs doivent rester discrets (pages drafts). **[aucune donnée]** officielle sur les icônes exactes des checks. Un projet tiers fait tourner les icônes en cours et coupe l'animation en reduced-motion. | primer drafts « ui-patterns/loading » ; github.com/Kilo-Org/cloud/pull/5882 |

Leçon **[estimation]** : le motif « couleur = état » est courant (oxker) ; le motif « pastille animée par ligne » n'apparaît dans aucune source trouvée. Le risque de rupture avec les usages est donc réel mais non mesuré.

## 2. Clignotement en terminal : accessibilité, coût, réseau

- **[fait]** WCAG 2.2.2 : tout contenu qui clignote, démarre automatiquement, dure plus de 5 s et accompagne d'autre contenu doit pouvoir être mis en pause, arrêté ou masqué. Source : https://www.w3.org/WAI/WCAG22/Understanding/pause-stop-hide
- **[fait]** WCAG 2.3.1 : pas plus de 3 flashs par seconde (seuil d'épilepsie photosensible). Les deux critères se cumulent. Source : https://ictbaseline.access-board.gov/document-baselines/09FlashingDocs/
- **[fait]** Le clignotement SGR 5 est jugé gênant/inaccessible par beaucoup de terminaux, qui ne l'implémentent pas ou offrent un réglage pour le couper (iTerm2, Konsole, Terminal.app cités). Windows Terminal : débat sur le rendu « DEC » (caractères atténués) contre « PC ». Sources : https://terminfo-dev.pages.dev/sgr/5-blink ; https://github.com/microsoft/terminal/issues/7388 ; https://github.com/microsoft/terminal/issues/16753
- **[fait]** Un clignotement émis par l'application (alternance de glyphe/couleur à chaque tick) ne dépend pas de SGR 5, donc ne peut pas être coupé par le réglage du terminal. Déduit de ce qui précède : **[estimation]**.
- **[fait]** Bubble Tea : le renderer est plafonné (`WithFPS`), le spinner avance via `tea.Tick` à son propre intervalle (ex. 1/10 s, 1/8 s), `TEA_TRACE` mesure le rendu. Sources : mintlify.com/charmbracelet/bubbletea/advanced/performance ; sources.debian.org (bubbles 0.21.0 spinner.go).
- **[aucune donnée]** mesurant le coût de redessin ou la bande passante d'un tick de pastille par ligne sur SSH/liaison lente. À mesurer sur DevDesk avec `TEA_TRACE`. Un spinner existe déjà dans `:jobs` et `:sec`, donc le coût marginal d'une pastille sur le même tick est probablement faible **[estimation]**.
- **[aucune donnée]** sur un équivalent de `prefers-reduced-motion` pour terminal. Le web a cette media query ; aucune convention terminal trouvée (NO_COLOR concerne la couleur, pas le mouvement). Un réglage de contexte DevDesk serait à définir (**[estimation]**).
- **[fait]** WCAG 1.4.1 : la couleur ne doit pas être le seul moyen de transmettre l'information ; ajouter forme ou texte. Source : https://w3c.github.io/wcag/understanding/use-of-color

## 3. Alternatives au clignotement

Toutes **[estimation]** de conception, à valider ; aucune source ne les compare.

| Option | Pour | Contre |
|---|---|---|
| Spinner braille (déjà utilisé) | Familier, mouvement continu net, déjà dans le code | Plus agité qu'un clignotement lent ; occupe un glyphe mobile |
| Pulsation de teinte (dégradé sur 3-4 niveaux) | Plus doux qu'un on/off, reste sous 3 flashs/s | Exige du truecolor ; rendu variable selon le terminal ; plus de rafraîchissements |
| Glyphe alterné lent (ex. ● / ○ à ~1 Hz) | Ne repose pas sur la couleur seule ; sous le seuil des 3 flashs/s | C'est encore un clignotement : WCAG 2.2.2 s'applique au-delà de 5 s |
| Glyphe statique par état (● en cours, ✖ échec, ◐ idle) | Aucun mouvement, aucun coût de redessin, accessible | L'activité en cours se voit moins ; on ne distingue pas « bloqué » de « en cours » sans le footer |

Un réglage permettant de passer en mode statique (ou de couper le mouvement) couvrirait 2.2.2 pour les options animées **[estimation]**.

## 4. Conclusion

- Pastille + couleur = état : courant (oxker) et cohérent avec la règle existante de DevDesk (couleur sémantique). **Faible risque**.
- Animation de la pastille : **non attestée** ailleurs dans ce que j'ai pu vérifier, et soumise à WCAG 2.2.2 si elle dépasse 5 s. **Risque moyen**, atténuable (réglage « sans mouvement », fréquence ≤ ~1 Hz, glyphe qui change en plus de la couleur).
- Couleur seule pour rouge/orange : **risque d'accessibilité** (1.4.1) ; prévoir un glyphe distinct par état et le détail dans le footer (déjà prévu).
- Remplacer l'icône de ligne (image/dossier/dépôt) : perte temporaire de l'information de type de ligne, à noter dans la décision ; la règle 125 (colonne d'icône sans titre, 2 cellules) reste compatible **[estimation]**.

## Points ouverts

- Vérifier directement le code/la doc de k9s, lazygit, btop, lazydocker, dry, `gh` (aucune donnée obtenue).
- Mesurer le coût de redessin sur SSH (`TEA_TRACE`).
- Choisir le comportement par défaut (animé ou statique) et son réglage.
- La pastille est-elle visible sous la ligne sélectionnée (règle 125 : la couleur disparaît sous le curseur) ?

## Sources (consultées le 2026-11-06)

- https://www.w3.org/WAI/WCAG22/Understanding/pause-stop-hide
- https://ictbaseline.access-board.gov/document-baselines/09FlashingDocs/
- https://w3c.github.io/wcag/understanding/use-of-color
- https://terminfo-dev.pages.dev/sgr/5-blink
- https://github.com/microsoft/terminal/issues/7388
- https://github.com/microsoft/terminal/issues/16753
- https://docs.rs/crate/oxker/0.13.0/source/src/config/config.toml
- https://k9scli.io/topics/skins/
- https://github.com/anthropics/claude-code/issues/27916
- https://claudeissues.com/issue/32858-completed-subagent-shows-as-running-in-ui-indefinitely-stale-agent-status-not-cl
- https://claudeissues.com/issue/40928-feature-request-show-running-agents-in-status-line-with-click-to-focus
- https://awesome.ecosyste.ms/projects/github.com%2Fnicknisi%2Ffleet
- https://primer-50742d62f9-26441320.drafts.github.io/ui-patterns/loading
- https://github.com/Kilo-Org/cloud/pull/5882
- https://www.mintlify.com/charmbracelet/bubbletea/advanced/performance
- https://sources.debian.org/src/golang-github-charmbracelet-bubbles/0.21.0-2/spinner/spinner.go
