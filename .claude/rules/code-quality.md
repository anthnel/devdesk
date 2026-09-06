## Code quality rules and proactive checks

### Rule 301 : Systematic code quality check before commit

**MANDATORY**: Before creating a commit, ALWAYS perform the following checks:

#### 1. Go linter (golangci-lint)
```bash
golangci-lint run
```

**Required actions:**
- ✅ Fix ALL warnings and errors
- ✅ Specifically check deprecation warnings (SA1019)
- ✅ NEVER ignore warnings without documented justification

#### 2. Tests
```bash
go test ./...
```

**Required actions:**
- ✅ Verify that all tests pass
- ✅ If tests fail, fix them BEFORE the commit

#### 3. Build
```bash
go build
```

**Required actions:**
- ✅ Verify that the build succeeds without errors
- ✅ Test the application if functional changes were made

#### 4. Deprecated dependencies

**Detection:**
- Check golangci-lint SA1019 warnings
- Look in go.mod for packages marked as deprecated

**Action:**
- Replace IMMEDIATELY with a maintained alternative
- Document the replacement in the commit message
- Update the documentation (.claude/CLAUDE.md)

**Examples of common replacements:**
- `github.com/go-ping/ping` → `github.com/prometheus-community/pro-bing`

### Rule 302 : IDE diagnostics check

**MANDATORY**: Before each commit, check the IDE diagnostics:

```
Use mcp__ide__getDiagnostics to check:
- Compilation errors
- Unused parameters
- Unused imports
- Other warnings
```

**Required actions:**
- ✅ Fix or justify every diagnostic
- ✅ Do not ignore "unused parameter" without a reason

### Rule 303 : go.mod and dependencies

**After any dependency change:**

```bash
go mod tidy
go mod verify
```

**Required actions:**
- ✅ Verify that go.mod is up to date
- ✅ Verify that go.sum is consistent
- ✅ Document new dependencies in .claude/CLAUDE.md

### Rule 304 : Quality commit message

**Mandatory format for quality fixes — in English (see Rule 307):**

```
type: short description

Detailed description explaining:
- What was detected (warning, error, etc.)
- Why it was a problem
- How it was resolved

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>
```

**Commit types for quality:**
- `fix:` - Bug fix detected by linter
- `chore:` - Deprecated dependency replacement
- `refactor:` - Code quality improvement

### Rule 305 : MANDATORY pre-commit checklist

**Before EVERY commit, check:**

- [ ] `golangci-lint run` → 0 warnings
- [ ] `go test ./...` → PASS
- [ ] `go build` → SUCCESS
- [ ] `mcp__ide__getDiagnostics` → No critical error
- [ ] `go.mod` up to date (if dependencies changed)
- [ ] Documentation updated (if API changed)
- [ ] Tests added (if new feature)

**If EVEN ONE of these checks fails:**
- ❌ DO NOT create the commit
- ✅ Fix it first
- ✅ Restart the checklist

### Rule 306 : MANDATORY proactivity

**Claude Code MUST:**
- ✅ Check the linter WITHOUT the user asking
- ✅ Propose fixes BEFORE the commit
- ✅ Flag deprecated dependencies AS SOON AS they are detected
- ✅ Document all quality changes

**Claude Code MUST NEVER:**
- ❌ Wait for the user to flag a warning
- ❌ Create a commit with unresolved warnings
- ❌ Ignore dependency issues
- ❌ Forget to update the documentation

### Correct workflow examples

**Example 1 - Feature commit:**
```
1. Write the code
2. go build → verify compilation
3. golangci-lint run → fix warnings
4. go test ./... → verify tests
5. mcp__ide__getDiagnostics → check the IDE
6. git commit with a detailed message
```

**Example 2 - Detecting a deprecated dependency:**
```
1. golangci-lint detects SA1019
2. Look up the recommended alternative
3. Replace the import and the code
4. go mod tidy
5. Test that everything works
6. Update .claude/CLAUDE.md
7. git commit explaining the replacement
```

### Rule 307 : Commits and code comments — English only

**Every git commit message and every comment in the code must be written
in English**, regardless of the language used to converse with the user.

| Element | Language |
|---------|--------|
| Commit message (title + body) | ✅ English only |
| Code comment (`//`, `/* */`, docstring) | ✅ English only |
| Replies to the user in the conversation | The user's language (see memory `feedback-langue-francais`) |
| UI and logs (Rule 129) | English US only — already covered, unchanged |

This **replaces** the earlier tolerance listed in `.claude/CLAUDE.md` under
"Code Conventions" ("Comments: French or English both accepted"): existing
French comments are not to be rewritten wholesale, but any new comment and
any edit to an existing comment must be in English.

```
// ✅ CORRECT
// retry with backoff because the API rate-limits bursts above 10 req/s

// ❌ WRONG (a French comment, which Rule 307 forbids)
// on relance avec un backoff car l'API limite les rafales au-delà de 10 req/s
```

```
✅ fix: correct table width calculation when a column is dropped

Bordures étaient soustraites deux fois, ce qui tronquait la dernière colonne.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>

❌ fix: corrige le calcul de largeur de colonne
```

Forbidden:
- ❌ A commit message written in French, even partially
- ❌ A new code comment in French
- ❌ Confusing this rule with the language of the conversation, which
  remains the user's
