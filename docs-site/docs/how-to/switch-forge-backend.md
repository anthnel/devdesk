# Switch forge backend (GitLab / GitHub)

A context targets exactly one forge — never both at once. `internal/forge`
abstracts what DevDesk needs from either one, so the **Forge Auth** and
**Forge Explorer** views work the same regardless of which backend a context
points at.

## Set the type

```yaml
forge:
  type: github    # gitlab | github — empty means gitlab
  url: https://github.com
```

An empty `forge.type` means `gitlab`, for backward compatibility with
configs written before this key existed. Set it explicitly to `github` to
target GitHub instead.

From the **Configuration** view (`:config`), `forge.type` is a closed-set
field — cycle it with `←/→` rather than typing it by hand.

## Authenticate

```
:git-auth      # or :ga
```

The authentication view adapts its fields and its flow to whichever forge
the current context targets — a GitLab URL + personal access token, or a
GitHub token. Whichever is active, the token is stored via the context's
credential backend, never in `config.yaml`.

## Switch per context, not per session

Because the type lives in the context's config, the way to have DevDesk
target both GitLab and GitHub is two contexts:

```
:ctx personal    # → forge.type: github
:ctx work        # → forge.type: gitlab
```

See [Configure a context](configure-a-context.md) for how contexts and
`:ctx` work.

## What differs under the hood

- **Identity**: GitLab addresses things by numeric ID, GitHub by
  owner/repository path. DevDesk's forge types keep this opaque outside the
  backend that issued it.
- **Nesting**: GitLab groups nest arbitrarily; GitHub organizations don't
  nest at all. The explorer's drill-down respects whichever the current
  forge allows.
- **Decoration cost**: role and CI-status badges cost extra API calls per
  repository on GitLab. The explorer asks for them; a bulk clone's discovery
  walk does not, so cloning a large group doesn't multiply into hundreds of
  calls for a badge nobody reads in that flow.

See [Forge (GitLab / GitHub)](../explanation/forge.md) for the full design.
