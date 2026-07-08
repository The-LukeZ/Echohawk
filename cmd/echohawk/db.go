package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS config (
	id                    INTEGER PRIMARY KEY CHECK (id = 1),
	alert_channel_id      TEXT NOT NULL DEFAULT '',
	excluded_channel_ids  TEXT NOT NULL DEFAULT '',
	similarity_min        REAL NOT NULL DEFAULT 0.85,
	alert_after           INTEGER NOT NULL DEFAULT 3,
	window_seconds        INTEGER NOT NULL DEFAULT 300,
	timeout_duration      INTEGER NOT NULL DEFAULT 300,
	unify_attachments     INTEGER NOT NULL DEFAULT 0,
	actions               TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS messages (
	key      TEXT PRIMARY KEY,
	template TEXT NOT NULL
);
`

// defaultMessages seeds the messages table on first run - these are the same
// strings that used to be hardcoded in actions.go.
var defaultMessages = map[string]string{
	"alert":          "⚠️ **Spam detected** - <@{user_id}> sent {count} similar messages in the last {window} seconds.\nLatest message: `{content}`",
	"dm_user":        "⚠️ Your messages in the server have been flagged for spam. Please avoid sending repetitive messages.",
	"timeout_reason": "Automated timeout: repeated/near-duplicate messages ({count} in {window}s)",
	"kick_reason":    "Automated kick: repeated/near-duplicate messages ({count} in {window}s)",
	"ban_reason":     "Automated ban: repeated/near-duplicate messages ({count} in {window}s)",
}

// openDB opens (creating if needed) the SQLite config database at path,
// applies the schema, and seeds the config row + message templates on
// first run only. On first run it also picks up legacy env vars so
// existing .env-based deployments migrate without a manual DB edit.
func openDB(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	if err := seedConfig(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("seed config: %w", err)
	}

	if err := seedMessages(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("seed messages: %w", err)
	}

	return db, nil
}

func seedConfig(db *sql.DB) error {
	var exists bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM config WHERE id = 1)`).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}

	similarityMin := 0.85
	if v := os.Getenv("SIMILARITY_MIN"); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			similarityMin = parsed
		}
	}

	alertAfter := int64(3)
	if v := os.Getenv("ALERT_AFTER"); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			alertAfter = parsed
		}
	}

	windowSeconds := int64(300)
	if v := os.Getenv("WINDOW_SECONDS"); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			windowSeconds = parsed
		}
	}

	timeoutDuration := int64(300)
	if v := os.Getenv("TIMEOUT_DURATION"); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			timeoutDuration = parsed
		}
	}

	unifyAttachments := false
	if v := os.Getenv("UNIFY_ATTACHMENTS"); v != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			unifyAttachments = parsed
		}
	}

	actionsCSV := strings.TrimSpace(os.Getenv("ACTIONS"))
	excludedCSV := strings.TrimSpace(os.Getenv("EXCLUDED_CHANNEL_IDS"))
	alertChannelID := os.Getenv("ALERT_CHANNEL_ID")

	_, err := db.Exec(`
		INSERT INTO config (
			id, alert_channel_id, excluded_channel_ids, similarity_min,
			alert_after, window_seconds, timeout_duration, unify_attachments, actions
		) VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?)`,
		alertChannelID, excludedCSV, similarityMin,
		alertAfter, windowSeconds, timeoutDuration, boolToInt(unifyAttachments), actionsCSV,
	)
	return err
}

func seedMessages(db *sql.DB) error {
	for key, template := range defaultMessages {
		if _, err := db.Exec(
			`INSERT OR IGNORE INTO messages (key, template) VALUES (?, ?)`,
			key, template,
		); err != nil {
			return err
		}
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
