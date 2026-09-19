# Create a repository from a template

Templates live in a catalog you manage in the **Templates** view (`:templates`, alias `:tpl`). Once an entry exists you can pick it when creating a project. For how it all works, see [Repository templates](../explanation/templates.md).

## Add a template to the catalog

1. Open `:templates` and press `N`.
2. Fill in the name, description and tags, then cycle **Source** with `←`/`→`:
    - **git** — a clone URL, an optional subdirectory to use as the root, and a branch, tag or SHA.
    - **local** — a repository directory on this machine, and a branch, tag or SHA.
    - **oci** — a registry URL, a repository, and a tag.
3. Confirm. The slug is derived from the name and stays fixed if you edit the entry later.

`E` edits the selected entry, `D` deletes it (the confirmation defaults to **No**).

!!! note "Private repositories"
    Your forge token is sent only to the forge's own host. For a private repository elsewhere, use an SSH URL.

## Check it before you use it

| Key | Effect |
|---|---|
| `V` | Preview the files the template would put in a repository |
| `S` | Scan it with Trivy and Gitleaks — the result shows up in `:sec` |
| `F` | Read the source again and replace the cached copy |

The first read is cached. A branch that moves is **not** picked up automatically — press `F` to refresh. A template pinned to a SHA never needs it.

## Create the repository

1. Open the forge explorer (`:ge`), go to the group where the project should live, and press `N`.
2. Focus the **Template** field (it shows `none`) and press `Enter` to open the catalog.
3. Select a template with `Enter` (`V` previews it, `Esc` cancels, `/` filters). `Backspace` on the field resets it to `none`.
4. Submit the form.

The template is fetched first. If that fails, nothing is created on the forge and the footer says so. The files arrive as one initial commit on both GitLab and GitHub.

## Limits

A template can hold at most **500 files** and **32 MiB**. Symlinks and submodules are refused, with the offending path named.
