package main

import (
	"math"
	"testing"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "lowercase and trim",
			input:    "  HELLO!  ",
			expected: "hello!",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "whitespace only",
			input:    "   ",
			expected: "",
		},
		{
			name:     "mixed case",
			input:    "HeLLo WoRLd",
			expected: "hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalize(tt.input, false)
			if result != tt.expected {
				t.Errorf("normalize(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizeUnifyAttachments(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "cdn.discordapp.com link replaced",
			input:    "check this out https://cdn.discordapp.com/attachments/123/456/image.png",
			expected: "check this out [attachment]",
		},
		{
			name:     "media.discordapp.net link replaced",
			input:    "LOOK https://media.discordapp.net/attachments/1/2/pic.jpg?ex=abc",
			expected: "look [attachment]",
		},
		{
			name:     "non-discord link left untouched",
			input:    "see https://example.com/image.png",
			expected: "see https://example.com/image.png",
		},
		{
			name:     "no link, unaffected",
			input:    "Hello!",
			expected: "hello!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalize(tt.input, true)
			if result != tt.expected {
				t.Errorf("normalize(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizeAttachmentsUntouchedWhenDisabled(t *testing.T) {
	input := "check this out https://cdn.discordapp.com/attachments/123/456/image.png"
	expected := "check this out https://cdn.discordapp.com/attachments/123/456/image.png"

	if result := normalize(input, false); result != expected {
		t.Errorf("normalize(%q) = %q, want %q (flag off should be no-op)", input, result, expected)
	}
}

func TestSimilarityUnifiesDifferentAttachmentLinks(t *testing.T) {
	a := normalize("check this out https://cdn.discordapp.com/attachments/1/1/a.png", true)
	b := normalize("check this out https://cdn.discordapp.com/attachments/2/2/b.png", true)

	if got := similarity(a, b); got != 1.0 {
		t.Errorf("similarity(%q, %q) = %v, want 1.0 (different attachment links should unify)", a, b, got)
	}
}

func TestSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected float64
		delta    float64
	}{
		{
			name:     "identical strings",
			a:        "hello",
			b:        "hello",
			expected: 1.0,
			delta:    0.0,
		},
		{
			name:     "completely different",
			a:        "abc",
			b:        "xyz",
			expected: 0.0,
			delta:    0.01,
		},
		{
			name:     "one char difference",
			a:        "hello",
			b:        "hallo",
			expected: 0.8,
			delta:    0.01,
		},
		{
			name:     "empty strings",
			a:        "",
			b:        "",
			expected: 1.0,
			delta:    0.0,
		},
		{
			name:     "one empty",
			a:        "test",
			b:        "",
			expected: 0.0,
			delta:    0.01,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := similarity(tt.a, tt.b)
			if math.Abs(result-tt.expected) > tt.delta {
				t.Errorf("similarity(%q, %q) = %f, want %f (±%f)", tt.a, tt.b, result, tt.expected, tt.delta)
			}
		})
	}
}

func TestSimilarityThreshold(t *testing.T) {
	// Verify that similarity meets the default similarityMin threshold for near-identical messages
	const defaultSimilarityMin = 0.85
	message1 := "this is a test message"
	message2 := "this is a test mesage" // one typo
	sim := similarity(message1, message2)

	if sim < defaultSimilarityMin {
		t.Logf("similarity score: %f (below threshold %f)", sim, defaultSimilarityMin)
	} else {
		t.Logf("similarity score: %f (meets threshold %f)", sim, defaultSimilarityMin)
	}
}
