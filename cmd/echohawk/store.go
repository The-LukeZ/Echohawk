package main

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/disgoorg/snowflake/v2"
)

// Config is the full set of runtime-mutable settings, loaded from SQLite.
// Unlike the old package-level vars, this is per-instance so it can be
// reloaded and swapped without a restart.
type Config struct {
	AlertChannel     snowflake.ID
	ExcludedChannels map[snowflake.ID]bool
	SimilarityMin    float64
	AlertAfter       int64
	WindowSeconds    int64
	TimeoutDuration  int64
	UnifyAttachments bool
	Actions          map[string]bool
	Messages         map[string]string
}

// configStore loads Config from the SQLite database opened by openDB.
type configStore struct {
	db *sql.DB
}

func newConfigStore(db *sql.DB) *configStore {
	return &configStore{db: db}
}

func (s *configStore) Load() (*Config, error) {
	var (
		alertChannelIDStr string
		excludedCSV       string
		actionsCSV        string
		unifyAttachments  int
	)
	cfg := &Config{}

	row := s.db.QueryRow(`
		SELECT alert_channel_id, excluded_channel_ids, similarity_min,
		       alert_after, window_seconds, timeout_duration, unify_attachments, actions
		FROM config WHERE id = 1`)
	if err := row.Scan(
		&alertChannelIDStr, &excludedCSV, &cfg.SimilarityMin,
		&cfg.AlertAfter, &cfg.WindowSeconds, &cfg.TimeoutDuration, &unifyAttachments, &actionsCSV,
	); err != nil {
		return nil, fmt.Errorf("load config row: %w", err)
	}
	cfg.UnifyAttachments = unifyAttachments != 0

	if alertChannelIDStr != "" {
		alertChannelID, err := snowflake.Parse(alertChannelIDStr)
		if err != nil {
			return nil, fmt.Errorf("invalid alert_channel_id in config: %w", err)
		}
		cfg.AlertChannel = alertChannelID
	}

	cfg.ExcludedChannels = map[snowflake.ID]bool{}
	if excludedCSV != "" {
		for _, raw := range strings.Split(excludedCSV, ",") {
			if id, err := snowflake.Parse(strings.TrimSpace(raw)); err == nil {
				cfg.ExcludedChannels[id] = true
			}
		}
	}

	cfg.Actions = map[string]bool{}
	if actionsCSV != "" {
		for _, a := range strings.Split(actionsCSV, ",") {
			cfg.Actions[strings.TrimSpace(a)] = true
		}
	}

	cfg.Messages = map[string]string{}
	rows, err := s.db.Query(`SELECT key, template FROM messages`)
	if err != nil {
		return nil, fmt.Errorf("load messages: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, template string
		if err := rows.Scan(&key, &template); err != nil {
			return nil, fmt.Errorf("scan message row: %w", err)
		}
		cfg.Messages[key] = template
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// SetSimilarityMin updates the similarity_min field and returns the reloaded Config.
func (s *configStore) SetSimilarityMin(v float64) (*Config, error) {
	if _, err := s.db.Exec(`UPDATE config SET similarity_min = ? WHERE id = 1`, v); err != nil {
		return nil, fmt.Errorf("set similarity_min: %w", err)
	}
	return s.Load()
}

// SetAlertAfter updates the alert_after field and returns the reloaded Config.
func (s *configStore) SetAlertAfter(v int64) (*Config, error) {
	if _, err := s.db.Exec(`UPDATE config SET alert_after = ? WHERE id = 1`, v); err != nil {
		return nil, fmt.Errorf("set alert_after: %w", err)
	}
	return s.Load()
}

// SetWindowSeconds updates the window_seconds field and returns the reloaded Config.
func (s *configStore) SetWindowSeconds(v int64) (*Config, error) {
	if _, err := s.db.Exec(`UPDATE config SET window_seconds = ? WHERE id = 1`, v); err != nil {
		return nil, fmt.Errorf("set window_seconds: %w", err)
	}
	return s.Load()
}

// SetTimeoutDuration updates the timeout_duration field and returns the reloaded Config.
func (s *configStore) SetTimeoutDuration(v int64) (*Config, error) {
	if _, err := s.db.Exec(`UPDATE config SET timeout_duration = ? WHERE id = 1`, v); err != nil {
		return nil, fmt.Errorf("set timeout_duration: %w", err)
	}
	return s.Load()
}

// SetUnifyAttachments updates the unify_attachments field and returns the reloaded Config.
func (s *configStore) SetUnifyAttachments(v bool) (*Config, error) {
	if _, err := s.db.Exec(`UPDATE config SET unify_attachments = ? WHERE id = 1`, boolToInt(v)); err != nil {
		return nil, fmt.Errorf("set unify_attachments: %w", err)
	}
	return s.Load()
}

// SetAlertChannel updates the alert_channel_id field and returns the reloaded Config.
func (s *configStore) SetAlertChannel(id snowflake.ID) (*Config, error) {
	if _, err := s.db.Exec(`UPDATE config SET alert_channel_id = ? WHERE id = 1`, id.String()); err != nil {
		return nil, fmt.Errorf("set alert_channel_id: %w", err)
	}
	return s.Load()
}

// SetAction enables or disables a single action in the actions CSV column and
// returns the reloaded Config. Exclusivity between delete_all/delete_last and
// kick_user/ban_user is enforced by the caller (command handler), not here.
func (s *configStore) SetAction(action string, enabled bool) (*Config, error) {
	cfg, err := s.Load()
	if err != nil {
		return nil, err
	}
	if enabled {
		cfg.Actions[action] = true
	} else {
		delete(cfg.Actions, action)
	}
	if err := s.writeActions(cfg.Actions); err != nil {
		return nil, err
	}
	return s.Load()
}

func (s *configStore) writeActions(actions map[string]bool) error {
	names := make([]string, 0, len(actions))
	for a := range actions {
		names = append(names, a)
	}
	csv := strings.Join(names, ",")
	if _, err := s.db.Exec(`UPDATE config SET actions = ? WHERE id = 1`, csv); err != nil {
		return fmt.Errorf("set actions: %w", err)
	}
	return nil
}

// SetExcludedChannel adds or removes a channel from the excluded_channel_ids
// CSV column and returns the reloaded Config.
func (s *configStore) SetExcludedChannel(id snowflake.ID, excluded bool) (*Config, error) {
	cfg, err := s.Load()
	if err != nil {
		return nil, err
	}
	if excluded {
		cfg.ExcludedChannels[id] = true
	} else {
		delete(cfg.ExcludedChannels, id)
	}
	ids := make([]string, 0, len(cfg.ExcludedChannels))
	for chID := range cfg.ExcludedChannels {
		ids = append(ids, chID.String())
	}
	csv := strings.Join(ids, ",")
	if _, err := s.db.Exec(`UPDATE config SET excluded_channel_ids = ? WHERE id = 1`, csv); err != nil {
		return nil, fmt.Errorf("set excluded_channel_ids: %w", err)
	}
	return s.Load()
}

// SetMessage writes a message template and returns the reloaded Config.
func (s *configStore) SetMessage(key, template string) (*Config, error) {
	if _, err := s.db.Exec(`UPDATE messages SET template = ? WHERE key = ?`, template, key); err != nil {
		return nil, fmt.Errorf("set message %s: %w", key, err)
	}
	return s.Load()
}
