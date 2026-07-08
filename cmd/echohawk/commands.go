package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/omit"
	"github.com/disgoorg/snowflake/v2"
)

// validActions is the set of action names accepted by /config actions add|remove,
// mirroring the switch in executeActions (actions.go).
var validActions = []string{"alert", "delete_all", "delete_last", "dm_user", "timeout_user", "kick_user", "ban_user"}

// validMessageKeys is the set of template keys accepted by /config message set|reset,
// mirroring the messages table seeded in db.go.
var validMessageKeys = []string{"alert", "dm_user", "timeout_reason", "kick_reason", "ban_reason"}

func actionChoices() []discord.ApplicationCommandOptionChoiceString {
	choices := make([]discord.ApplicationCommandOptionChoiceString, len(validActions))
	for i, a := range validActions {
		choices[i] = discord.ApplicationCommandOptionChoiceString{Name: a, Value: a}
	}
	return choices
}

func messageKeyChoices() []discord.ApplicationCommandOptionChoiceString {
	choices := make([]discord.ApplicationCommandOptionChoiceString, len(validMessageKeys))
	for i, k := range validMessageKeys {
		choices[i] = discord.ApplicationCommandOptionChoiceString{Name: k, Value: k}
	}
	return choices
}

// alertChannelTypes restricts the alert_channel option to text channels and
// their threads - the only channel kinds executeActions can post an alert into.
var alertChannelTypes = []discord.ChannelType{
	discord.ChannelTypeGuildText,
	discord.ChannelTypeGuildNews,
	discord.ChannelTypeGuildNewsThread,
	discord.ChannelTypeGuildPublicThread,
	discord.ChannelTypeGuildPrivateThread,
}

