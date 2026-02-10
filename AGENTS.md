# AGENTS.md

## Project Overview
- Go-based bot project
- Entry point: `cmd/nekomimi/main.go`
- Default config path: `config.yml`

## Prerequisites
- Go `1.25.7` (see `go.mod`)

## Setup
- Copy `config.example.yml` to `config.yml`
- Update values for bot nickname, LLM provider, and WebSocket driver

## Common Commands
- Install deps: `go mod download`
- Run: `go run ./cmd/nekomimi`
- Test: `go test ./...`

## Code Style
- Use `gofmt` on changed Go files

## Notes
- `config.yml` is required at runtime (loaded by default)
- ZeroBot GoDoc: https://pkg.go.dev/github.com/wdvxdr1123/ZeroBot
