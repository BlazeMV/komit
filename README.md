# komit

A TUI for selecting changed files and committing them with a Claude-generated message.

Requires the `claude` CLI on PATH. komit shells out to it in headless mode and uses your existing Claude subscription; there is no API key and no metered billing.

## Install

```
go install github.com/BlazeMV/komit@latest
```

## Keys

| Key | Action |
| --- | --- |
| `space` | select |
| `a` | all |
| `d` | diff pane |
| `tab` | cycle focus |
| `g` | generate |
| `r` | regenerate with a nudge |
| `e` | edit |
| `E` | open `$EDITOR` |
| `A` | amend |
| `c` | commit |
| `P` | commit and push |
| `esc` | cancel |
| `q` | quit |

## Config

komit commits exactly the files you select, using `git commit --only`, leaving the rest of your index untouched.

Config lives at `$XDG_CONFIG_HOME/komit/config.yml` (else `~/.config/komit/config.yml`), overridden per key by a `.komit.yml` in the repo root.

`komit init` writes the defaults and refuses to overwrite:

```yaml
model: haiku
prompt: |
  Write a git commit message for the diff below.

  Rules:
  - single line, imperative mood, lowercase, no trailing period
  - no body unless the change is non-obvious
  - describe what changed, not which files changed
  - output only the message, nothing else

  Recent commits in this repo, for style:
  {{recent_commits}}

  Files: {{files}}

  Diff:
  {{diff}}
```
