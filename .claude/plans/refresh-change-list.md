# refresh the change list

Pick up work done outside komit without restarting: manual key, on terminal focus, and a poll that pauses while blurred.

## Steps

- [x] `config`: add `Refresh{OnFocus bool, Interval int}` under a `refresh:` key, plus `Every() time.Duration` (0 disables)
- [x] `config/default.yml`: `on_focus: true`, `interval: 10`
- [x] `cmd/komit`: `komit init` writes the `refresh` block
- [x] `keys.go`: `keyRefresh = "R"` (`r` is regenerate)
- [x] `model.go`: `statusMsg.preserve`, `refreshTickMsg{gen}`, `focused`/`pollGen` fields, `mergeSelection`, `focusPath`
- [x] `update.go`: `loadStatus(preserve bool)`; `Init` starts the poll; `R`/`FocusMsg`/`refreshTickMsg` refresh with preserve; `BlurMsg` clears `focused`, which ends the chain at the next tick; all three skip while `busy`
- [x] `update.go`: statusMsg reloads the diff when the pane is open; `diffMsg` only jumps to top when the path changed
- [x] `view.go`: `ReportFocus` on the view, `R refresh` in the help line
- [x] tests: `mergeSelection` rules, preserve vs reset, `R` (incl. busy no-op), focus/blur/tick chain, diff scroll retention, config defaults + override, `komit init` output, teatest e2e
- [x] README keys table + `refresh` config docs; CLAUDE.md invariants

## Done when

- `go test ./... -race` passes
- A file changed outside komit shows up on `R`, on regaining focus, and within one interval while focused
- A curated selection survives every refresh; new files inherit selection only when everything was selected
- No poll runs while the terminal is blurred, and no key or tick refreshes mid-generation or mid-commit