// commandDefs is the full /config command tree, registered per-guild in main().
var commandDefs = []discord.ApplicationCommandCreate{
	discord.SlashCommandCreate{
		Name:        "config",
		Description: "View and edit Echohawk's runtime configuration",
		// Belt-and-suspenders: Discord enforces this at the gateway/REST level,
		// and HandleConfigCommand re-checks the invoking member's permissions too.
		DefaultMemberPermissions: omit.NewPtr(discord.PermissionManageGuild),
		Options: []discord.ApplicationCommandOption{
			discord.ApplicationCommandOptionSubCommand{
				Name:        "view",
				Description: "Show the current configuration",
			},
			discord.ApplicationCommandOptionSubCommand{
				Name:        "set",
				Description: "Update one or more config fields",
				Options: []discord.ApplicationCommandOption{
					discord.ApplicationCommandOptionFloat{
						Name:        "similarity_min",
						Description: "Similarity threshold (0-1) above which messages count as duplicates",
						MinValue:    omit.Ptr(0.0),
						MaxValue:    omit.Ptr(1.0),
					},
					discord.ApplicationCommandOptionInt{
						Name:        "alert_after",
						Description: "Number of similar messages before taking action",
						MinValue:    omit.Ptr(1),
					},
					discord.ApplicationCommandOptionInt{
						Name:        "window_seconds",
						Description: "Rolling window (seconds) the similar-message count resets after",
						MinValue:    omit.Ptr(1),
					},
					discord.ApplicationCommandOptionInt{
						Name:        "timeout_duration",
						Description: "Timeout duration in seconds (max 2419200 = 28 days)",
						MinValue:    omit.Ptr(0),
						MaxValue:    omit.Ptr(2419200),
					},
					discord.ApplicationCommandOptionBool{
						Name:        "unify_attachments",
						Description: "Treat different attachment links as identical when comparing messages",
					},
					discord.ApplicationCommandOptionChannel{
						Name:         "alert_channel",
						Description:  "Channel alerts are posted to",
						ChannelTypes: alertChannelTypes,
					},
				},
			},
			discord.ApplicationCommandOptionSubCommandGroup{
				Name:        "actions",
				Description: "Enable or disable moderation actions",
				Options: []discord.ApplicationCommandOptionSubCommand{
					{
						Name:        "add",
						Description: "Enable an action",
						Options: []discord.ApplicationCommandOption{
							discord.ApplicationCommandOptionString{
								Name:        "action",
								Description: "Action to enable",
								Required:    true,
								Choices:     actionChoices(),
							},
						},
					},
					{
						Name:        "remove",
						Description: "Disable an action",
						Options: []discord.ApplicationCommandOption{
							discord.ApplicationCommandOptionString{
								Name:        "action",
								Description: "Action to disable",
								Required:    true,
								Choices:     actionChoices(),
							},
						},
					},
				},
			},
			discord.ApplicationCommandOptionSubCommandGroup{
				Name:        "excluded_channels",
				Description: "Manage channels excluded from spam detection",
				Options: []discord.ApplicationCommandOptionSubCommand{
					{
						Name:        "add",
						Description: "Exclude a channel (and its threads) from spam detection",
						Options: []discord.ApplicationCommandOption{
							discord.ApplicationCommandOptionChannel{
								Name:        "channel",
								Description: "Channel to exclude",
								Required:    true,
							},
						},
					},
					{
						Name:        "remove",
						Description: "Re-include a previously excluded channel",
						Options: []discord.ApplicationCommandOption{
							discord.ApplicationCommandOptionChannel{
								Name:        "channel",
								Description: "Channel to re-include",
								Required:    true,
							},
						},
					},
				},
			},
			discord.ApplicationCommandOptionSubCommandGroup{
				Name:        "message",
				Description: "Manage customizable action message templates",
				Options: []discord.ApplicationCommandOptionSubCommand{
					{
						Name:        "set",
						Description: "Edit a message template in a popup with a multi-line text box",
						Options: []discord.ApplicationCommandOption{
							discord.ApplicationCommandOptionString{
								Name:        "key",
								Description: "Which template to edit",
								Required:    true,
								Choices:     messageKeyChoices(),
							},
						},
					},
					{
						Name:        "reset",
						Description: "Reset a message template to its default",
						Options: []discord.ApplicationCommandOption{
							discord.ApplicationCommandOptionString{
								Name:        "key",
								Description: "Which template to reset",
								Required:    true,
								Choices:     messageKeyChoices(),
							},
						},
					},
					{
						Name:        "get",
						Description: "Show the current text of a message template",
						Options: []discord.ApplicationCommandOption{
							discord.ApplicationCommandOptionString{
								Name:        "key",
								Description: "Which template to show",
								Required:    true,
								Choices:     messageKeyChoices(),
							},
						},
					},
				},
			},
		},
	},
}

// HandleConfigCommand dispatches /config subcommands. Registered as the
// ApplicationCommandInteractionCreate listener in main().
func (c *Checker) HandleConfigCommand(e *events.ApplicationCommandInteractionCreate) {
	data := e.SlashCommandInteractionData()
	if data.CommandName() != "config" {
		return
	}

	if e.GuildID() == nil || *e.GuildID() != c.guildID {
		c.replyError(e, "This command can only be used in the server Echohawk is configured for.")
		return
	}

	// Belt-and-suspenders: DefaultMemberPermissions already gates this at
	// Discord's level, but re-check here in case of API changes or
	// silently-configured overrides.
	member := e.Member()
	if member == nil || !member.Permissions.Has(discord.PermissionManageGuild) {
		c.replyError(e, "You need the Manage Server permission to use this command.")
		return
	}

	var (
		cfg *Config
		err error
	)

	switch {
	case data.SubCommandGroupName != nil && *data.SubCommandGroupName == "actions":
		cfg, err = c.handleActionsCommand(data)
	case data.SubCommandGroupName != nil && *data.SubCommandGroupName == "excluded_channels":
		cfg, err = c.handleExcludedChannelsCommand(data)
	case data.SubCommandGroupName != nil && *data.SubCommandGroupName == "message" && data.SubCommandName != nil && *data.SubCommandName == "get":
		c.replyMessageGet(e, data.String("key"))
		return
	case data.SubCommandGroupName != nil && *data.SubCommandGroupName == "message" && data.SubCommandName != nil && *data.SubCommandName == "set":
		c.openMessageModal(e, data.String("key"))
		return
	case data.SubCommandGroupName != nil && *data.SubCommandGroupName == "message":
		cfg, err = c.handleMessageResetCommand(data)
	case data.SubCommandName != nil && *data.SubCommandName == "view":
		c.replyView(e)
		return
	case data.SubCommandName != nil && *data.SubCommandName == "set":
		cfg, err = c.handleSetCommand(data)
	default:
		c.replyError(e, "Unknown subcommand.")
		return
	}

	if err != nil {
		c.replyError(e, err.Error())
		return
	}

	c.SetCfg(cfg)
	c.replySuccess(e, "Configuration updated.")
}

