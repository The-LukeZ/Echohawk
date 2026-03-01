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

	"github.com/agnivade/levenshtein"
	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/snowflake/v2"
	"github.com/joho/godotenv"
	valkey "github.com/valkey-io/valkey-go"
)

const (
	maxCached = 30   // max messages stored per user
	cacheTTL  = 3600 // key expires after 1 hour of inactivity
)

var (
	similarityMin float64 = 0.85
	alertAfter    int64   = 3
	windowSeconds int64   = 300
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
}

// Checker holds shared state — the Valkey client and alert channel ID.
// Structs replace classes in Go: no constructor, just initialize the fields.
type Checker struct {
	vk               valkey.Client
	alertChannel     snowflake.ID
	excludedChannels map[snowflake.ID]bool
	similarityMin    float64
	alertAfter       int64
	windowSeconds    int64
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
	guildIdStr := os.Getenv("GUILD_ID")

	if c.excludedChannels[msg.ChannelID] || msg.Author.Bot {
		return
	}

	if guildIdStr == "" {
		return // ignore DMs and messages from other servers
	}

	guildId, err := snowflake.Parse(guildIdStr)
	if err != nil {
		fmt.Println("invalid GUILD_ID in .env:", err)
		return
	}

	if *msg.GuildID != guildId {
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

	// --- Step 2: Count how many previous messages are too similar ---
	similarCount := 0
	for _, p := range prev {
		if similarity(content, p) >= similarityMin {
			similarCount++
		}
	}

	// --- Step 3: Store the new message (3 commands, auto-pipelined by valkey-go) ---
	// DoMulti sends all commands in one round-trip automatically.
	// LPUSH → prepend, LTRIM → keep only last 30, EXPIRE → reset the 1h TTL.
	c.vk.DoMulti(ctx,
		c.vk.B().Lpush().Key(msgKey).Element(content).Build(),
		c.vk.B().Ltrim().Key(msgKey).Start(0).Stop(int64(maxCached-1)).Build(),
		c.vk.B().Expire().Key(msgKey).Seconds(cacheTTL).Build(),
	)

	if similarCount == 0 {
		return
	}

	// --- Step 4: Increment the rolling 5-minute similarity counter ---
	// INCR is atomic, so concurrent messages won't cause a race condition.
	count, err := c.vk.Do(ctx,
		c.vk.B().Incr().Key(counterKey).Build(),
	).AsInt64()
	if err != nil {
		fmt.Println("valkey incr error:", err)
		return
	}

	// Set the TTL only on first increment (count == 1 means key was just created).
	// This starts the 5-minute window from the first similar message.
	if count == 1 {
		c.vk.Do(ctx, c.vk.B().Expire().Key(counterKey).Seconds(c.windowSeconds).Build())
	}

	// --- Step 5: Alert if threshold exceeded, then reset the counter ---
	if count >= int64(alertAfter) {
		c.vk.Do(ctx, c.vk.B().Del().Key(counterKey).Build())

		alert := fmt.Sprintf(
			"⚠️ **Spam detected** — <@%s> sent %d similar messages in the last %d seconds.\nLatest message: `%s`",
			msg.Author.ID, count, c.windowSeconds, content,
		)

		_, err := e.Client().Rest.CreateMessage(
			c.alertChannel,
			discord.NewMessageCreate().WithContent(alert),
		)
		if err != nil {
			fmt.Println("failed to send alert:", err)
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
		similarityMin:    similarityMin,
		alertAfter:       alertAfter,
		windowSeconds:    windowSeconds,
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
