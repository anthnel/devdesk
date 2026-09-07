---
project_name: 'devdesk'
user_name: 'Anthoni'
date: '2026-09-06T00:00:00+01:00'
sections_completed: ['technology_stack', 'critical_rules', 'ai_collaboration']
---

# Project Context for AI Agents

_This file contains critical rules and patterns that AI agents must follow when implementing code in this project. Focus on unobvious details that agents might otherwise miss._

---

## AI Collaboration Rules

> [!IMPORTANT]
> Planning, brainstorming, implementation and bug fixes are all in scope for
> Claude Code — planning is no longer delegated to Gemini/Antigravity (see
> `.claude/CLAUDE.md`, `## AI Collaboration Rule`).
> - **Plan Location**: All implementation plans MUST be saved in the `.claude/plans/` directory.
> - **Any task that will produce a commit starts in its own worktree**, cut
>   from `main`, under `.worktrees/` inside the repository — see
>   `.claude/CLAUDE.md`, `## Work starts in a new worktree`.

---

## Technology Stack & Versions

- **Language**: Go 1.25.5
- **TUI Framework**: Bubble Tea (github.com/charmbracelet/bubbletea v1.3.10)
- **UI Components**: Bubbles (github.com/charmbracelet/bubbles v0.21.0)
- **Styling**: Lipgloss (github.com/charmbracelet/lipgloss v1.1.0)
- **API Clients**: GitLab (gitlab.com/gitlab-org/api/client-go v1.10.0) **and**
  GitHub (github.com/google/go-github/v68) — `internal/forge` abstracts both;
  a context targets exactly one, never two.
- **Security Tools**: Trivy, Gitleaks
- **Containerization**: Docker only. Podman was never added — there is no
  Podman code anywhere in the tree to remove.

---

## Critical Implementation Rules

1.  **Architecture**: Multi-view TUI using a central Router (`internal/app/app.go`).
2.  **State Management**: Follow the Elm Architecture (TEA). **NEVER** modify model state inside a `Cmd`.
3.  **Styling**: Use the centralized `internal/ui/theme` package for all styles and colors.
4.  **Keybindings**: Follow the vocabulary in `internal/ui/keymap`, detailed
    in `.claude/rules/tui-layout.md` (Rule 111). Only three `Ctrl`
    combinations exist anywhere in the app — `ctrl+c` (SIGINT), `ctrl+r`
    (refresh, nothing else), `ctrl+p` (command palette) — and there is no
    `ctrl+d`/`ctrl+s`/`ctrl+n`/`ctrl+a` binding. Resource actions are a
    single **uppercase** letter instead: `S` scan, `A` scan all, `D` delete,
    `N` create, `E` edit, and the rest of the shared table in Rule 111.
    Lowercase letters are always local filters/toggles, never actions, and
    vim-style `hjkl` navigation aliases do not exist (removed entirely).
    - `.`: Cycle sort column.
5.  **Documentation**: Keep `GetHelpContent()` synchronized with changes.
