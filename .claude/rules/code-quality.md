## Règles de qualité de code et vérifications proactives

### Rule 301 : Vérification systématique de la qualité du code avant commit

**OBLIGATOIRE** : Avant de créer un commit, TOUJOURS effectuer les vérifications suivantes:

#### 1. Linter Go (golangci-lint)
```bash
golangci-lint run
```

**Actions requises:**
- ✅ Corriger TOUS les warnings et erreurs
- ✅ Vérifier spécifiquement les warnings de dépréciation (SA1019)
- ✅ Ne JAMAIS ignorer les warnings sans justification documentée

#### 2. Tests
```bash
go test ./...
```

**Actions requises:**
- ✅ Vérifier que tous les tests passent
- ✅ Si des tests échouent, les corriger AVANT le commit

#### 3. Build
```bash
go build
```

**Actions requises:**
- ✅ Vérifier que le build réussit sans erreurs
- ✅ Tester l'application si des changements fonctionnels ont été faits

#### 4. Dépendances deprecated

**Détection:**
- Vérifier les warnings golangci-lint SA1019
- Chercher dans go.mod les packages marqués comme deprecated

**Action:**
- Remplacer IMMÉDIATEMENT par une alternative maintenue
- Documenter le remplacement dans le message de commit
- Mettre à jour la documentation (.claude/CLAUDE.md)

**Exemples de remplacements courants:**
- `github.com/go-ping/ping` → `github.com/prometheus-community/pro-bing`

### Rule 302 : Vérification IDE diagnostics

**OBLIGATOIRE** : Avant chaque commit, vérifier les diagnostics de l'IDE:

```
Utiliser mcp__ide__getDiagnostics pour vérifier:
- Erreurs de compilation
- Paramètres non utilisés
- Imports non utilisés
- Autres warnings
```

**Actions requises:**
- ✅ Corriger ou justifier tous les diagnostics
- ✅ Ne pas ignorer les "unused parameter" sans raison

### Rule 303 : go.mod et dépendances

**Après tout changement de dépendance:**

```bash
go mod tidy
go mod verify
```

**Actions requises:**
- ✅ Vérifier que go.mod est à jour
- ✅ Vérifier que go.sum est cohérent
- ✅ Documenter les nouvelles dépendances dans .claude/CLAUDE.md

### Rule 304 : Message de commit qualité

**Format obligatoire pour les corrections de qualité — en anglais (voir Rule 307):**

```
type: short description

Detailed description explaining:
- What was detected (warning, error, etc.)
- Why it was a problem
- How it was resolved

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>
```

**Types de commit pour qualité:**
- `fix:` - Correction de bug détecté par linter
- `chore:` - Remplacement de dépendance deprecated
- `refactor:` - Amélioration de qualité du code

### Rule 305 : Checklist pré-commit OBLIGATOIRE

**Avant CHAQUE commit, vérifier:**

- [ ] `golangci-lint run` → 0 warnings
- [ ] `go test ./...` → PASS
- [ ] `go build` → SUCCESS
- [ ] `mcp__ide__getDiagnostics` → Aucune erreur critique
- [ ] `go.mod` à jour (si dépendances modifiées)
- [ ] Documentation mise à jour (si API changée)
- [ ] Tests ajoutés (si nouvelle fonctionnalité)

**Si UNE SEULE de ces vérifications échoue:**
- ❌ NE PAS créer le commit
- ✅ Corriger d'abord
- ✅ Recommencer la checklist

### Rule 306 : Proactivité OBLIGATOIRE

**Claude Code DOIT:**
- ✅ Vérifier le linter SANS que l'utilisateur le demande
- ✅ Proposer des corrections AVANT le commit
- ✅ Signaler les dépendances deprecated DÈS leur détection
- ✅ Documenter tous les changements de qualité

**Claude Code NE DOIT JAMAIS:**
- ❌ Attendre que l'utilisateur signale un warning
- ❌ Créer un commit avec des warnings non résolus
- ❌ Ignorer les problèmes de dépendances
- ❌ Oublier de mettre à jour la documentation

### Exemples de workflow correct

**Exemple 1 - Commit de feature:**
```
1. Écriture du code
2. go build → vérifier compilation
3. golangci-lint run → corriger warnings
4. go test ./... → vérifier tests
5. mcp__ide__getDiagnostics → vérifier IDE
6. git commit avec message détaillé
```

**Exemple 2 - Détection de deprecated:**
```
1. golangci-lint détecte SA1019
2. Rechercher l'alternative recommandée
3. Remplacer l'import et le code
4. go mod tidy
5. Tester que tout fonctionne
6. Mettre à jour .claude/CLAUDE.md
7. git commit avec explication du remplacement
```

### Rule 307 : Commits et commentaires de code — anglais uniquement

**Tout message de commit git et tout commentaire dans le code doivent être
rédigés en anglais**, quelle que soit la langue utilisée pour échanger avec
l'utilisateur.

| Élément | Langue |
|---------|--------|
| Message de commit (titre + corps) | ✅ English only |
| Commentaire de code (`//`, `/* */`, docstring) | ✅ English only |
| Réponses à l'utilisateur dans la conversation | Langue de l'utilisateur (voir mémoire `feedback-langue-francais`) |
| UI et logs (Rule 129) | English US uniquement — déjà couvert, inchangé |

Ceci **remplace** la tolérance précédente listée dans `.claude/CLAUDE.md` sous
« Code Conventions » (« Comments: French or English both accepted ») : les
commentaires en français existants ne sont pas à réécrire en masse, mais tout
nouveau commentaire et toute modification de commentaire existant doivent être
en anglais.

```
// ✅ CORRECT
// retry with backoff because the API rate-limits bursts above 10 req/s

// ❌ INTERDIT
// on relance avec un backoff car l'API limite les rafales au-delà de 10 req/s
```

```
✅ fix: correct table width calculation when a column is dropped

Bordures étaient soustraites deux fois, ce qui tronquait la dernière colonne.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>

❌ fix: corrige le calcul de largeur de colonne
```

Interdit :
- ❌ Un message de commit rédigé en français, même partiellement
- ❌ Un nouveau commentaire de code en français
- ❌ Confondre cette règle avec la langue de la conversation, qui reste celle
  de l'utilisateur
