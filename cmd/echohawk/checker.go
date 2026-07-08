package main

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/disgoorg/disgo/cache"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
	valkey "github.com/valkey-io/valkey-go"
)

// Checker holds shared state - the Valkey client and the runtime config
// loaded from SQLite. Structs replace classes in Go: no constructor, just
// initialize the fields.
//
// cfg is an atomic.Pointer rather than a plain *Config so that /config
// commands can swap in a freshly-loaded Config without a bot restart, and
// without readers on the hot message path blocking on a lock.
type Checker struct {
	vk      valkey.Client
	guildID snowflake.ID
	store   *configStore
	cfg     atomic.Pointer[Config]
}

// Cfg returns the currently active Config. Safe to call concurrently with
// SetCfg.
func (c *Checker) Cfg() *Config {
	return c.cfg.Load()
}

// SetCfg swaps in a new Config, e.g. after a /config command mutates SQLite.
func (c *Checker) SetCfg(cfg *Config) {
	c.cfg.Store(cfg)
}

// isChannelExcluded checks channelID and walks up its parent chain (thread -> parent
// channel -> category) against excludedChannels, so excluding a channel or category also
// excludes its threads/children. Parent lookups use disgo's in-memory channel cache
// (populated from gateway events), so no extra REST calls or Valkey round-trips are needed.
// Channels only nest two levels deep on Discord (category -> channel -> thread), so the
// loop bound is just a safety net against unexpected data, not an expected case.
func (c *Checker) isChannelExcluded(caches cache.Caches, channelID snowflake.ID, excluded map[snowflake.ID]bool) bool {
	id := channelID
	for range 5 {
		if excluded[id] {
			return true
		}
		ch, ok := caches.Channel(id)
		if !ok {
			return false
		}
		parentID := ch.ParentID()
		if parentID == nil {
			return false
		}
		id = *parentID
	}
	return false
}

// HandleMessage is the core logic, called for every non-bot message.
// The method receiver (c *Checker) is Go's way of attaching functions to a struct -
// like a class method, but declared separately. c is equivalent to `this` in TS.
func (c *Checker) HandleMessage(e *events.MessageCreate) {
	ctx := context.Background()
	msg := e.Message
	cfg := c.Cfg()

	if msg.Author.Bot {
		return
	}

	if c.isChannelExcluded(e.Client().Caches, msg.ChannelID, cfg.ExcludedChannels) {
		return
	}

	if msg.GuildID == nil || *msg.GuildID != c.guildID {
		return // ignore DMs and messages from other servers
	}

	content := normalize(msg.Content, cfg.UnifyAttachments)
	if content == "" {
		return // skip embeds-only or empty messages
	}

	println("Received message:", content)

	userID := msg.Author.ID.String()
	msgKey := fmt.Sprintf("user:%s:messages", userID)
	counterKey := fmt.Sprintf("user:%s:similar_count", userID)

	// --- Step 1: Fetch the user's last N messages from Valkey ---
	// .Do() sends a single command. The builder pattern (B().Lrange()...) is
	// valkey-go's typed command builder - no raw string commands.
	prev, err := c.vk.Do(ctx,
		c.vk.B().Lrange().Key(msgKey).Start(0).Stop(-1).Build(),
	).AsStrSlice()

	// IsValkeyNil means the key doesn't exist yet - not a real error.
	if err != nil && !valkey.IsValkeyNil(err) {
		fmt.Println("valkey fetch error:", err)
		return
	}

	// --- Step 2: Find similar previous messages and collect their IDs for potential deletion ---
	var similarMsgs []cachedMsg
	for _, raw := range prev {
		cached := parseEntry(raw)
		if similarity(content, cached.content) >= cfg.SimilarityMin {
			similarMsgs = append(similarMsgs, cached)
		}
	}

	// --- Step 3: Store the new message with its channel/message IDs ---
	// Format: channelID|messageID|content - lets delete_all recover the IDs later.
	// LPUSH → prepend, LTRIM → keep only last 30, EXPIRE → reset the 1h TTL.
	entry := formatEntry(msg.ChannelID.String(), msg.ID.String(), content)
	c.vk.DoMulti(ctx,
		c.vk.B().Lpush().Key(msgKey).Element(entry).Build(),
		c.vk.B().Ltrim().Key(msgKey).Start(0).Stop(int64(maxCached-1)).Build(),
		c.vk.B().Expire().Key(msgKey).Seconds(cacheTTL).Build(),
	)

	if len(similarMsgs) == 0 {
		return
	}

	// --- Step 4: Increment the rolling similarity counter ---
	// INCR is atomic, so concurrent messages won't cause a race condition.
	count, err := c.vk.Do(ctx,
		c.vk.B().Incr().Key(counterKey).Build(),
	).AsInt64()
	if err != nil {
		fmt.Println("valkey incr error:", err)
		return
	}

	// Set the TTL only on first increment (count == 1 means key was just created).
	// This starts the window from the first similar message.
	if count == 1 {
		c.vk.Do(ctx, c.vk.B().Expire().Key(counterKey).Seconds(cfg.WindowSeconds).Build())
	}

	// --- Step 5: Execute configured actions when threshold exceeded, then reset counter ---
	if count >= cfg.AlertAfter {
		c.vk.Do(ctx, c.vk.B().Del().Key(counterKey).Build())
		c.executeActions(e, cfg, count, content, similarMsgs)
	}
}