func (c *Checker) handleSetCommand(data discord.SlashCommandInteractionData) (*Config, error) {
	var cfg *Config
	var err error

	if v, ok := data.OptFloat("similarity_min"); ok {
		if v < 0 || v > 1 {
			return nil, fmt.Errorf("similarity_min must be between 0 and 1")
		}
		if cfg, err = c.store.SetSimilarityMin(v); err != nil {
			return nil, err
		}
	}
	if v, ok := data.OptInt("alert_after"); ok {
		if v < 1 {
			return nil, fmt.Errorf("alert_after must be at least 1")
		}
		if cfg, err = c.store.SetAlertAfter(int64(v)); err != nil {
			return nil, err
		}
	}
	if v, ok := data.OptInt("window_seconds"); ok {
		if v < 1 {
			return nil, fmt.Errorf("window_seconds must be at least 1")
		}
		if cfg, err = c.store.SetWindowSeconds(int64(v)); err != nil {
			return nil, err
		}
	}
	if v, ok := data.OptInt("timeout_duration"); ok {
		if v < 0 || v > 2419200 {
			return nil, fmt.Errorf("timeout_duration must be between 0 and 2419200 (28 days)")
		}
		if cfg, err = c.store.SetTimeoutDuration(int64(v)); err != nil {
			return nil, err
		}
	}
	if v, ok := data.OptBool("unify_attachments"); ok {
		if cfg, err = c.store.SetUnifyAttachments(v); err != nil {
			return nil, err
		}
	}
	if ch, ok := data.OptChannel("alert_channel"); ok {
		if cfg, err = c.store.SetAlertChannel(ch.ID); err != nil {
			return nil, err
		}
	}

	if cfg == nil {
		// Nothing was set - reload so the caller still gets a valid Config back.
		return c.store.Load()
	}
	return cfg, nil
}

func (c *Checker) handleActionsCommand(data discord.SlashCommandInteractionData) (*Config, error) {
	action := data.String("action")

	enabled := data.SubCommandName != nil && *data.SubCommandName == "add"
	if enabled {
		if (action == "delete_all" || action == "delete_last") && data.String("action") != "" {
			cfg, err := c.store.Load()
			if err != nil {
				return nil, err
			}
			if action == "delete_all" && cfg.Actions["delete_last"] {
				return nil, fmt.Errorf("delete_last is already enabled; remove it first (mutually exclusive with delete_all)")
			}
			if action == "delete_last" && cfg.Actions["delete_all"] {
				return nil, fmt.Errorf("delete_all is already enabled; remove it first (mutually exclusive with delete_last)")
			}
		}
		if action == "kick_user" || action == "ban_user" {
			cfg, err := c.store.Load()
			if err != nil {
				return nil, err
			}
			if action == "kick_user" && cfg.Actions["ban_user"] {
				return nil, fmt.Errorf("ban_user is already enabled; remove it first (mutually exclusive with kick_user)")
			}
			if action == "ban_user" && cfg.Actions["kick_user"] {
				return nil, fmt.Errorf("kick_user is already enabled; remove it first (mutually exclusive with ban_user)")
			}
		}
	}

	return c.store.SetAction(action, enabled)
}

