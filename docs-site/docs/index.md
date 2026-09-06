# DevDesk

A terminal-based DevSecOps workstation built with Go and [Bubble Tea](https://github.com/charmbracelet/bubbletea). DevDesk centralizes the tools a developer needs daily — GitLab or GitHub, Docker, security scanning, and system monitoring — in a single keyboard-driven TUI.

| | |
|---|---|
| **Dashboard** | Forge stats, Docker usage, OCI images, tool availability, service health |
| **Forge Explorer** | Browse and clone GitLab or GitHub, one backend per context |
| **Workspaces** | Local repositories with branch, status, last scan at a glance |
| **Security** | Trivy (CVE, secrets, licenses, misconfig) and Gitleaks scans |
| **Containers & OCI** | Manage running containers, browse and scan images |
| **Network Diagnostics** | DNS, ICMP, traceroute, TCP/HTTP/TLS checks, live port monitor |

## Where to start

This site follows the [Diátaxis](https://diataxis.fr/) documentation framework: each section answers a different kind of question.

| Section | Answers | Start here if… |
|---|---|---|
| [Tutorial](tutorial/first-session.md) | "Show me, step by step" | You've never run DevDesk before |
| [How-to guides](how-to/install.md) | "How do I do *this specific thing*?" | You know DevDesk and need to accomplish a task |
| [Reference](reference/commands.md) | "What's the exact name/value/key?" | You need to look something up |
| [Explanation](explanation/index.md) | "Why does it work this way?" | You want to understand the design, or you're contributing code |

## Requirements

- Go 1.25.5+ (to build from source)
- A terminal with [Nerd Font](https://www.nerdfonts.com/) support — icons are used throughout the UI
- Optional, feature degrades gracefully if absent: [Trivy](https://trivy.dev/), [Gitleaks](https://github.com/gitleaks/gitleaks), Docker

## Source

DevDesk is developed at [github.com/anthnel/devdesk](https://github.com/anthnel/devdesk). Issues and pull requests are welcome — see [Contributing](contributing.md).
