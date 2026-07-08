package main

import "strings"

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
