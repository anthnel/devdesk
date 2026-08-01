---
project_name: 'devdesk'
user_name: 'Anthoni'
date: '2026-02-13T10:45:00+01:00'
sections_completed: ['technology_stack', 'critical_rules', 'ai_collaboration']
---

# Project Context for AI Agents

_This file contains critical rules and patterns that AI agents must follow when implementing code in this project. Focus on unobvious details that agents might otherwise miss._

---

## AI Collaboration Rules

> [!IMPORTANT]
> This project uses a hybrid AI collaboration model:
> - **Planning, Research, Brainstorming, Requirements, and Spec Engineering**: Use **Gemini** (or Antigravity).
> - **Implementation, Code Writing, and Fixes**: Use **Claude Code**.
> - **Plan Location**: All implementation plans MUST be saved in the `.claude/plans/` directory to be accessible by Claude Code.

---

## Technology Stack & Versions

- **Language**: Go 1.25.5
- **TUI Framework**: Bubble Tea (github.com/charmbracelet/bubbletea v1.3.10)
- **UI Components**: Bubbles (github.com/charmbracelet/bubbles v0.21.0)
- **Styling**: Lipgloss (github.com/charmbracelet/lipgloss v1.1.0)
- **API Clients**: GitLab API (gitlab.com/gitlab-org/api/client-go v1.10.0)
- **Security Tools**: Trivy, Gitleaks
- **Containerization**: Docker (Podman support is currently premature and should be removed)

---

## Critical Implementation Rules

1.  **Architecture**: Multi-view TUI using a central Router (`internal/app/app.go`).
2.  **State Management**: Follow the Elm Architecture (TEA). **NEVER** modify model state inside a `Cmd`.
3.  **Styling**: Use the centralized `internal/ui/theme` package for all styles and colors.
4.  **Keybindings**: Follow standardized keybindings defined in `.claude/rules/tui.md`.
    - `ctrl+d`: Delete
    - `ctrl+s`: Scan
    - `.`: Cycle sorting
    - `hjkl`: Navigation
5.  **Documentation**: Keep `GetHelpContent()` synchronized with changes.
