# How the bot works

This document describes the runtime behavior and data flow implemented in `cmd/echohawk/`.

## Overview

When a non-bot message is posted in the configured guild, the bot:

1. Loads the current runtime config (`cfg := c.Cfg()`, an in-memory snapshot backed by SQLite - see "Runtime configuration" below).
2. Normalizes the message content (`strings.ToLower` + `TrimSpace`). Empty or embed-only messages are ignored.
   - If `unify_attachments` is `true`, Discord CDN/media links (`cdn.discordapp.com`, `media.discordapp.net`) are also collapsed to a single `[attachment]` placeholder, so messages differing only by attachment URL are treated as identical content.
3. Loads the author's recent messages from Valkey using a list key `user:<userID>:messages`.
4. Computes similarity between the new message and each previous message using Levenshtein distance, converted to a 0.0–1.0 ratio:

   ```
   similarity = 1 - (levenshteinDistance / max(len(a), len(b)))
   ```

5. If at least one previous message meets `similarity_min`, the bot increments a rolling counter stored at `user:<userID>:similar_count`.
   - The counter key is created atomically via `INCR` and given a TTL of `window_seconds` on first increment.
   - This produces a sliding window: repeated similar messages within `window_seconds` seconds increment the same counter.
6. If the counter reaches or exceeds `alert_after`, the bot deletes the counter key and runs whichever moderation actions are configured (alert, delete, DM, timeout, kick, ban - see "Actions" below).

## Storage keys and invariants

- `user:<id>:messages` - list (LPUSH) of recent normalized messages for the user. The list is trimmed to `maxCached` (default 30) and given a TTL of `cacheTTL` (default 3600s) on each write.
- `user:<id>:similar_count` - incrementing integer used to count similar messages inside the rolling window. TTL equals `WINDOW_SECONDS`.

## Why Valkey and approach

- Valkey (via `valkey-go`) gives a simple, in-memory K/V API with list and TTL semantics like Redis. It is used for short-lived caches and counters.
- Using Levenshtein-based similarity tolerates small edits and punctuation differences while still recognizing repeated content.
- The separate per-user counter with a TTL implements a rolling window rather than fixed intervals.

## Runtime configuration

Every parameter below lives in the SQLite `config` table (single row, `id = 1`) plus the `messages` table for message templates - not in `.env`. `.env` only holds bootstrap/secrets (`BOT_TOKEN`, `VALKEY_ADDR`, `GUILD_ID`, `SQLITE_PATH`); its values for the fields below are read exactly once, to seed the database on its very first run, then ignored.

The bot loads this config into memory once at startup and keeps it behind an `atomic.Pointer[Config]` on the `Checker` struct. `/config` commands write straight to SQLite and then swap that pointer, so changes take effect immediately - no restart needed.

- `similarity_min` - similarity threshold (float, 0-1). Default `0.85`.
- `alert_after` - number of similar messages required to trigger actions. Default `3`.
- `window_seconds` - seconds for the rolling window. Default `300`.
- `timeout_duration` - seconds a `timeout_user` action disables a member for. Default `300`.
- `unify_attachments` - bool, collapse Discord CDN/media links to one placeholder before comparison. Default `false`.
- `alert_channel_id` - channel the `alert` action posts to.
- `excluded_channel_ids` - channels/categories/threads skipped entirely.
- `actions` - which moderation actions run when the threshold is hit (see "Actions" below).
- `maxCached` and `cacheTTL` remain compile-time constants in code (not DB-backed): `maxCached = 50`, `cacheTTL = 3600`.

### The `/config` command

A guild-scoped slash command, gated to members with the Manage Server permission (enforced by Discord via `DefaultMemberPermissions`, re-checked in the handler). Registered on every bot startup.

- `/config view` - ephemeral embed dump of all fields above (message templates shown by key only, not full text).
- `/config set <field>...` - update `similarity_min`, `alert_after`, `window_seconds`, `timeout_duration`, `unify_attachments`, and/or `alert_channel` in one call; all options optional, only the ones passed are changed.
- `/config actions add|remove <action>` - enable/disable one action. Rejects `delete_all`+`delete_last` or `kick_user`+`ban_user` both being enabled at once.
- `/config excluded_channels add|remove <channel>`.
- `/config message set <key>` - opens a modal with a multi-line text box pre-filled with the current template, so you can see and edit the existing text (including real newlines) directly, instead of retyping the whole thing into a slash-command option.
- `/config message get <key>` - ephemeral preview of a template's full current text in a code block.
- `/config message reset <key>` - restores a template to its built-in default.

Message templates are validated on write: empty templates are rejected, and any `{...}` token that isn't one of `{user_id}`, `{count}`, `{window}`, `{content}` is rejected as a likely typo.

## Actions

When the similar-message count reaches `alert_after`, the bot runs every action enabled in `actions` (a set, so order doesn't matter):

- `alert` - posts a message to `alert_channel_id`. Always runs regardless of what else is enabled.
- `delete_all` - bulk-deletes the triggering message plus every prior message that was flagged as similar (message IDs are recovered from the Valkey cache - see "Storage keys" above).
- `delete_last` - deletes just the triggering message. Mutually exclusive with `delete_all`.
- `dm_user` - DMs the author a warning.
- `timeout_user` - times the author out for `timeout_duration` seconds.
- `kick_user` - kicks the author. Mutually exclusive with `ban_user`.
- `ban_user` - bans the author. Mutually exclusive with `kick_user`.

`timeout_user`/`kick_user`/`ban_user` all attach an audit-log reason built from the `timeout_reason`/`kick_reason`/`ban_reason` message templates.

## Extensibility notes

- Excluding channels: `excluded_channel_ids` can be set (via `/config excluded_channels add`) to skip specific channels, categories, or threads. Exclusion checks walk the channel's parent chain (thread -> parent channel -> category) via disgo's in-memory channel cache, so excluding a category or channel also excludes everything nested under it.
- The similarity function and thresholds can be tuned or replaced (e.g., fuzzy hashing, NLP embeddings) for different tolerance.
