# komit

**Goal:** TUI replacement for PhpStorm's commit window — select changed files, Claude writes the commit message from a user-defined prompt, commit.

## Shape

- Standalone Go binary (`komit`), bubbletea/lipgloss TUI, runs in any git repo
- Message generation shells out to `claude -p` — uses the existing Max/Pro subscription, no API key, no metered billing
- Distribution: `go install`, homebrew tap, GitHub release binaries
- Not a Claude Code plugin; lives outside `blaze-claude-plugins`

## UI

```
┌ komit ── blaze-claude-plugins · main ↑2 ──────────────────────────────────┐
│ Changes (2/4 selected)          │ Diff · src/Services/BillingService.go   │
│ ▸[x] M  cmd/komit/main.go       │ @@ -0,0 +1,42 @@                        │
│  [x] A  internal/ai/claude.go   │ +package services                       │
│  [ ] M  internal/ui/keys.go     │ +                                       │
│  [ ] D  legacy/old.go           │ +func NewBilling() *Billing {           │
├─ Message ─────────────────────────────────────────────────────────────────┤
│ feat: add claude generation backend                                       │
├───────────────────────────────────────────────────────────────────────────┤
│ space sel · a all · d diff · g gen · r regen · e edit · c commit · P push │
└───────────────────────────────────────────────────────────────────────────┘
```

| Key | Action |
|---|---|
| `j/k` `↑/↓` | move cursor |
| `space` / `a` | toggle file / toggle all |
| `tab` | switch pane focus |
| `d` | show/hide diff pane (follows cursor, scrollable when focused) |
| `g` | generate message from selected files' diff |
| `r` | regenerate with a one-line nudge |
| `e` / `E` | edit message inline / in `$EDITOR` |
| `A` | amend mode |
| `c` / `P` | commit / commit + push |
| `q` | quit (confirm if message unsaved) |
| `esc` | cancel in-flight generation |

**Flow rules**
- Startup selection: staged files if any are staged, otherwise everything. No persisted state.
- `c` with empty message is blocked.
- Commit failure (hook rejection etc.) keeps the TUI open, shows git stderr, refreshes the list.
- Successful commit refreshes in place — multiple batches per launch.
- Amend mode shows an amber banner; refused when HEAD is already on the upstream.

## Git semantics

| Concern | Approach |
|---|---|
| Commit only selected | `git commit --only -- <paths>` — index of unselected files untouched |
| Untracked files | `git add -N <path>` first so `--only` accepts them |
| Partially staged file | `--only` commits full working-tree content; marked `±` in the list |
| Diff for generation | `git diff HEAD -- <selected>` |
| Amend | `git commit --amend --only -- <paths>`; refuse unless `git rev-list @{u}..HEAD` contains HEAD |
| Implementation | shell out to `git`, not go-git — keeps hooks, aliases, worktrees, signing |

## Generation

```
claude -p --model haiku --output-format text --safe-mode --no-session-persistence
```

- `--safe-mode` disables CLAUDE.md, hooks, MCP, plugins → hermetic and fast
- Prompt on stdin, 30s timeout, cancellable
- Diff truncated at 60KB: per-file cap first, then whole-diff cap
- Binary/lockfile diffs replaced with `<binary, N bytes changed>`
- Output stripped of code fences and preamble
- Exact flag set verified against `claude --help` at implementation time

## Config

- `~/.config/komit/config.yml`, overridden per-key by `.komit.yml` in the repo root
- Template vars: `{{diff}}`, `{{files}}`, `{{branch}}`, `{{recent_commits}}`
- Built-in default prompt so `komit` works with zero setup; `komit init` writes the default file

```yaml
model: haiku
prompt: |
  Write a git commit message for this diff.
  Single line, imperative, lowercase, no period.
  No body unless the change is non-obvious.

  Recent commits for style reference:
  {{recent_commits}}

  Diff:
  {{diff}}
```

## Packages

```
cmd/komit/main.go        flags (--version, init), repo detection, bubbletea boot
internal/git/            status, diff, commit, amend, push, upstream state
internal/config/         load + merge + template render
internal/ai/             prompt build, truncation, claude runner (interface)
internal/ui/             model, update, view, filelist, diffview, message, keys, theme
```

## Errors

Every case keeps the TUI alive with git's stderr in the status bar, except the first:

- not a git repo → hard exit before TUI starts
- no changes → empty state
- `claude` not on PATH → error + install hint; `e` still works for manual messages
- generation timeout / non-zero exit → retry or write manually
- hook rejection, push rejected → stderr shown, list refreshed

## Testing (TDD)

- `git`: real repos in `t.TempDir()`, table-driven; partial-staging and untracked cases tested explicitly — that is where data loss would hide
- `ai`: runner interface with a fake; one fake `claude` script on `PATH` covers the exec path
- `config`: YAML fixtures for merge precedence and template rendering
- `ui`: `teatest` golden flows — select → generate → commit, and amend refusal

## Done criteria

- [ ] `komit` in a dirty repo: select a subset, `g`, `c` → only those files committed, index of the rest unchanged
- [ ] untracked and partially-staged files commit correctly
- [ ] `.komit.yml` prompt overrides the global one
- [ ] amend refused on a pushed HEAD
- [ ] `claude` missing → usable with a manually typed message
- [ ] `go test ./...` green
- [ ] `go install github.com/BlazeMV/komit@latest` works from a clean machine
