package auth

import (
	"errors"
	"reflect"
	"testing"
)

func TestParseTokens(t *testing.T) {
	entries, err := ParseTokens("alice:admin:s3cret, bob:reviewer:t0k3n")
	if err != nil {
		t.Fatalf("ParseTokens() error = %v", err)
	}
	want := []TokenEntry{
		{Name: "alice", Role: RoleAdmin, Token: "s3cret"},
		{Name: "bob", Role: RoleReviewer, Token: "t0k3n"},
	}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("ParseTokens() = %#v, want %#v", entries, want)
	}

	// The token part may contain ':' (SplitN with 3 fields).
	entries, err = ParseTokens("ci:admin:v1:rotated:xyz")
	if err != nil || len(entries) != 1 || entries[0].Token != "v1:rotated:xyz" {
		t.Fatalf("ParseTokens(colon token) = %#v, %v; want the full token kept", entries, err)
	}

	// Empty input disables auth without error.
	if entries, err := ParseTokens("  "); err != nil || entries != nil {
		t.Fatalf("ParseTokens(blank) = %#v, %v; want nil, nil", entries, err)
	}

	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"missing fields", "alice:admin"},
		{"empty name", ":admin:tok"},
		{"empty token", "alice:admin:"},
		{"unknown role", "alice:root:tok"},
		{"duplicate name", "alice:admin:tok1,alice:reviewer:tok2"},
		{"duplicate token", "alice:admin:tok,bob:reviewer:tok"},
	} {
		if _, err := ParseTokens(tc.raw); err == nil {
			t.Fatalf("ParseTokens(%s %q) error = nil, want error", tc.name, tc.raw)
		}
	}
}

func TestAllows(t *testing.T) {
	admin := Principal{Name: "alice", Role: RoleAdmin}
	reviewer := Principal{Name: "bob", Role: RoleReviewer}
	if !admin.Allows(RoleAdmin) || !admin.Allows(RoleReviewer) {
		t.Fatal("admin must cover both roles")
	}
	if !reviewer.Allows(RoleReviewer) {
		t.Fatal("reviewer must cover reviewer operations")
	}
	if reviewer.Allows(RoleAdmin) {
		t.Fatal("reviewer must not cover admin operations")
	}
}

func TestAuthenticate(t *testing.T) {
	a := New([]TokenEntry{
		{Name: "alice", Role: RoleAdmin, Token: "s3cret"},
		{Name: "bob", Role: RoleReviewer, Token: "t0k3n"},
		{Name: "carol", Role: RoleReviewer, Token: "c4rol"},
	})
	if !a.Enabled() {
		t.Fatal("Enabled() = false with three tokens configured")
	}

	for _, tc := range []struct {
		token string
		want  Principal
	}{
		{"s3cret", Principal{Name: "alice", Role: RoleAdmin}},
		{"t0k3n", Principal{Name: "bob", Role: RoleReviewer}},
		{"c4rol", Principal{Name: "carol", Role: RoleReviewer}},
	} {
		got, err := a.Authenticate(tc.token)
		if err != nil || got != tc.want {
			t.Fatalf("Authenticate(%q) = %#v, %v; want %#v", tc.token, got, err, tc.want)
		}
	}

	if _, err := a.Authenticate("wrong"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Authenticate(wrong) error = %v, want ErrInvalidToken", err)
	}
	if _, err := a.Authenticate(""); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("Authenticate(empty) error = %v, want ErrInvalidToken", err)
	}

	disabled := New(nil)
	if disabled.Enabled() {
		t.Fatal("Enabled() = true with no tokens")
	}
	if _, err := disabled.Authenticate("s3cret"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled Authenticate() error = %v, want ErrDisabled", err)
	}
}
