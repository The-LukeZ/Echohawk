package main

import (
	"reflect"
	"testing"
)

func TestUnknownPlaceholders(t *testing.T) {
	tests := []struct {
		name     string
		template string
		want     []string
	}{
		{"no placeholders", "just plain text", nil},
		{"all known", "{user_id} did it {count} times in {window}s: {content}", nil},
		{"one unknown", "hello {name}", []string{"{name}"}},
		{"mixed", "{count} of {foo} and {bar}", []string{"{foo}", "{bar}"}},
		{"unterminated brace ignored", "hello {count", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := unknownPlaceholders(tt.template)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("unknownPlaceholders(%q) = %v, want %v", tt.template, got, tt.want)
			}
		})
	}
}
