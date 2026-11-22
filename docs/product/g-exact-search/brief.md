# Brief — g-exact-search

## Idée

Dans le prompt `g` de `ws` et de `ge` (forge explorer), pouvoir basculer entre la
recherche floue (comportement actuel) et une recherche exacte.

## Problème

Le prompt `g` (`internal/ui/fuzzy`, partagé par `ws` et `ge`) n'a qu'un scorer,
`fuzzy.Match` : une requête est acceptée si ses lettres apparaissent dans l'ordre dans le
chemin, même dispersées. `dev` remonte donc aussi tout chemin contenant d, e, v dans
l'ordre. Quand on connaît le nom cherché, ces faux positifs noient le bon résultat.

## Décisions (validées avec l'utilisateur)

- « Exact » = sous-chaîne contiguë, insensible à la casse.
- Bascule par une touche dans le prompt : `tab` (libre dans le finder, sans conflit avec
  la saisie ; `ctrl+e` écarté, c'est « fin de ligne » dans le champ).
- Exception déclarée à la Rule 135 (`.claude/rules/tui-forms.md`, surface `fuzzy/finder`) :
  la bascule utilise `tab` (décision validée par l'utilisateur après la faisabilité). `tab`
  doit figurer dans l'en-tête des raccourcis et dans l'aide de la vue, avec la description
  du mode vers lequel il bascule (« Exact match » / « Fuzzy match »).
- Mode affiché dans la barre `Find`.
- Un seul réglage commun à `ws` et `ge`, non mémorisé : chaque ouverture de `g` démarre en
  `fuzzy`.
- Classement en `exact` : un match en début de chemin ou juste après `/` passe devant, puis
  le chemin le plus court.
- Parcours allégé : pas de recherche ni de critique (besoin clair, impact limité à un
  composant partagé).

## MVP

- Touche `tab` : `fuzzy` ↔ `exact`, la liste se recalcule immédiatement.
- Libellé du mode actif dans la barre `Find`.
- Seuil minimum de caractères et compteur de correspondances inchangés.

## Non-objectifs

- Pas de mémorisation (ni session, ni YAML).
- Pas de sensibilité à la casse, de regex, ni de correspondance sur un segment entier.
- Pas de réglage par vue.
- Pas de changement du scorer flou existant.

## Métriques de succès

- Dans `ws` et `ge`, `tab` bascule le mode et recalcule la liste.
- En `exact`, `dev` ne retourne que des chemins contenant littéralement « dev » ; `wsd` ne
  retourne rien.
- En `fuzzy`, aucun changement observable.

## Points techniques repérés

- `internal/ui/fuzzy/match.go` (`Match`) et `finder.go` (`rematch`, `RenderBar`,
  `Update`) concentrent le changement.
- Docs à mettre à jour : `docs/architecture/workspaces.md`, `forge.md`.
