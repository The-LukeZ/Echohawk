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

	alertChannelID, err := snowflake.Parse(alertChannelIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid alert_channel_id in config: %w", err)
	}
	cfg.AlertChannel = alertChannelID

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
