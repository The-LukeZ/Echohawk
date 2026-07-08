package main

import (
	"math"
	"regexp"
	"strings"

	"github.com/agnivade/levenshtein"
)

// attachmentURLRegex matches Discord CDN/media links so different attachment URLs
// (e.g. two unrelated image uploads) can be collapsed to one placeholder before comparison.
var attachmentURLRegex = regexp.MustCompile(`(?i)https?://(?:cdn\.discordapp\.com|media\.discordapp\.net)/\S+`)

// normalize strips noise before comparing so "Hello!" and "hello" count as the same.
// When unifyAttachments is on, Discord CDN links are collapsed to one placeholder so
// spam consisting of different attachment URLs (e.g. unique image links) still matches.
func normalize(s string, unifyAttachments bool) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if unifyAttachments {
		s = attachmentURLRegex.ReplaceAllString(s, "[attachment]")
	}
	return s
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
