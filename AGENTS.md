# AGENTS.md

## Project Overview
- Go-based bot project
- Entry point: `cmd/nekomimi/main.go`
- Default config path: `config/config.yml`
- Main modules:
  - `internal/bot`: command registration, runtime wiring, immersive chat behavior
  - `internal/llm`: provider adapter, request client, history/context management
  - `internal/config`: YAML loading and prompt reference resolution

## Prerequisites
- Go `1.25.7` (see `go.mod`)

## Setup
1. Copy `config/config.example.yml` to `config/config.yml`
2. Update required values:
   - `nickname`
   - `super_users`
   - `driver.websocket.url`
   - `driver.websocket.token`
3. If enabling LLM (`llm.enabled: true`), also set:
   - `llm.provider`
   - `llm.key`
   - `llm.model`
   - `llm.api` (optional; can be auto-normalized)

## Runtime and Config Notes
- `config/config.yml` must exist before startup (not auto-generated).
- App currently always loads `config/config.yml` (no custom path CLI flag).
- API auth state persists to SQLite at `data/auth.db` (relative to process working directory).
- For Docker persistence, mount host directory to container `data`: `./data:/app/data`.
- `llm.provider` behavior:
  - `responses`: supported
  - `openai`: supported
  - `gemini`: currently not implemented (will return error)
  - Any unknown provider value falls back to `responses`
- `llm.api` is normalized by provider:
  - `openai` -> `/chat/completions`
  - non-openai -> `/responses`
- `llm.system_prompt` is appended after built-in system prompts.
- `llm.system_prompt` supports file refs: `{{file:relative/path.txt}}`
  - Absolute paths are rejected.
  - Path traversal outside config root is rejected.
  - Ambiguous file matches cause load failure.
- In-memory conversation history is not persisted across process restart.
- `/reload` hot-reloads config but keeps existing in-memory session history.

## Common Commands
- Install deps: `go mod download`
- Run: `go run ./cmd/nekomimi`
- Print version: `go run ./cmd/nekomimi --version`
- Test: `go test ./...`
- Format changed files: `gofmt -w <changed-go-files>`

## Command Discovery Rule
- Do not treat `AGENTS.md` as the source of truth for bot command list.
- Agents must inspect `internal/bot/commands` for current commands, args, and permission scope.
- Recommended quick scan:
  - `rg -n "OnCommand\\(|OnFullMatch\\(" internal/bot/commands`
- If docs and code differ, trust local code first.
- When command behavior changes, update tests in the same module; do not maintain a duplicated command catalog in `AGENTS.md`.

## Code Style
- Use `gofmt` on all changed Go files.
- Keep changes minimal and scoped; avoid unrelated refactors.
- Prefer behavior matching the version pinned in `go.mod`; if docs differ, trust local dependency version.

## Change Checklist (Definition of Done)
- Changed Go files are formatted with `gofmt`.
- `go test ./...` passes locally.
- If config schema changed:
  - Update `config/config.example.yml` in sync.
  - Update/add tests under `internal/config` and relevant modules.
- If command behavior changed:
  - Verify permission scope (public vs SuperUser).
  - Add/adjust command tests where applicable.

## References
- ZeroBot API reference: https://pkg.go.dev/github.com/wdvxdr1123/ZeroBot
