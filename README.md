# komit

A TUI for selecting changed files and committing them with a Claude-generated message.

By default komit shells out to the `claude` CLI in headless mode and uses your existing Claude subscription — no API key, no metered billing. Without that subscription, point it at the Anthropic API or any OpenAI-compatible endpoint instead; see [Providers](#providers).

## Install

```
go install github.com/BlazeMV/komit/cmd/komit@latest
```

Re-run the same command to update; it replaces the binary in place. `komit --version` reports what you have, and [releases](https://github.com/BlazeMV/komit/releases) lists what is current.

## Build from source

Requires Go 1.25 or later.

```
git clone https://github.com/BlazeMV/komit.git
cd komit
go build -o komit ./cmd/komit
go test ./... -race
```

`go install ./cmd/komit` puts it on your PATH. `CLAUDE.md` covers the stack quirks worth knowing before changing anything.

## Keys

| Key | Action |
| --- | --- |
| `space` | select |
| `a` | all |
| `↑` `↓` / `k` `j` | move |
| `d` | diff pane |
| `tab` | cycle focus |
| `g` | generate |
| `r` | regenerate with a nudge |
| `e` | edit |
| `E` | open `$EDITOR` |
| `A` | amend |
| `c` | commit |
| `P` | commit and push |
| `R` | refresh the change list |
| `esc` | cancel |
| `q` | quit |

## Config

komit commits exactly the files you select, using `git commit --only`, leaving the rest of your index untouched.

Config lives at `$XDG_CONFIG_HOME/komit/config.yml` (else `~/.config/komit/config.yml`), overridden per key by a `.komit.yml` in the repo root.

`komit init` writes the defaults and refuses to overwrite:

```yaml
provider: claude-cli
recent_commits: 10
refresh:
  on_focus: true
  interval: 10
providers:
  claude-cli:
    type: claude-cli
    model: haiku
  anthropic:
    type: anthropic
    model: claude-haiku-4-5
  openai:
    type: openai
    model: gpt-5.6-luna
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

`recent_commits` is how many commit subjects fill `{{recent_commits}}`, which anchors the generated message to the repo's existing style. Set it to `0` to leave the placeholder empty and skip the `git log` entirely.

`refresh` controls how the change list picks up edits made outside komit. `on_focus` reloads it when the terminal regains focus; `interval` is the seconds between background polls, which run only while komit has focus and pause while it is generating or committing. Set `interval: 0` to poll only when you press `R`.

A refresh keeps your selection: files you ticked stay ticked, and a file that appears joins the selection only if everything was already selected. A commit is the exception — it re-applies the startup rule to whatever is left.

## Providers

Each block under `providers` is a backend; `provider` names the one to use. There are three kinds:

| Kind | Generates via | Needs |
| --- | --- | --- |
| `claude-cli` (default) | the `claude` CLI, on your subscription | the binary on PATH |
| `anthropic` | the Anthropic Messages API | an API key |
| `openai` | any OpenAI-compatible `/chat/completions` endpoint | an API key, unless `base_url` is set |

A block's name is a label of your choosing and is never read as a kind — `type` is required on every block and names the kind. Any number of blocks can share one, so since `openai` takes a `base_url`, that covers OpenRouter, Groq, DeepSeek, xAI, Ollama and LM Studio:

```yaml
provider: ollama

providers:
  claude-cli:
    type: claude-cli
    model: haiku
  ollama:
    type: openai
    model: qwen2.5-coder:7b
    base_url: http://localhost:11434/v1   # a local server needs no key at all
  openrouter:
    type: openai
    model: anthropic/claude-haiku-4.5
    base_url: https://openrouter.ai/api/v1
    api_key_env: OPENROUTER_API_KEY
```

Configure as many as you like and switch between them by editing `provider`. Every block is validated, not just the active one, so a typo in a backend you have not switched to yet is reported straight away.

### API keys

Resolved in this order, first hit winning:

1. the variable named by `api_key_env`
2. `ANTHROPIC_API_KEY` or `OPENAI_API_KEY`
3. `api_key` in your user config

Setting `api_key_env` opts out of step 2 entirely, so a stray `OPENAI_API_KEY` cannot stand in for the OpenRouter key you meant.

`api_key` is read from your user config only. In a repo's `.komit.yml` it is ignored and komit says so — that file is one the repo can commit.

### Startup checks

komit validates the whole config before opening, and refuses to start with the problem named on stderr: any block missing `type` or naming an unknown one, a `provider` with no matching block, a block with no `model`, a missing `claude` binary, an unparseable `base_url`, or an API provider with no key. It also warns when `.komit.yml` is present but neither tracked nor ignored, since it will otherwise turn up in your change list.

Upgrading from v0.2: `model` moved from the top level into `providers.<name>.model`. komit prints the rewrite on startup.

## Uninstall

```
rm "$(go env GOPATH)/bin/komit"
rm -rf "${XDG_CONFIG_HOME:-$HOME/.config}/komit"
```

The second line removes your saved prompt. Any `.komit.yml` files you added to individual repos are left alone.
