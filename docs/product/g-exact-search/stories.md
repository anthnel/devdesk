# Stories — g-exact-search

## Epic : Recherche du prompt `g`

### S-01 — Basculer entre recherche floue et exacte dans le prompt g
Epic : Recherche du prompt `g` — Priorité : Must — Effort : S — Plane : DEVDESK-678
(Hypothèse: un seul composant partagé `internal/ui/fuzzy`, ~60 lignes de prod, docs ~la
moitié de l'effort.)

En tant qu'utilisateur de `ws` ou `ge`, je veux basculer le prompt `g` entre recherche
floue et exacte afin de ne voir que les chemins contenant littéralement ce que je tape
quand je connais le nom cherché.

**Critères d'acceptation**
1. Étant donné le prompt `g` ouvert dans `ws` ou dans `ge`, quand il s'affiche, alors le
   mode est `fuzzy` et la barre `Find` l'indique.
2. Étant donné le prompt ouvert en `fuzzy` avec la requête `dev`, quand j'appuie sur
   `tab`, alors le mode passe à `exact`, la barre l'indique et la liste se recalcule
   immédiatement sans retaper la requête ; un nouvel appui sur `tab` revient à `fuzzy`.
3. Étant donné le mode `exact` et les chemins `workspaces/devdesk` et `docs/data-env`,
   quand la requête est `dev`, alors seul `workspaces/devdesk` est listé ; avec `wsd`,
   alors aucun chemin n'est listé.
4. Étant donné le mode `exact`, quand la requête est `DEV`, alors les chemins contenant
   `dev` sont trouvés (insensible à la casse).
5. Étant donné le mode `exact` et plusieurs chemins qui matchent, quand la liste s'affiche,
   alors un match en début de chemin ou juste après `/` passe devant les autres, puis le
   chemin le plus court.
6. Étant donné le mode `exact` actif, quand je ferme le prompt (`esc` ou `enter`) puis le
   rouvre, dans la même vue ou dans l'autre, alors il redémarre en `fuzzy`.
7. Étant donné le mode `fuzzy`, quand je tape une requête, alors les résultats et leur
   ordre sont identiques à ceux d'avant cette story ; en `exact`, le seuil minimum de
   caractères et le compteur de correspondances restent ceux d'aujourd'hui.
8. Étant donné le prompt ouvert en mode finder, quand je regarde l'en-tête des raccourcis
   et l'aide de la vue, alors `tab` y figure avec la description du mode vers lequel il
   bascule (« Exact match » / « Fuzzy match »).

**Hors périmètre** : mémorisation du mode, sensibilité à la casse, regex, correspondance
sur un segment entier, réglage par vue, changement du scorer flou.
**Hypothèses** : Hypothèse: `tab` n'est capturé ni par le prompt (champ de saisie) ni par
le routeur pendant que le finder est actif. `tab` est une exception déclarée à la Rule 135
(`.claude/rules/tui-forms.md`, surface `fuzzy/finder`), décidée par l'utilisateur après la
faisabilité (voir brief.md, Décisions).
**Questions ouvertes** : aucune.
