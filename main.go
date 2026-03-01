package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/agnivade/levenshtein"
	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/omit"
	"github.com/disgoorg/snowflake/v2"
	"github.com/joho/godotenv"
	valkey "github.com/valkey-io/valkey-go"
)

const (
	maxCached = 30   // max messages stored per user
	cacheTTL  = 3600 // key expires after 1 hour of inactivity
)

var (
	similarityMin   float64         = 0.85
	alertAfter      int64           = 3
	windowSeconds   int64           = 300
	timeoutDuration int64           = 300 // seconds to timeout a user
	actions         map[string]bool       // set of actions to execute on spam detection
)

func init() {
	if v := os.Getenv("SIMILARITY_MIN"); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			similarityMin = parsed
		}
	}

	if v := os.Getenv("ALERT_AFTER"); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			alertAfter = parsed
		}
	}

	if v := os.Getenv("WINDOW_SECONDS"); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			windowSeconds = parsed
		}
	}

	if v := os.Getenv("TIMEOUT_DURATION"); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			timeoutDuration = parsed
		}
	}

	// Parse ACTIONS as a comma-separated list, e.g. "delete_last,dm_user,timeout_user".
	// Valid values: delete_all, delete_last, dm_user, timeout_user, kick_user, ban_user
	actions = map[string]bool{}
	if v := os.Getenv("ACTIONS"); v != "" {
		for _, a := range strings.Split(v, ",") {
			actions[strings.TrimSpace(a)] = true
		}
	}
}

// Checker holds shared state — the Valkey client and alert channel ID.
// Structs replace classes in Go: no constructor, just initialize the fields.
type Checker struct {
	vk               valkey.Client
	alertChannel     snowflake.ID
	excludedChannels map[snowflake.ID]bool
	guildID          snowflake.ID
	actions          map[string]bool
	similarityMin    float64
	alertAfter       int64
	windowSeconds    int64
	timeoutDuration  int64
}

// cachedMsg holds a stored message's location and normalized content,
// parsed back out of the Valkey list entry.
type cachedMsg struct {
	channelID string
	messageID string
	content   string
}

// formatEntry encodes channelID, messageID and content into a single string for Valkey storage.
func formatEntry(channelID, messageID, content string) string {
	return channelID + "|" + messageID + "|" + content
}

// parseEntry decodes a stored Valkey entry. Entries without embedded IDs (legacy/test data)
// return empty strings for channelID and messageID.
func parseEntry(entry string) cachedMsg {
	parts := strings.SplitN(entry, "|", 3)
	if len(parts) == 3 {
		return cachedMsg{channelID: parts[0], messageID: parts[1], content: parts[2]}
	}
	return cachedMsg{content: entry} // legacy format fallback
}

// normalize strips noise before comparing so "Hello!" and "hello" count as the same.
func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// similarity returns a 0.0–1.0 ratio so message length doesn't skew results.
// A raw distance of 3 means very different things in a 5-char vs 200-char message.
func similarity(a, b string) float64 {
	if a == b {
		return 1.0
	}
	maxLen := math.Max(float64(len([]rune(a))), float64(len([]rune(b))))
	if maxLen == 0 {
		return 1.0
	}
	dist := levenshtein.ComputeDistance(a, b)
	return 1.0 - float64(dist)/maxLen
}

// HandleMessage is the core logic, called for every non-bot message.
// The method receiver (c *Checker) is Go's way of attaching functions to a struct —
// like a class method, but declared separately. c is equivalent to `this` in TS.
func (c *Checker) HandleMessage(e *events.MessageCreate) {
	ctx := context.Background()
	msg := e.Message

	if c.excludedChannels[msg.ChannelID] || msg.Author.Bot {
		return
	}

	if msg.GuildID == nil || *msg.GuildID != c.guildID {
		return // ignore DMs and messages from other servers
	}

	content := normalize(msg.Content)
	if content == "" {
		return // skip embeds-only or empty messages
	}

	println("Received message:", content)

	userID := msg.Author.ID.String()
	msgKey := fmt.Sprintf("user:%s:messages", userID)
	counterKey := fmt.Sprintf("user:%s:similar_count", userID)

	// --- Step 1: Fetch the user's last N messages from Valkey ---
	// .Do() sends a single command. The builder pattern (B().Lrange()...) is
	// valkey-go's typed command builder — no raw string commands.
	prev, err := c.vk.Do(ctx,
		c.vk.B().Lrange().Key(msgKey).Start(0).Stop(-1).Build(),
	).AsStrSlice()

	// IsValkeyNil means the key doesn't exist yet — not a real error.
	if err != nil && !valkey.IsValkeyNil(err) {
		fmt.Println("valkey fetch error:", err)
		return
	}

	// --- Step 2: Find similar previous messages and collect their IDs for potential deletion ---
	var similarMsgs []cachedMsg
	for _, raw := range prev {
		cached := parseEntry(raw)
		if similarity(content, cached.content) >= c.similarityMin {
			similarMsgs = append(similarMsgs, cached)
		}
	}

	// --- Step 3: Store the new message with its channel/message IDs ---
	// Format: channelID|messageID|content — lets delete_all recover the IDs later.
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
		c.vk.Do(ctx, c.vk.B().Expire().Key(counterKey).Seconds(c.windowSeconds).Build())
	}

	// --- Step 5: Execute configured actions when threshold exceeded, then reset counter ---
	if count >= c.alertAfter {
		c.vk.Do(ctx, c.vk.B().Del().Key(counterKey).Build())
		c.executeActions(e, count, content, similarMsgs)
	}
}

