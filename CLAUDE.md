# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Echohawk: single-package Go Discord bot (`cmd/echohawk/`) detecting repeated/near-duplicate messages per user and taking configurable moderation actions. Uses `disgo` for Discord gateway/REST and Valkey (Redis-compatible) for per-user message caches and rolling counters. First Go project of the author - comments in code lean pedagogical (Go-vs-TS/JS analogies); keep that style if adding comments.

## Commands

```bash
go mod tidy                            # deps
go build -o echohawk ./cmd/echohawk     # build
go run ./cmd/echohawk                   # run directly (loads .env via godotenv, from repo root)
go test ./...                            # all tests
go test -run TestSimilarity ./...        # single test
go test -bench=. -benchmem ./...         # benchmarks (bench_test.go)
docker compose up -d --build             # run bot + Valkey via compose
```

Requires a `.env` file (see `.env.example`) and a reachable Valkey instance. `VALKEY_ADDR` must be `valkey:6379` when running via Docker Compose.

## Architecture

All source lives in `cmd/echohawk/` (package `main`), split by concern:

- `main.go` - entrypoint: env/config wiring, Valkey/disgo client setup, gateway startup
- `config.go` - env-driven config vars and `init()`
- `similarity.go` - `normalize`/`similarity` (Levenshtein-based)
- `storage.go` - `cachedMsg` and Valkey entry encode/decode
- `checker.go` - `Checker` struct, channel-exclusion logic, `HandleMessage` pipeline
- `actions.go` - `executeActions` (alert/delete/dm/timeout/kick/ban)

- **Config**: package-level vars in `config.go` (`similarityMin`, `alertAfter`, `windowSeconds`, `timeoutDuration`, `actions`) are set from env vars in `init()`, with defaults if unset. `maxCached`/`cacheTTL` are compile-time constants.
- **`Checker` struct**: holds the Valkey client, alert channel, guild ID, excluded channels, and a copy of the config values. `checker.HandleMessage` is registered as the disgo `MessageCreate` event listener in `main()`.
- **Message pipeline** (`HandleMessage`): normalize → filter (bot/DM/wrong-guild/excluded-channel/empty) → fetch user's cached messages from Valkey (`user:<id>:messages`, a list) → compute Levenshtein-based similarity (`similarity()`) against each cached message → store the new message (LPUSH + LTRIM to `maxCached` + EXPIRE `cacheTTL`) → if any match ≥ `similarityMin`, atomically INCR `user:<id>:similar_count` (TTL `windowSeconds`, set only on first increment - this is what makes it a sliding window) → if count ≥ `alertAfter`, delete the counter and call `executeActions`.
- **`executeActions`**: iterates the `actions` set (map used for membership, not order) and runs whichever of `alert`, `delete_all`, `delete_last`, `dm_user`, `timeout_user`, `kick_user`, `ban_user` are enabled. `alert` always fires regardless of other actions configured. `delete_all`/`delete_last` are mutually exclusive by convention (not enforced in code), same for `kick_user`/`ban_user`.
- **Storage format**: each cached message is a single string `channelID|messageID|content` (`formatEntry`/`parseEntry`), so `delete_all` can recover message IDs to bulk-delete later. Entries without embedded IDs are treated as legacy/test data with empty channel/message IDs.

Full behavioral spec and env var reference: `docs/02_HOW_IT_WORKS.md` and `docs/01_SETUP.md`.
