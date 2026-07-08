package main

import (
	"path/filepath"
	"testing"

	"github.com/disgoorg/snowflake/v2"
)

func newTestStore(t *testing.T) *configStore {
	t.Helper()
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return newConfigStore(db)
}

func TestSetSimilarityMin(t *testing.T) {
	s := newTestStore(t)
	cfg, err := s.SetSimilarityMin(0.5)
	if err != nil {
		t.Fatalf("SetSimilarityMin: %v", err)
	}
	if cfg.SimilarityMin != 0.5 {
		t.Errorf("SimilarityMin = %v, want 0.5", cfg.SimilarityMin)
	}
}

func TestSetAlertAfter(t *testing.T) {
	s := newTestStore(t)
	cfg, err := s.SetAlertAfter(7)
	if err != nil {
		t.Fatalf("SetAlertAfter: %v", err)
	}
	if cfg.AlertAfter != 7 {
		t.Errorf("AlertAfter = %v, want 7", cfg.AlertAfter)
	}
}

func TestSetWindowSeconds(t *testing.T) {
	s := newTestStore(t)
	cfg, err := s.SetWindowSeconds(600)
	if err != nil {
		t.Fatalf("SetWindowSeconds: %v", err)
	}
	if cfg.WindowSeconds != 600 {
		t.Errorf("WindowSeconds = %v, want 600", cfg.WindowSeconds)
	}
}

func TestSetTimeoutDuration(t *testing.T) {
	s := newTestStore(t)
	cfg, err := s.SetTimeoutDuration(120)
	if err != nil {
		t.Fatalf("SetTimeoutDuration: %v", err)
	}
	if cfg.TimeoutDuration != 120 {
		t.Errorf("TimeoutDuration = %v, want 120", cfg.TimeoutDuration)
	}
}

func TestSetUnifyAttachments(t *testing.T) {
	s := newTestStore(t)
	cfg, err := s.SetUnifyAttachments(true)
	if err != nil {
		t.Fatalf("SetUnifyAttachments: %v", err)
	}
	if !cfg.UnifyAttachments {
		t.Errorf("UnifyAttachments = false, want true")
	}
}

func TestSetAlertChannel(t *testing.T) {
	s := newTestStore(t)
	id := snowflake.ID(123456789)
	cfg, err := s.SetAlertChannel(id)
	if err != nil {
		t.Fatalf("SetAlertChannel: %v", err)
	}
	if cfg.AlertChannel != id {
		t.Errorf("AlertChannel = %v, want %v", cfg.AlertChannel, id)
	}
}

func TestSetActionAddRemove(t *testing.T) {
	s := newTestStore(t)

	cfg, err := s.SetAction("alert", true)
	if err != nil {
		t.Fatalf("SetAction add: %v", err)
	}
	if !cfg.Actions["alert"] {
		t.Errorf("expected alert action enabled")
	}

	cfg, err = s.SetAction("alert", false)
	if err != nil {
		t.Fatalf("SetAction remove: %v", err)
	}
	if cfg.Actions["alert"] {
		t.Errorf("expected alert action disabled")
	}
}

func TestSetExcludedChannelAddRemove(t *testing.T) {
	s := newTestStore(t)
	id := snowflake.ID(987654321)

	cfg, err := s.SetExcludedChannel(id, true)
	if err != nil {
		t.Fatalf("SetExcludedChannel add: %v", err)
	}
	if !cfg.ExcludedChannels[id] {
		t.Errorf("expected channel %v excluded", id)
	}

	cfg, err = s.SetExcludedChannel(id, false)
	if err != nil {
		t.Fatalf("SetExcludedChannel remove: %v", err)
	}
	if cfg.ExcludedChannels[id] {
		t.Errorf("expected channel %v no longer excluded", id)
	}
}

func TestSetMessage(t *testing.T) {
	s := newTestStore(t)
	cfg, err := s.SetMessage("alert", "custom {count} alert")
	if err != nil {
		t.Fatalf("SetMessage: %v", err)
	}
	if cfg.Messages["alert"] != "custom {count} alert" {
		t.Errorf("Messages[alert] = %q, want %q", cfg.Messages["alert"], "custom {count} alert")
	}
}