// executeActions sends the alert and runs every action listed in ACTIONS.
// It is called only when the spam threshold is exceeded.
func (c *Checker) executeActions(e *events.MessageCreate, count int64, content string, similarMsgs []cachedMsg) {
	msg := e.Message
	rest := e.Client().Rest

	for action := range c.actions {
		switch action {

		case "alert":
			alert := fmt.Sprintf(
				"⚠️ **Spam detected** — <@%s> sent %d similar messages in the last %d seconds.\nLatest message: `%s`",
				msg.Author.ID, count, c.windowSeconds, content,
			)
			if _, err := rest.CreateMessage(c.alertChannel, discord.NewMessageCreate().WithContent(alert)); err != nil {
				fmt.Println("failed to send alert:", err)
			}

		case "delete_all":
			// make array with all ids to delete and then bulk delete with rest.BulkDeleteMessages(channelID, []messageID)
			var toDelete []snowflake.ID = []snowflake.ID{msg.ID}
			for _, cached := range similarMsgs {
				if cached.channelID == "" || cached.messageID == "" {
					continue
				}
				mID, errM := snowflake.Parse(cached.messageID)
				if errM == nil {
					toDelete = append(toDelete, mID)
				}
			}
			if len(toDelete) > 0 {
				_ = rest.BulkDeleteMessages(msg.ChannelID, toDelete)
			}

		case "delete_last":
			_ = rest.DeleteMessage(msg.ChannelID, msg.ID)

		case "dm_user":
			dmChannel, err := rest.CreateDMChannel(msg.Author.ID)
			if err == nil {
				dm := "⚠️ Your messages in the server have been flagged for spam. Please avoid sending repetitive messages."
				_, _ = rest.CreateMessage(dmChannel.ID(), discord.NewMessageCreate().WithContent(dm))
			}

		case "timeout_user":
			until := time.Now().Add(time.Duration(c.timeoutDuration) * time.Second)
			_, _ = rest.UpdateMember(c.guildID, msg.Author.ID, discord.MemberUpdate{
				CommunicationDisabledUntil: omit.NewPtr(until),
			})

		case "kick_user":
			_ = rest.RemoveMember(c.guildID, msg.Author.ID)

		case "ban_user":
			_ = rest.AddBan(c.guildID, msg.Author.ID, 0)
		}
	}
}

func main() {
	if err := godotenv.Load(); err != nil {
		panic("Error loading .env file")
	}

	// Connect to Valkey. valkey-go handles connection pooling automatically.
	vk, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{os.Getenv("VALKEY_ADDR")},
	})
	if err != nil {
		panic(fmt.Sprintf("failed to connect to Valkey: %v", err))
	}
	defer vk.Close() // defer runs when main() exits — like `finally` in TS

	alertChannelID, err := snowflake.Parse(os.Getenv("ALERT_CHANNEL_ID"))
	if err != nil {
		panic("invalid ALERT_CHANNEL_ID in .env")
	}

	guildID, err := snowflake.Parse(os.Getenv("GUILD_ID"))
	if err != nil {
		panic("invalid GUILD_ID in .env")
	}

	excludedChannels := map[snowflake.ID]bool{}
	if v := os.Getenv("EXCLUDED_CHANNEL_IDS"); v != "" {
		for raw := range strings.SplitSeq(v, ",") {
			if id, err := snowflake.Parse(strings.TrimSpace(raw)); err == nil {
				excludedChannels[id] = true
			}
		}
	}

	checker := &Checker{ // & means "give me a pointer to this struct"
		vk:               vk,
		alertChannel:     alertChannelID,
		excludedChannels: excludedChannels,
		guildID:          guildID,
		actions:          actions,
		similarityMin:    similarityMin,
		alertAfter:       alertAfter,
		windowSeconds:    windowSeconds,
		timeoutDuration:  timeoutDuration,
	}

	client, err := disgo.New(
		os.Getenv("BOT_TOKEN"),
		bot.WithGatewayConfigOpts(
			gateway.WithIntents(
				gateway.IntentGuilds,
				gateway.IntentGuildMessages,
				gateway.IntentMessageContent, // privileged — enable in Dev Portal
			),
		),
		bot.WithEventListenerFunc(func(e *events.Ready) {
			fmt.Printf("Logged in as %s\n", e.User.Username+"#"+e.User.Discriminator)
		}),
		// Pass the method as a function value with checker.HandleMessage
		bot.WithEventListenerFunc(checker.HandleMessage),
	)
	if err != nil {
		panic(err)
	}

	if err = client.OpenGateway(context.TODO()); err != nil {
		panic(err)
	}

	s := make(chan os.Signal, 1)
	signal.Notify(s, syscall.SIGINT, syscall.SIGTERM)
	<-s
}
