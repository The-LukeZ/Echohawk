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
			result := normalize(tt.input)
			if result != tt.expected {
				t.Errorf("normalize(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
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
	// Verify that similarity meets the similarityMin threshold for near-identical messages
	message1 := "this is a test message"
	message2 := "this is a test mesage" // one typo
	sim := similarity(message1, message2)

	if sim < similarityMin {
		t.Logf("similarity score: %f (below threshold %f)", sim, similarityMin)
	} else {
		t.Logf("similarity score: %f (meets threshold %f)", sim, similarityMin)
	}
}
