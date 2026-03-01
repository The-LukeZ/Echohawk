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

## Why Go?

I always wanted to do a bot that does some kind of spam detection, but didn't find the time for it. Then someone asked if there are any public bots that do this - and the simple answer is "not really".
So I offered to finally build it, and decided to do it in Go because the person's requirement was a server where with a very high volume of messages per second, usually up to 70k messages per day. Go's simplicity, performance and efficiency with concurrency seemed like a good fit for this use case, especially since the bot's only task is to process messages and interact with Valkey, which should be pretty lightweight (which was also a requirement from the server manager). I also wanted to learn Go for a while, so this seemed like a good opportunity to do both at the same time.
