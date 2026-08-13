# komit

TUI for committing a selected subset of changed files with a Claude-generated message.

## Stack gotchas

- Charm v2 libraries live on a **vanity module path**: `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`, `charm.land/lipgloss/v2`. Importing `github.com/charmbracelet/...` fails with `module declares its path as: charm.land/...`.
- **teatest is the exception** — still `github.com/charmbracelet/x/exp/teatest/v2`. Its go.mod declares the github path even though the vanity path resolves in the index.
- `tea.Model.View()` returns `tea.View`, not `string`. Layout lives in `render() string`; `View()` wraps it and sets `AltScreen`.
- There is no `tea.WithAltScreen()` in v2 — alt screen is a field on `tea.View`.
- `tea.KeyMsg.String()` returns `"space"` for the space bar, not `" "`. Hence `keyToggle = "space"`.
- Go 1.25 floor comes from `bubbles/v2`.

## Index safety

The promise: komit never leaves the user's index dirtier than it found it. Anything touching the index must hold to this.

- `Commit` uses `git commit --only -- <paths>`. Never `git add` the selection — that would swallow staged-but-unselected files.
- `MarkIntent` may only receive genuinely untracked paths (`untrackedSelected()`), never `selectedPaths()`. Its cleanup resets what it staged; handing it a deliberately-staged path would discard that state.
- Cleanups register in a drainable registry on `Repo` — a `defer` inside a `tea.Cmd` goroutine is not enough, because bubbletea does not wait for command goroutines on quit. Drained on the quit key and again in `main` after `p.Run()`.
- Index-mutating git calls are serialised behind `LockIndex`/`UnlockIndex`. Diff browsing deliberately avoids the index entirely via `git diff --no-index`.

## Config merging

- `yaml.Unmarshal` into a live `map[string]Provider` **replaces whole blocks**, so a repo file setting `providers.openai.model` would wipe `base_url` and `api_key`. Top-level keys still merge by unmarshalling over the struct (only present keys are touched, so `interval: 0` stays distinct from unset); `Providers` is merged field by field instead. Do not collapse the two.
- `api_key` is stripped from `.komit.yml` — that file is one the repo can commit. `mergeFile`'s `allowSecrets` is what enforces it.
- An empty override means "unset", so no config can blank a default. `Validate`'s empty-model rule is unreachable through `Load` and guards the runner only.

## Providers

- `Runner.Run` takes no model: which names are valid depends on the provider, so the model is fixed when the runner is built.
- `config.Validate` runs in `main` before `tea.NewProgram`. Fatal problems go to stderr and exit 1 — the alt screen would swallow them otherwise. Non-fatal warnings ride into the model and clear on the first keypress.
- `openai` sends no `max_tokens`: the reasoning models reject it.
- Provider tests use `httptest.Server`. A handler that blocks on `r.Context().Done()` will hang `srv.Close()` — a client-side cancel does not reliably cancel the server's request context, so give the handler its own release channel.

## UI invariants

- Generations are epoch-tagged; a superseded generation's result is ignored. Errors clear `busy`/`cancel` only for the current epoch.
- Refusal branches clear `m.err` — the view ranks `err` above `status`, so a stale error masks the refusal otherwise.

## Refresh

- `statusMsg.preserve` picks the selection rule: startup and post-commit reset via `applyStartupSelection`, everything else merges via `mergeSelection`. A poll that reset would wipe the selection every interval.
- The poll is a self-rescheduling `tea.Tick` chain tagged with `pollGen`. `FocusMsg` bumps the generation before scheduling, which strands any tick still in flight — two live chains would double the rate.
- `BlurMsg` only clears `focused`; the in-flight tick lands, sees it, and ends the chain. Nothing else stops the poll.
- `focused` starts true so terminals that never report focus still poll.
- `diffMsg` rewinds the viewport only when the path changed — a refresh reloading the same diff would otherwise yank the scroll to the top every interval.

## Tests

- `go test ./... -race`
- No test may invoke the real `claude` binary or the network. Exec paths go through a fake `claude` script on `PATH`; generation goes through a fake `Runner`.
- Tests use `t.Setenv`/`t.Chdir`, so no `t.Parallel()`.
- `internal/ui` tests that construct `Model{...}` directly bypass `New`, so `Update` lazily initialises the textarea.

## Release

- `-ldflags -X main.version=` sets the version; `version` is a package-level `var` in `cmd/komit`.
- Install path is `github.com/BlazeMV/komit/cmd/komit` — `main` is not at the module root.
