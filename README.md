# Echohawk

Echohawk is a small Discord bot written in Go that detects repeated or highly similar messages from the same user and sends an alert to a configured channel, optionally taking automated moderation actions. It is intended to help moderators identify and respond to spammy behaviour by measuring message similarity and counting events in a rolling time window.

> [!CAUTION]
> This is my first Go project and still in development. I don't think it's stable yet, but feel free to check it out and give feedback.

## Key features

- Lightweight: uses Valkey (valkey-go) for a tiny, in-memory key/value store with TTLs.
- Configurable: similarity threshold, alert threshold, time window and moderation actions are all environment-configurable.
- Automated actions: when spam is detected the bot can delete messages, DM the user, apply a timeout, kick, or ban — individually or in any combination.
- Discord-native: uses privileged message content intent and sends alerts as messages to a configured channel.

This repository contains the bot source (`main.go`) and Docker/compose artifacts for deployment.

For further docs, check [the docs pages](./docs).
