# Setup and configuration

## Prerequisites

- A Discord bot application with `Message Content` intent enabled in the Developer Portal

### Building and running directly

- Go 1.20+
- A running Valkey instance reachable from the bot

### Docker

- Docker and Docker Compose installed  
  Valkey is included as a service in the `docker-compose.yml`.

## Environment variables

Create an `.env` file (the project uses `github.com/joho/godotenv`) and set these values.

Bootstrap/secrets - always read from `.env`, every run:

- `BOT_TOKEN` - Discord bot token
- `GUILD_ID` - the Snowflake ID of the guild (server) to monitor
- `VALKEY_ADDR` - address(es) of your Valkey instance (e.g. `127.0.0.1:6379`)
  - When running via Docker Compose, you need to use `valkey:6379` as the address
- `SQLITE_PATH` - path to the SQLite config database (optional, default: `./data/echohawk.db`). Created automatically on first run - no separate init step or migration tool needed.  
  `./data/echohawk.db` is the default path when running via Docker Compose, since the `data` directory is mounted as a volume.

Runtime config - **deprecated**. These exist only as a legacy migration path: read once to seed the SQLite `config` table on its very first run (when that table is empty), then ignored forever after. Don't rely on them for new deployments - use the `/config` slash command (see `docs/02_HOW_IT_WORKS.md`) to set these at runtime instead:

- `ALERT_CHANNEL_ID` - Snowflake ID of the channel where alerts should be posted
- `EXCLUDED_CHANNEL_IDS` - comma-separated channel/category/thread IDs to ignore (optional). Excluding a channel or category also excludes its threads and other children, since exclusion checks walk up the parent chain (thread -> parent channel -> category).
- `SIMILARITY_MIN` - float (0.0–1.0) threshold for similarity (optional, default: `0.85`)
- `ALERT_AFTER` - number of similar messages to trigger an alert (optional, default: `3`)
- `WINDOW_SECONDS` - rolling window in seconds for counting similar messages (optional, default: `300`)
- `ACTIONS` - comma-separated list of automated actions to take when spam is detected (optional, default: none - alert only). See table below.
- `TIMEOUT_DURATION` - how long in seconds to time out a user when `timeout_user` is in `ACTIONS` (optional, default: `300`)
- `UNIFY_ATTACHMENTS` - when `true`, Discord CDN attachment/media links are collapsed to one placeholder before comparison, so spam using a different image link per message still matches (optional, default: `false`)

Message templates (`alert`, `dm_user`, `timeout_reason`, `kick_reason`, `ban_reason`) have no env var - they're seeded with built-in defaults on first run and edited only via `/config message set <key>`.

### Available actions

| Value          | Behaviour                                                                         |
| -------------- | --------------------------------------------------------------------------------- |
| `alert`        | Posts an alert message to `ALERT_CHANNEL_ID` with details about the detected spam |
| `delete_all`   | Deletes the triggering message and all stored similar messages from the same user |
| `delete_last`  | Deletes only the triggering message                                               |
| `dm_user`      | Sends the user a direct message warning them about the spam detection             |
| `timeout_user` | Applies a server timeout for `TIMEOUT_DURATION` seconds                           |
| `kick_user`    | Kicks the user from the guild                                                     |
| `ban_user`     | Bans the user from the guild                                                      |

Multiple actions can be combined freely, e.g. `ACTIONS=delete_all,dm_user,timeout_user`. An alert is always posted to `ALERT_CHANNEL_ID` regardless of what actions are configured.

Mind, that you can only use `delete_all` **OR** `delete_last` - not both at the same time. Same applies to `kick_user` and `ban_user`.

#### Example `.env`

_copy via `cp .env.example .env` and edit values_

```bash
BOT_TOKEN=your_bot_token_here
GUILD_ID=123456789012345678
VALKEY_ADDR=valkey:6379
SQLITE_PATH=./data/echohawk.db

# deprecated: seed values only, used once on first run - edit via /config after that
ALERT_CHANNEL_ID=987654321098765432
EXCLUDED_CHANNEL_IDS=111111111111111111,222222222222222222
SIMILARITY_MIN=0.85
ALERT_AFTER=3
WINDOW_SECONDS=300
ACTIONS=delete_last,dm_user,timeout_user
TIMEOUT_DURATION=3600
```

## Build & run (local)

1. Download dependencies and build:

```bash
go mod tidy
go build -o echohawk ./cmd/echohawk
```

Note that valkey must be running and reachable at the address specified in `VALKEY_ADDR` for the bot to function correctly.

2. Run the bot (loads `.env` automatically via godotenv):

```bash
./echohawk
```

Run with `go run` for quick testing:

```bash
go run ./cmd/echohawk
```

### Docker

This repo includes a `Dockerfile` and `docker-compose.yml`. Build and run via Docker if you prefer containerized deployment:

```bash
docker compose up -d --build
```

## Permissions and intents

Ensure your bot has the `Message Content` intent enabled in the Developer Portal and the bot is invited to the target guild with the correct scopes and permissions.

For moderation actions (`timeout_user`, `kick_user`, `ban_user`, `delete_all`, `delete_last`) the bot also needs the corresponding Discord permissions: `Moderate Members`, `Kick Members`, `Ban Members`, and `Manage Messages`.

## How to exclude channels

There are two ways to exclude channels from monitoring:

1. Run `/config excluded_channels add <channel>` (or `remove`) at runtime - the recommended way, takes effect immediately without a restart. `EXCLUDED_CHANNEL_IDS` in `.env` only seeds the initial DB value on first run.
2. Deny the bot's `View Channel` permission for specific channels (even whole categories!) in your server settings. The bot will simply not receive message events for channels it cannot view.

Excluding users based on roles or permissions is not currently supported but could be implemented in a future update.
