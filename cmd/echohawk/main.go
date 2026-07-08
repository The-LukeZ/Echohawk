package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/cache"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/snowflake/v2"
	"github.com/joho/godotenv"
	valkey "github.com/valkey-io/valkey-go"
)

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
	defer vk.Close() // defer runs when main() exits - like `finally` in TS

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
				gateway.IntentMessageContent, // privileged - enable in Dev Portal
			),
		),
		// FlagChannels caches channels/threads (populated via GUILD_CREATE / CHANNEL_CREATE
		// gateway events) so isChannelExcluded can walk the thread -> channel -> category
		// parent chain without extra REST calls.
		bot.WithCacheConfigOpts(
			cache.WithCaches(cache.FlagChannels),
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
