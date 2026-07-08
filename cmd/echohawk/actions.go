package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/omit"
	"github.com/disgoorg/snowflake/v2"
)

// renderMessage fills in a template from cfg.Messages with the placeholders
// {user_id}, {count}, {window} and {content}. Missing keys render as "".
func renderMessage(cfg *Config, key string, msg discord.Message, count int64, content string) string {
	replacer := strings.NewReplacer(
		"{user_id}", msg.Author.ID.String(),
		"{count}", strconv.FormatInt(count, 10),
		"{window}", strconv.FormatInt(cfg.WindowSeconds, 10),
		"{content}", content,
	)
	return replacer.Replace(cfg.Messages[key])
}

// executeActions sends the alert and runs every action listed in ACTIONS.
// It is called only when the spam threshold is exceeded.
func (c *Checker) executeActions(e *events.MessageCreate, cfg *Config, count int64, content string, similarMsgs []cachedMsg) {
	msg := e.Message
	r := e.Client().Rest

	for action := range cfg.Actions {
		switch action {

		case "alert":
			alert := renderMessage(cfg, "alert", msg, count, content)
			if _, err := r.CreateMessage(cfg.AlertChannel, discord.NewMessageCreate().WithContent(alert)); err != nil {
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
				_ = r.BulkDeleteMessages(msg.ChannelID, toDelete)
			}

		case "delete_last":
			_ = r.DeleteMessage(msg.ChannelID, msg.ID)

		case "dm_user":
			dmChannel, err := r.CreateDMChannel(msg.Author.ID)
			if err == nil {
				dm := renderMessage(cfg, "dm_user", msg, count, content)
				_, _ = r.CreateMessage(dmChannel.ID(), discord.NewMessageCreate().WithContent(dm))
			}

		case "timeout_user":
			until := time.Now().Add(time.Duration(cfg.TimeoutDuration) * time.Second)
			reason := renderMessage(cfg, "timeout_reason", msg, count, content)
			_, _ = r.UpdateMember(c.guildID, msg.Author.ID, discord.MemberUpdate{
				CommunicationDisabledUntil: omit.NewPtr(until),
			}, rest.WithReason(reason))

		case "kick_user":
			reason := renderMessage(cfg, "kick_reason", msg, count, content)
			_ = r.RemoveMember(c.guildID, msg.Author.ID, rest.WithReason(reason))

		case "ban_user":
			reason := renderMessage(cfg, "ban_reason", msg, count, content)
			_ = r.AddBan(c.guildID, msg.Author.ID, 0, rest.WithReason(reason))
		}
	}
}
