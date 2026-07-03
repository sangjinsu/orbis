package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// ErrSkillConflict is returned when a promotion targets a skill id that already
// exists in the active index. v2 rejects conflicts instead of creating a new
// version; multi-version promotion is future work.
var ErrSkillConflict = errors.New("skill id already exists")

// initialVersion is the version assigned to a newly promoted skill.
const initialVersion = "1"

// contentHash returns the sha256 hex digest of a skill body. It is the same
// derivation the loader uses for active skills, so a promoted skill's recorded
// hash matches what the store computes on reload.
func contentHash(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// normalizeVersion defaults an empty version to the initial version.
func normalizeVersion(version string) string {
	if strings.TrimSpace(version) == "" {
		return initialVersion
	}
	return strings.TrimSpace(version)
}

// EnsureSkillIDAvailable checks a proposed skill id against the active index
// snapshot and returns ErrSkillConflict when it is already taken. A nil index
// performs no check (the caller has nothing to conflict with).
func EnsureSkillIDAvailable(index Index, skillID string) error {
	if index == nil {
		return nil
	}
	for _, entry := range index.Snapshot() {
		if entry.ID == skillID {
			return fmt.Errorf("%w: %s", ErrSkillConflict, skillID)
		}
	}
	return nil
}
