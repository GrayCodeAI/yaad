package engine

import (
	"context"
	"testing"
)

func TestGetUserProfile_Empty(t *testing.T) {
	eng := newTestEngine()
	ctx := context.Background()
	p, err := eng.GetUserProfile(ctx, "myproject")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Preferences) != 0 {
		t.Fatalf("expected empty preferences, got %v", p.Preferences)
	}
}

func TestUpdateAndGetUserPreference(t *testing.T) {
	eng := newTestEngine()
	ctx := context.Background()

	if err := eng.UpdateUserPreference(ctx, "myproject", "indent", "tabs"); err != nil {
		t.Fatal(err)
	}
	if err := eng.UpdateUserPreference(ctx, "myproject", "test_framework", "testify"); err != nil {
		t.Fatal(err)
	}

	p, err := eng.GetUserProfile(ctx, "myproject")
	if err != nil {
		t.Fatal(err)
	}
	if p.Preferences["indent"] != "tabs" {
		t.Fatalf("expected indent=tabs, got %q", p.Preferences["indent"])
	}
	if p.Preferences["test_framework"] != "testify" {
		t.Fatalf("expected test_framework=testify, got %q", p.Preferences["test_framework"])
	}
}

func TestUpdateUserPreference_Upsert(t *testing.T) {
	eng := newTestEngine()
	ctx := context.Background()

	if err := eng.UpdateUserPreference(ctx, "proj", "indent", "tabs"); err != nil {
		t.Fatal(err)
	}
	if err := eng.UpdateUserPreference(ctx, "proj", "indent", "spaces"); err != nil {
		t.Fatal(err)
	}

	p, err := eng.GetUserProfile(ctx, "proj")
	if err != nil {
		t.Fatal(err)
	}
	if p.Preferences["indent"] != "spaces" {
		t.Fatalf("expected upserted value 'spaces', got %q", p.Preferences["indent"])
	}
}

func TestUpdateUserPreference_EmptyKey(t *testing.T) {
	eng := newTestEngine()
	ctx := context.Background()
	if err := eng.UpdateUserPreference(ctx, "proj", "", "val"); err == nil {
		t.Fatal("expected error for empty key")
	}
}
