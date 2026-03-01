# How the bot works

This document describes the runtime behavior and data flow implemented in `main.go`.

## Overview

When a non-bot message is posted in the configured guild, the bot:

1. Normalizes the message content (`strings.ToLower` + `TrimSpace`). Empty or embed-only messages are ignored.
2. Loads the author's recent messages from Valkey using a list key `user:<userID>:messages`.
3. Computes similarity between the new message and each previous message using Levenshtein distance, converted to a 0.0–1.0 ratio:

   similarity = 1 - (levenshteinDistance / max(len(a), len(b)))

4. If at least one previous message meets `SIMILARITY_MIN`, the bot increments a rolling counter stored at `user:<userID>:similar_count`.
   - The counter key is created atomically via `INCR` and given a TTL of `WINDOW_SECONDS` on first increment.
   - This produces a sliding window: repeated similar messages within `WINDOW_SECONDS` seconds increment the same counter.
5. If the counter reaches or exceeds `ALERT_AFTER`, the bot deletes the counter key and posts an alert message to the configured `ALERT_CHANNEL_ID`.

## Storage keys and invariants

- `user:<id>:messages` — list (LPUSH) of recent normalized messages for the user. The list is trimmed to `maxCached` (default 30) and given a TTL of `cacheTTL` (default 3600s) on each write.
- `user:<id>:similar_count` — incrementing integer used to count similar messages inside the rolling window. TTL equals `WINDOW_SECONDS`.

## Why Valkey and approach

- Valkey (via `valkey-go`) gives a simple, in-memory K/V API with list and TTL semantics like Redis. It is used for short-lived caches and counters.
- Using Levenshtein-based similarity tolerates small edits and punctuation differences while still recognizing repeated content.
- The separate per-user counter with a TTL implements a rolling window rather than fixed intervals.

## Configurable parameters

- `SIMILARITY_MIN` — similarity threshold (float). Default `0.85`.
- `ALERT_AFTER` — number of similar messages required to alert. Default `3`.
- `WINDOW_SECONDS` — seconds for the rolling window. Default `300`.
- `maxCached` and `cacheTTL` are defined in code: `maxCached = 30`, `cacheTTL = 3600`.

## Extensibility notes

- Excluding channels: `EXCLUDED_CHANNEL_IDS` can be set to skip specific channels.
- The similarity function and thresholds can be tuned or replaced (e.g., fuzzy hashing, NLP embeddings) for different tolerance.
- Alert formatting and destination can be modified to include more context, logging, or webhook integration.
