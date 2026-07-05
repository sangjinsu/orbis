// Package auth provides named-token authentication for the runtime's mutating
// surfaces (v2.1). Tokens are static bearer credentials configured at startup;
// each carries a name (the audit actor) and a role. It is a leaf package so
// config, gateway, and app can all depend on it without cycles.
package auth

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
)

// Role is the coarse permission level a token grants. RoleReviewer covers the
// skill-proposal review operations (create/update/approve/reject); RoleAdmin
// additionally covers operational surfaces such as the skills reload.
type Role string

const (
	RoleAdmin    Role = "admin"
	RoleReviewer Role = "reviewer"
)

// Principal is an authenticated caller: the token's name (recorded as the
// audit actor) and its role.
type Principal struct {
	Name string
	Role Role
}

// Allows reports whether the principal's role covers the required role.
// Admin covers everything; reviewer covers only reviewer-level operations.
func (p Principal) Allows(required Role) bool {
	if p.Role == RoleAdmin {
		return true
	}
	return p.Role == required
}

// TokenEntry is one configured credential.
type TokenEntry struct {
	Name  string
	Role  Role
	Token string
}

var (
	// ErrDisabled is returned when no tokens are configured: mutating
	// endpoints are disabled entirely (the safe default), never left open.
	ErrDisabled = errors.New("auth is disabled: no tokens configured")
	// ErrInvalidToken is returned for a token that matches no entry.
	ErrInvalidToken = errors.New("invalid token")
)

// ParseTokens parses the ORBIS_AUTH_TOKENS format: comma-separated
// name:role:token entries (e.g. "alice:admin:s3cret,bob:reviewer:t0k3n").
// The token part may itself contain ':' but never ','. Names and tokens must
// be unique and non-empty; the role must be a known role. An empty input
// yields no entries (auth disabled).
func ParseTokens(raw string) ([]TokenEntry, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var entries []TokenEntry
	names := map[string]struct{}{}
	tokens := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		fields := strings.SplitN(part, ":", 3)
		if len(fields) != 3 {
			return nil, fmt.Errorf("auth token entry %q: want name:role:token", part)
		}
		name := strings.TrimSpace(fields[0])
		role := Role(strings.TrimSpace(fields[1]))
		token := strings.TrimSpace(fields[2])
		if name == "" || token == "" {
			return nil, fmt.Errorf("auth token entry %q: name and token are required", part)
		}
		if role != RoleAdmin && role != RoleReviewer {
			return nil, fmt.Errorf("auth token entry %q: unknown role %q", part, role)
		}
		if _, dup := names[name]; dup {
			return nil, fmt.Errorf("auth token entry %q: duplicate name %q", part, name)
		}
		if _, dup := tokens[token]; dup {
			return nil, fmt.Errorf("auth token entry %q: duplicate token", part)
		}
		names[name] = struct{}{}
		tokens[token] = struct{}{}
		entries = append(entries, TokenEntry{Name: name, Role: role, Token: token})
	}
	if len(entries) == 0 {
		return nil, nil
	}
	return entries, nil
}

// Authenticator resolves bearer tokens to principals.
type Authenticator struct {
	entries []TokenEntry
}

// New creates an authenticator over the configured entries. A nil or empty
// entry list yields a disabled authenticator.
func New(entries []TokenEntry) *Authenticator {
	return &Authenticator{entries: entries}
}

// Enabled reports whether any token is configured. Disabled means every
// mutating endpoint is off (403 over HTTP), matching the v2 admin-gate
// invariant.
func (a *Authenticator) Enabled() bool {
	return a != nil && len(a.entries) > 0
}

// Authenticate resolves a presented token to its principal: ErrDisabled with
// no tokens configured, ErrInvalidToken when nothing matches. Every entry is
// visited with a constant-time comparison and no early return, so response
// timing reveals neither which entry matched nor whether any did. (The
// comparison still short-circuits on unequal lengths — leaking the token
// length is the standard accepted trade-off.)
func (a *Authenticator) Authenticate(token string) (Principal, error) {
	if !a.Enabled() {
		return Principal{}, ErrDisabled
	}
	presented := []byte(token)
	matched := -1
	for i := range a.entries {
		if subtle.ConstantTimeCompare(presented, []byte(a.entries[i].Token)) == 1 {
			matched = i
		}
	}
	if matched < 0 {
		return Principal{}, ErrInvalidToken
	}
	return Principal{Name: a.entries[matched].Name, Role: a.entries[matched].Role}, nil
}
