# Command-based config plan

Goal: edit `config`/`messages` SQLite rows via Discord slash commands, no more manual DB edits.

## Commands (single top-level `/config` w/ subcommands, guild-only, admin-perm-gated)

- `/config view` — embed dump of current config + actions + messages
- `/config set` — options (all optional, set what's passed):
  - `similarity_min` (number)
  - `alert_after` (int)
  - `window_seconds` (int)
  - `timeout_duration` (int)
  - `unify_attachments` (bool)
  - `alert_channel` (channel (text and threads only))
- `/config actions add|remove <action>` — action = choice from alert/delete_all/delete_last/dm_user/timeout_user/kick_user/ban_user. Enforce mutual exclusivity (delete_all vs delete_last, kick_user vs ban_user) in code, reject with error instead of silent overwrite.
- `/config excluded_channels add|remove <channel>`
- `/config message set <key> <template>` — key = choice from alert/dm_user/timeout_reason/kick_reason/ban_reason. Validate placeholders (only allow `{user_id} {count} {window} {content}` tokens, warn on unknown `{...}` left in template).
- `/config message reset <key>` — restore default from `defaultMessages`.

Default to one `/config` command w/ subcommand groups (`actions`, `excluded_channels`, `message`) — avoids Discord's 100-command clutter, matches disgo's `discord.SlashCommandCreate` SubcommandGroups.

## Permission gate

Restrict at Discord level: register command w/ `DefaultMemberPermissions` = `discord.PermissionManageGuild` (or `ManageServer`), so no in-code role check needed beyond guild-only + defer. Belt-and-suspenders: re-check `event.Member().Permissions.Has(discord.PermissionManageGuild)` in handler in case of API changes, reject silently-configured overrides.

## Code changes

New file `commands.go`:

- `commandDefs []discord.ApplicationCommandCreate` — the `/config` tree
- `registerCommands(client bot.Client, guildID snowflake.ID)` — call in `main()` after client build, before `OpenGateway` (guild-scoped registration, instant vs global's ~1hr propagation — fits single-guild design)
- `(c *Checker) HandleConfigCommand(e *events.ApplicationCommandInteractionCreate)` — dispatch on subcommand/group, validate input, write via new `configStore` methods, then swap `c.cfg` (see below), reply w/ ephemeral confirmation embed

`store.go` additions:

- `(s *configStore) UpdateField(field string, value any) error` or discrete typed setters (`SetSimilarityMin`, `SetAlertAfter`, ...) — discrete setters preferred, keeps SQL explicit or use one `UPDATE config SET x = ? WHERE id = 1` per call built by a small internal helper (column name is trusted/hardcoded per setter, never interpolated from user text, so no injection risk)
- `(s *configStore) SetAction(action string, enabled bool) error` — read actions CSV, add/remove, enforce exclusivity, write back
- `(s *configStore) SetExcludedChannel(id snowflake.ID, excluded bool) error`
- `(s *configStore) SetMessage(key, template string) error`
- All setters re-`Load()` at the end and return fresh `*Config` (or caller reloads) — avoids duplicating parse logic

**Live reload problem**: `Checker.cfg` is a `*Config` loaded once at startup (per CLAUDE.md, "not live-reloaded"). Commands mutating the DB must update the running config too, else edits need a bot restart to take effect — defeats the purpose. Options:

1. `Checker.cfg` becomes `atomic.Pointer[Config]` — command handler calls `store.Load()` then `checker.cfg.Store(newCfg)`, `HandleMessage` calls `checker.cfg.Load()` at top instead of direct field access. Small blast radius, no lock contention on hot path.
2. Mutex-protected `*Config` w/ RLock in `HandleMessage`.

Go with (1), atomic.Pointer — read-mostly workload, no reader blocking.

## Validation rules to enforce in handler (not just DB constraint)

- `similarity_min` in `[0, 1]`
- `alert_after` >= 1
- `window_seconds` >= 1
- `timeout_duration` >= 0 (Discord max 28 days = 2419200s, clamp/reject above)
- `alert_channel` must be text channel in same guild (disgo gives `discord.Channel` directly from option, no extra fetch)
- action add: reject `delete_all`+`delete_last` both set, `kick_user`+`ban_user` both set
- message templates: reject empty string, reject unknown key (shouldn't happen w/ choice-constrained option but double check)

## Registration/deployment notes

- Guild command registration (not global) — matches single-guild-only design already in CLAUDE.md
- Re-register on every startup (idempotent, disgo overwrites by name) so command tree changes ship w/ each deploy, no manual step
- No new env vars needed — reuse existing `GUILD_ID`

## Testing

- Unit tests for new `configStore` setters (round-trip: set → Load() → assert field) in existing `_test.go` pattern, in-memory sqlite (`:memory:` or temp file, whichever `db_test.go`/existing tests already use — check `main_test.go` for pattern first)
- Manual test via `/config view` after each setter command in a dev guild

## Open questions for user before implementing

1. One `/config` command w/ subcommand groups, or split into `/config-set`, `/config-view`, `/config-message` etc.? (plan assumes single grouped command)
2. Should `/config view` show full message templates inline, or just keys (templates can be long, may want a separate `/config message get <key>` to avoid embed clutter)?
3. Any other role/permission model wanted beyond Discord's built-in `ManageGuild`, e.g. a custom "bot admin" role stored in config?