func (c *Checker) handleExcludedChannelsCommand(data discord.SlashCommandInteractionData) (*Config, error) {
	ch := data.Channel("channel")
	excluded := data.SubCommandName != nil && *data.SubCommandName == "add"
	return c.store.SetExcludedChannel(ch.ID, excluded)
}

func (c *Checker) handleMessageResetCommand(data discord.SlashCommandInteractionData) (*Config, error) {
	key := data.String("key")
	def, ok := defaultMessages[key]
	if !ok {
		return nil, fmt.Errorf("unknown message key %q", key)
	}
	return c.store.SetMessage(key, def)
}

// messageModalCustomID and parseMessageModalCustomID round-trip the message
// key through the modal's custom_id, since a modal can't carry extra hidden
// state of its own - the key chosen in the "/config message set" option has
// to survive until the modal is submitted (a separate interaction).
const messageModalCustomIDPrefix = "config_message_set:"

func messageModalCustomID(key string) string {
	return messageModalCustomIDPrefix + key
}

func parseMessageModalCustomID(customID string) (key string, ok bool) {
	if !strings.HasPrefix(customID, messageModalCustomIDPrefix) {
		return "", false
	}
	return strings.TrimPrefix(customID, messageModalCustomIDPrefix), true
}

// openMessageModal responds to "/config message set <key>" with a popup
// containing a multi-line text box, pre-filled with the current template -
// this replaces the old "template" string option, which couldn't take
// literal newlines or show existing content for editing.
func (c *Checker) openMessageModal(e *events.ApplicationCommandInteractionCreate, key string) {
	if _, ok := defaultMessages[key]; !ok {
		c.replyError(e, fmt.Sprintf("unknown message key %q", key))
		return
	}

	current := c.Cfg().Messages[key]
	textInput := discord.NewParagraphTextInput("template").
		WithRequired(true).
		WithMaxLength(4000).
		WithValue(current).
		WithPlaceholder("Placeholders: {user_id} {count} {window} {content}")

	modal := discord.NewModalCreate(messageModalCustomID(key), "Edit message: "+key).
		AddLabel("Template", textInput)

	if err := e.Modal(modal); err != nil {
		fmt.Println("failed to open message modal:", err)
	}
}

// HandleConfigMessageModalSubmit handles the modal opened by openMessageModal.
// Registered as the ModalSubmitInteractionCreate listener in main().
func (c *Checker) HandleConfigMessageModalSubmit(e *events.ModalSubmitInteractionCreate) {
	key, ok := parseMessageModalCustomID(e.Data.CustomID)
	if !ok {
		return
	}

	if e.GuildID() == nil || *e.GuildID() != c.guildID {
		c.replyModalError(e, "This command can only be used in the server Echohawk is configured for.")
		return
	}
	member := e.Member()
	if member == nil || !member.Permissions.Has(discord.PermissionManageGuild) {
		c.replyModalError(e, "You need the Manage Server permission to use this command.")
		return
	}

	template := e.Data.Text("template")
	if strings.TrimSpace(template) == "" {
		c.replyModalError(e, "template cannot be empty")
		return
	}
	if unknown := unknownPlaceholders(template); len(unknown) > 0 {
		c.replyModalError(e, fmt.Sprintf("template contains unknown placeholder(s): %s (allowed: {user_id} {count} {window} {content})", strings.Join(unknown, ", ")))
		return
	}

	cfg, err := c.store.SetMessage(key, template)
	if err != nil {
		c.replyModalError(e, err.Error())
		return
	}

	c.SetCfg(cfg)
	_ = e.CreateMessage(discord.NewMessageCreate().WithContent("✅ Configuration updated.").WithEphemeral(true))
}

func (c *Checker) replyModalError(e *events.ModalSubmitInteractionCreate, msg string) {
	_ = e.CreateMessage(discord.NewMessageCreate().WithContent("❌ " + msg).WithEphemeral(true))
}

