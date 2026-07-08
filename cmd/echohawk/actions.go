package main

import (
	"fmt"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/omit"
	"github.com/disgoorg/snowflake/v2"
)

// executeActions sends the alert and runs every action listed in ACTIONS.
// It is called only when the spam threshold is exceeded.
func (c *Checker) executeActions(e *events.MessageCreate, count int64, content string, similarMsgs []cachedMsg) {
	msg := e.Message
	rest := e.Client().Rest

	for action := range c.actions {
		switch action {

		case "alert":
			alert := fmt.Sprintf(
				"⚠️ **Spam detected** - <@%s> sent %d similar messages in the last %d seconds.\nLatest message: `%s`",
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
