package main

import (
	"os"
	"strconv"
	"strings"
)

const (
	maxCached = 50   // max messages stored per user
	cacheTTL  = 3600 // key expires after 1 hour of inactivity
)

var (
	similarityMin    float64         = 0.85
	alertAfter       int64           = 3
	windowSeconds    int64           = 300
	timeoutDuration  int64           = 300 // seconds to timeout a user
	actions          map[string]bool       // set of actions to execute on spam detection
	unifyAttachments bool                  // treat all Discord CDN attachment links as identical content
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

	if v := os.Getenv("UNIFY_ATTACHMENTS"); v != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			unifyAttachments = parsed
		}
	}
}