// unknownPlaceholders scans template for {...} tokens that aren't one of the
// placeholders renderMessage actually replaces, so a typo doesn't silently
// ship a template with a dangling literal "{foo}" in it.
func unknownPlaceholders(template string) []string {
	allowed := map[string]bool{
		"{user_id}": true,
		"{count}":   true,
		"{window}":  true,
		"{content}": true,
	}

	var unknown []string
	for i := 0; i < len(template); i++ {
		if template[i] != '{' {
			continue
		}
		end := strings.IndexByte(template[i:], '}')
		if end == -1 {
			break
		}
		token := template[i : i+end+1]
		if !allowed[token] {
			unknown = append(unknown, token)
		}
		i += end
	}
	return unknown
}

// replyMessageGet shows the full current text of a single message template -
// /config view only lists keys since templates can be long enough to clutter
// that embed.
func (c *Checker) replyMessageGet(e *events.ApplicationCommandInteractionCreate, key string) {
	if _, ok := defaultMessages[key]; !ok {
		c.replyError(e, fmt.Sprintf("unknown message key %q", key))
		return
	}

	template := c.Cfg().Messages[key]
	embed := discord.Embed{
		Title:       "message template: " + key,
		Description: "```\n" + escapeCodeBlock(template) + "\n```",
	}
	_ = e.CreateMessage(discord.NewMessageCreate().WithEmbeds(embed).WithEphemeral(true))
}

// escapeCodeBlock breaks up any ``` sequence in template so it can't close
// the surrounding code block early.
func escapeCodeBlock(template string) string {
	return strings.ReplaceAll(template, "```", "` ` `")
}

func (c *Checker) replyView(e *events.ApplicationCommandInteractionCreate) {
	cfg := c.Cfg()

	excluded := make([]string, 0, len(cfg.ExcludedChannels))
	for id := range cfg.ExcludedChannels {
		excluded = append(excluded, "<#"+id.String()+">")
	}
	actions := make([]string, 0, len(cfg.Actions))
	for a := range cfg.Actions {
		actions = append(actions, a)
	}

	embed := discord.Embed{
		Title: "Echohawk configuration",
		Fields: []discord.EmbedField{
			{Name: "similarity_min", Value: strconv.FormatFloat(cfg.SimilarityMin, 'f', -1, 64), Inline: omit.Ptr(true)},
			{Name: "alert_after", Value: strconv.FormatInt(cfg.AlertAfter, 10), Inline: omit.Ptr(true)},
			{Name: "window_seconds", Value: strconv.FormatInt(cfg.WindowSeconds, 10), Inline: omit.Ptr(true)},
			{Name: "timeout_duration", Value: strconv.FormatInt(cfg.TimeoutDuration, 10), Inline: omit.Ptr(true)},
			{Name: "unify_attachments", Value: strconv.FormatBool(cfg.UnifyAttachments), Inline: omit.Ptr(true)},
			{Name: "alert_channel", Value: channelMentionOrNone(cfg.AlertChannel), Inline: omit.Ptr(true)},
			{Name: "actions", Value: joinOrNone(actions)},
			{Name: "excluded_channels", Value: joinOrNone(excluded)},
			{Name: "message keys", Value: strings.Join(validMessageKeys, ", ")},
		},
	}

	_ = e.CreateMessage(discord.NewMessageCreate().WithEmbeds(embed).WithEphemeral(true))
}

func channelMentionOrNone(id snowflake.ID) string {
	if id == 0 {
		return "none"
	}
	return "<#" + id.String() + ">"
}

func joinOrNone(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}

func (c *Checker) replySuccess(e *events.ApplicationCommandInteractionCreate, msg string) {
	_ = e.CreateMessage(discord.NewMessageCreate().WithContent("✅ " + msg).WithEphemeral(true))
}

func (c *Checker) replyError(e *events.ApplicationCommandInteractionCreate, msg string) {
	_ = e.CreateMessage(discord.NewMessageCreate().WithContent("❌ " + msg).WithEphemeral(true))
}
