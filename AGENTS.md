# AGENTS.md

## Project Overview
- Go-based bot project
- Entry point: `cmd/nekomimi/main.go`
- Default config path: `config/config.yml`

## Prerequisites
- Go `1.25.7` (see `go.mod`)

## Setup
- Copy `config/config.example.yml` to `config/config.yml`
- Update values for bot nickname, LLM provider, and WebSocket driver

## Common Commands
- Install deps: `go mod download`
- Run: `go run ./cmd/nekomimi`
- Test: `go test ./...`

## Code Style
- Use `gofmt` on changed Go files

## Notes
- `config/config.yml` must exist before startup (it is not auto-generated)
- When `config/config.yml` structure changes, update `config/config.example.yml` in sync
- ZeroBot API reference: https://pkg.go.dev/github.com/wdvxdr1123/ZeroBot
- Prefer behavior matching the version pinned in `go.mod`; if docs differ from local code, trust local dependency version first
