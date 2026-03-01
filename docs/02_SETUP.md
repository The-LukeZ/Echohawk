# Setup and configuration

Prerequisites

- Go 1.20+ (or appropriate Go toolchain installed)
- A running Valkey instance reachable from the bot
- A Discord bot application with `Message Content` intent enabled in the Developer Portal

Environment variables

Create a `.env` file (the project uses `github.com/joho/godotenv`) and set these values:

- `BOT_TOKEN` — Discord bot token
- `GUILD_ID` — the Snowflake ID of the guild (server) to monitor
- `ALERT_CHANNEL_ID` — Snowflake ID of the channel where alerts should be posted
- `VALKEY_ADDR` — address(es) of your Valkey instance (e.g. `127.0.0.1:7000`)
- `EXCLUDED_CHANNEL_IDS` — comma-separated channel IDs to ignore (optional)
- `SIMILARITY_MIN` — float (0.0–1.0) threshold for similarity (optional, default: `0.85`)
- `ALERT_AFTER` — number of similar messages to trigger an alert (optional, default: `3`)
- `WINDOW_SECONDS` — rolling window in seconds for counting similar messages (optional, default: `300`)
- `ACTIONS` — comma-separated list of automated actions to take when spam is detected (optional, default: none — alert only). See table below.
- `TIMEOUT_DURATION` — how long in seconds to time out a user when `timeout_user` is in `ACTIONS` (optional, default: `300`)

Available actions

| Value          | Behaviour                                                                         |
| -------------- | --------------------------------------------------------------------------------- |
| `delete_all`   | Deletes the triggering message and all stored similar messages from the same user |
| `delete_last`  | Deletes only the triggering message                                               |
| `dm_user`      | Sends the user a direct message warning them about the spam detection             |
| `timeout_user` | Applies a server timeout for `TIMEOUT_DURATION` seconds                           |
| `kick_user`    | Kicks the user from the guild                                                     |
| `ban_user`     | Bans the user from the guild                                                      |

Multiple actions can be combined freely, e.g. `ACTIONS=delete_all,dm_user,timeout_user`. An alert is always posted to `ALERT_CHANNEL_ID` regardless of what actions are configured.

Mind, that you can only use `delete_all` **OR** `delete_last` — not both at the same time.

Example `.env`

```
BOT_TOKEN=your_bot_token_here
GUILD_ID=123456789012345678
ALERT_CHANNEL_ID=987654321098765432
VALKEY_ADDR=127.0.0.1:7000
EXCLUDED_CHANNEL_IDS=111111111111111111,222222222222222222
SIMILARITY_MIN=0.85
ALERT_AFTER=3
WINDOW_SECONDS=300
ACTIONS=delete_last,dm_user
TIMEOUT_DURATION=300
```

Build & run (local)

1. Download dependencies and build:

```
go mod tidy
go build -o echohawk ./
```

2. Run the bot (loads `.env` automatically via godotenv):

```
./echohawk
```

Run with `go run` for quick testing:

```
go run main.go
```

Docker

This repo includes a `Dockerfile` and `docker-compose.yml` (if present). Build and run via Docker if you prefer containerized deployment:

```
docker build -t echohawk .
docker run --env-file .env echohawk
```

Permissions and intents

Ensure your bot has the `Guild Messages` and `Message Content` intents enabled in the Discord Developer Portal and the bot is invited to the target guild with the correct scopes and permissions.

For moderation actions (`timeout_user`, `kick_user`, `ban_user`, `delete_all`, `delete_last`) the bot also needs the corresponding Discord permissions: `Moderate Members`, `Kick Members`, `Ban Members`, and `Manage Messages`.
