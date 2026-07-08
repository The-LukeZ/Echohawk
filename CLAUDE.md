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

Requires a `.env` file (see `.env.example`) and a reachable Valkey instance. `VALKEY_ADDR` must be `valkey:6379` when running via Docker Compose. A SQLite config database is created automatically on first run (default path `./data/echohawk.db`, override with `SQLITE_PATH`); no separate init step or migration tool is needed.

## Architecture

All source lives in `cmd/echohawk/` (package `main`), split by concern:

- `main.go` - entrypoint: env wiring for secrets/bootstrap, Valkey/SQLite/disgo client setup, gateway startup
- `db.go` - `openDB`: creates/migrates the SQLite file, seeds the `config` row and `messages` templates from legacy env vars on first run only
- `store.go` - `Config` struct and `configStore.Load()`, which reads the full runtime config out of SQLite
- `similarity.go` - `normalize`/`similarity` (Levenshtein-based)
- `storage.go` - `cachedMsg`/Valkey entry encode/decode, plus the `maxCached`/`cacheTTL` compile-time constants
- `checker.go` - `Checker` struct, channel-exclusion logic, `HandleMessage` pipeline
- `actions.go` - `executeActions` (alert/delete/dm/timeout/kick/ban) and `renderMessage` (template rendering)
- `commands.go` - the `/config` slash command tree (`commandDefs`), its handler (`Checker.HandleConfigCommand`), and per-subcommand validation

- **Config split**: secrets/bootstrap (`BOT_TOKEN`, `VALKEY_ADDR`, `GUILD_ID`, `SQLITE_PATH`) stay in `.env`. Everything runtime-mutable - `similarity_min`, `alert_after`, `window_seconds`, `timeout_duration`, `unify_attachments`, `actions`, `alert_channel_id`, `excluded_channel_ids`, plus the customizable message templates - lives in the SQLite `config` (single row, `id = 1`) and `messages` tables. `.env` values for these are only read once, to seed the DB on its very first run; after that the DB is authoritative and env vars for them are ignored. `maxCached`/`cacheTTL` remain compile-time constants (not DB-backed).
- **`Checker` struct**: holds the Valkey client, guild ID, a `*configStore`, and the runtime `Config` behind an `atomic.Pointer[Config]` (field `cfg`, accessed via `Cfg()`/`SetCfg()`). The atomic pointer - not a plain `*Config` - is what lets `/config` commands hot-swap the running config without a restart or blocking readers on the `HandleMessage` hot path. `checker.HandleMessage` and `checker.HandleConfigCommand` are registered as disgo event listeners in `main()`.
- **Message pipeline** (`HandleMessage`): loads `cfg := c.Cfg()` once at the top, then normalize (`cfg.UnifyAttachments`) → filter (bot/DM/wrong-guild/excluded-channel/empty) → fetch user's cached messages from Valkey (`user:<id>:messages`, a list) → compute Levenshtein-based similarity (`similarity()`) against each cached message → store the new message (LPUSH + LTRIM to `maxCached` + EXPIRE `cacheTTL`) → if any match ≥ `cfg.SimilarityMin`, atomically INCR `user:<id>:similar_count` (TTL `cfg.WindowSeconds`, set only on first increment - this is what makes it a sliding window) → if count ≥ `cfg.AlertAfter`, delete the counter and call `executeActions`.
- **`executeActions`**: takes the loaded `*Config` as a parameter (not read off `Checker`) and iterates `cfg.Actions` (map used for membership, not order), running whichever of `alert`, `delete_all`, `delete_last`, `dm_user`, `timeout_user`, `kick_user`, `ban_user` are enabled. `alert` always fires regardless of other actions configured. `delete_all`/`delete_last` are mutually exclusive, same for `kick_user`/`ban_user` - enforced both by `/config actions add` (rejects the conflicting pair) and by convention if the DB is hand-edited. `timeout_user`/`kick_user`/`ban_user` pass an audit-log reason via `rest.WithReason`.
- **Customizable messages**: the `messages` table holds one template per key (`alert`, `dm_user`, `timeout_reason`, `kick_reason`, `ban_reason`), seeded with defaults on first run. `renderMessage` (`actions.go`, now a free function taking `*Config`) fills in `{user_id}`, `{count}`, `{window}`, `{content}` via `strings.NewReplacer`. Edit via `/config message set <key> <template>` (rejects empty templates and unknown `{...}` placeholders) or `/config message reset <key>`.
- **`/config` command**: single guild-scoped slash command with subcommands `view`/`set` and subcommand groups `actions` (add/remove), `excluded_channels` (add/remove), `message` (set/reset). Gated by `DefaultMemberPermissions = PermissionManageGuild` at Discord's level, re-checked in `HandleConfigCommand` against the invoking member. Every setter in `store.go` (`SetSimilarityMin`, `SetAlertAfter`, etc.) re-`Load()`s and returns a fresh `*Config`, which the handler then pushes via `checker.SetCfg`. Registered per-guild (instant propagation) on every startup in `main()` - idempotent, so command tree changes ship with each deploy.
- **Storage format**: each cached message is a single string `channelID|messageID|content` (`formatEntry`/`parseEntry`), so `delete_all` can recover message IDs to bulk-delete later. Entries without embedded IDs are treated as legacy/test data with empty channel/message IDs.
- **Single-guild only**: the bot is not designed for multi-guild use; there's no per-guild config, just one global `config` row.

Full behavioral spec and env var reference: `docs/02_HOW_IT_WORKS.md` and `docs/01_SETUP.md` (may be stale re: SQLite migration - verify against `db.go`/`store.go` before trusting env var claims there).
