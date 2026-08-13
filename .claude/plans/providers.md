# providers

Let komit generate through the `claude` CLI, the Anthropic API, or any OpenAI-compatible endpoint, so it is usable without a Claude Code subscription.

## Shape

```yaml
provider: openai
providers:
  claude-cli:
    model: haiku
    bin: claude                              # optional, non-PATH binary
  anthropic:
    model: claude-haiku-4-5
    api_key_env: ANTHROPIC_API_KEY
  openai:
    model: anthropic/claude-haiku-4.5
    base_url: https://openrouter.ai/api/v1   # unset = api.openai.com
    api_key_env: OPENROUTER_API_KEY
```

| Provider | Transport | Default model | Key |
| --- | --- | --- | --- |
| `claude-cli` (default) | `exec`, unchanged | `haiku` | — |
| `anthropic` | `POST /v1/messages`, `x-api-key`, `anthropic-version: 2023-06-01` | `claude-haiku-4-5` | required |
| `openai` | `POST {base_url}/chat/completions`, `Authorization: Bearer` | `gpt-5-mini` | required only when `base_url` is unset |

Raw `net/http` for both API providers — one non-streaming turn, no new dependencies.

Key resolution, first hit wins: `api_key_env` (if named, consulted alone) → provider default env var → `api_key`.

`api_key` is accepted in the user config only; in `.komit.yml` it is stripped and warned about.

## Validation

Fatal, on stderr before the TUI, exit 1:

- top-level `model:` set — moved under `providers.<name>.model`, print the rewrite
- `provider:` not one of the three
- active provider block missing, or its `model` empty
- `claude-cli` and `bin` not on PATH
- `anthropic` with no resolvable key
- `openai` with no `base_url` and no resolvable key
- `base_url` unparseable

Warnings, on the status line:

- `api_key` found in `.komit.yml`
- `.komit.yml` exists, is untracked, and no `.gitignore` rule covers it

## Steps

- [x] `config`: `Provider{Model, BaseURL, APIKey, APIKeyEnv, Bin}` + `Providers map[string]Provider` + `Provider string`; keep `Model string` solely as a tripwire for the v0.2 layout
- [x] `config`: `Load` keeps per-file `Unmarshal` for top-level scalars — `interval: 0` must stay distinguishable from unset — and hand-merges `Providers` per field; `yaml.Unmarshal` into an existing map replaces whole blocks
- [x] `config`: `mergeFile(cfg, path, allowSecrets)`; strip `api_key` from every incoming block when false, returning a warning
- [x] `config`: `Active() (Provider, error)`; `Validate() []error` covering every rule above — `Active()` needs no error, the caller validates first
- [x] `config`: `RepoFileWarning(repo)` — exists && `git ls-files` empty && `git check-ignore` non-zero — landed as `git.Repo.Loose`, which is where `run` lives
- [x] `config/default.yml`: `provider: claude-cli` + all three blocks
- [x] `ai`: `Runner.Run(ctx, prompt)` — model moves onto the runner, it is provider config; update `Generate`, `CLI`, and both stubs
- [x] `ai/provider.go`: `New(cfg) (Runner, error)`
- [x] `ai/anthropic.go`, `ai/openai.go`: request, response, error mapping, context cancellation
- [x] `ai/claude.go`: `Model` field; drop `ErrMissing` in favour of validation — kept, it still guards the binary vanishing mid-session
- [x] `cmd/komit`: run `Validate` before `tea.NewProgram`; `komit init` writes the nested config
- [x] `ui`: drop `claudeMissingHint`; carry the config warnings into the startup status
- [x] confirm `gpt-5-mini` and `claude-haiku-4-5` are current ids — `gpt-5-mini` was retired; now `gpt-5.6-luna`
- [x] tests: nested merge keeps sibling keys, `interval: 0` still disables polling, repo `api_key` stripped, every validation rule, both runners against `httptest.Server`, `komit init` output
- [x] README config section and the `model: haiku` example; CLAUDE.md gotchas

## Done when

- [x] `go test ./... -race` passes, and no test reaches the network
- [x] Each of the three providers generates a message end to end — `openai` against OpenAI itself is blocked on account credit; auth, model id and error mapping all confirmed correct against the live endpoint
- [x] `provider: openai` with `base_url` pointing at Ollama works with no key set — `qwen2.5-coder:7b`, 823ms warm
- [x] Every misconfiguration in the list above fails on stderr with the fix named, before the TUI opens
- [x] A `.komit.yml` carrying `api_key` never contributes it, and says so
- [x] An untracked, unignored `.komit.yml` warns once at startup; a tracked or ignored one is silent

## Follow-up

Blocks are labels carrying a required `type`, added after the plan was written — several OpenAI-compatible endpoints can be configured at once and switched between by editing `provider`.
