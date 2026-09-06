# Your first session

This walks through installing DevDesk, launching it, and running your first
security scan against a local repository. By the end you will have a working
`~/.devdesk/` context and know your way around the command palette.

## 1. Install

You need Go 1.25.5+ and a terminal with [Nerd Font](https://www.nerdfonts.com/)
support (DevDesk uses icons throughout the UI).

```bash
git clone https://github.com/anthnel/devdesk.git
cd devdesk
mise run build      # → bin/dk
```

No `mise`? `go build -o bin/dk .` works too — `mise` only pins tool versions
and wires up the project's tasks.

## 2. Launch

```bash
./bin/dk
```

The **Dashboard** view opens first. It is a summary — forge stats, Docker
usage, tool availability — and every number on it is a shortcut into the view
that produced it.

## 3. Open the command palette

Press `ctrl+p` (or `:` when no text field has focus). Type `workspaces` and
press `Enter` — or just type `ws`, its short alias.

```
:ws
```

The **Workspaces** view lists the git repositories under
`app.workspaces_dir` (`~/workspaces` by default — see
[Configure a context](../how-to/configure-a-context.md) to change it). If
the list is empty, that directory has no git repositories yet; clone one
with `git clone` in a separate terminal, or use `git-explorer` once you have
authenticated against a forge (see the next tutorial step for authentication,
or skip straight to a scan if you already have a local clone).

## 4. Run a scan

With a repository selected in Workspaces, press `S` (uppercase). DevDesk
runs [Trivy](https://trivy.dev/) (CVEs, secrets, misconfiguration) and
[Gitleaks](https://github.com/gitleaks/gitleaks) (secrets) against it in the
background.

Press `:jobs` (or `:j`) to watch it run — scans are jobs, not blocking waits,
so you can keep navigating while one is in flight.

When it finishes, `:sec` (Security) shows the findings: severities, file
locations, and remediation notes where Trivy provides them.

## 5. What's next

- [Configure a context](../how-to/configure-a-context.md) — point DevDesk at
  your own workspaces directory, forge, and registry.
- [Switch forge backend](../how-to/switch-forge-backend.md) — target GitHub
  instead of GitLab.
- [Command reference](../reference/commands.md) — every command and alias.
- [Keybinding reference](../reference/keybindings.md) — the full vocabulary.
