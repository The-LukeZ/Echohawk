# Echohawk — Discord similarity/spam detector

Echohawk is a small Discord bot written in Go that detects repeated or highly similar messages from the same user and sends an alert to a configured channel. It is intended to help moderators identify spammy behaviour by measuring message similarity and counting events in a rolling time window.

Key features

- Lightweight: uses Valkey (valkey-go) for a tiny, in-memory key/value store with TTLs.
- Configurable: similarity threshold, alert threshold and time window are environment-configurable.
- Discord-native: uses privileged message content intent and sends alerts as messages to a configured channel.

This repository contains the bot source (`main.go`) and Docker/compose artifacts for deployment.
