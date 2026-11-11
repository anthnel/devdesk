# Activity indicator

In most tables, the first column (no title) tells you what is happening on a
row. A row at rest shows its usual icon, or nothing. The glyph carries the
meaning and the color repeats it, so nothing relies on color alone. Press `?` in
a view for the same legend.

## Glyphs

| Glyph | State | Meaning |
|---|---|---|
| `●` / `○` | Running | An operation is running on the row. The dot blinks. |
| `◴` | Queued | Waiting to start. |
| `⊗` | Failed | The last attempt failed. It stays until the next attempt on that row; the footer gives the cause. A container that exited with a non-zero code, or is dead, also shows it. |
| `◐` | Suspended | An agent that has been idle, or a paused container. |
| `◕` | Restarting | A container that is restarting. |
| `◉` | Done | A finished job, clone or agent. |
| `⊘` | Cancelled | A job you stopped. |
| `⊖` | Skipped | A job skipped, a repository already on disk, or a container that exited with code 0. |
| `◌` | Unknown | An agent whose state is not known, or a container that is created but not started. |

A view only uses the states that make sense for its rows. The color says the
state, never the operation: a scan, a clone and a `docker stop` all show the
same running dot. Failed is red, running is the running color, suspended and
restarting share one color, and the rest are neutral. Done is not green.

## When the terminal is not focused

While the terminal window has no focus, DevDesk stops the blink: running rows
show a steady `●`, and the periodic reads of the dashboard, `:oci`,
`:containers`, the Ports tab and `:agent-usage` are skipped. Jobs keep running.
When the focus comes back, each view reads once and the blink restarts. In
`:agent-usage` the header shows `Updates › paused (terminal not focused)` in the
meantime.

Set `app.when_unfocused` to `run` (or use `:cfg` > Appearance > When unfocused)
to suspend nothing. See [Configuration](configuration.md).

DevDesk learns about focus from the terminal (xterm focus reporting, mode
1004). Under tmux, add this to `~/.tmux.conf`, or tmux never forwards it:

```
set -g focus-events on
```

A terminal without mode 1004 never reports a blur, so DevDesk behaves as if it
were always focused: nothing is paused. A visible window that is not focused,
such as a side pane, counts as blurred.

A running container shows its usual container icon, not a dot: the dot appears
while an action (stop, restart, pause, resume, delete) is running on it.

## Views

| View | What the column shows |
|---|---|
| `:jobs` | The state of each run and of each target. |
| Workspaces (`:ws`) | A scan, sync or delete on a repository; a remembered failure. |
| Security (`:sec`) | A scan on a repository or image; a remembered failure. |
| OCI resources (`:oci`): Images, Networks, Volumes, Registries, registry browser | A scan, pull, removal, login or logout on the row. |
| Forge explorer (`:ge`) and clone list | A create or delete on a row; the state of each clone. |
| Templates (`:tpl`) | A sync or scan on a template. |
| Ports tab of `:net` | A kill in flight. |
| `:agent-usage` | The state of each agent: running, idle after 5 minutes, done, unknown. |
| Containers (`:ct`) | The container's state and any action running on it. |

The footer describes the selected row in words, next to the overall progress.

## Turn the blinking off

Set `app.motion` to `static` in the context file, or in `:cfg` > Appearance >
Motion. A running row then shows a steady `●`. The change applies without a
restart. See the [configuration reference](configuration.md).
