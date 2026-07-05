package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrSkillConflict is returned when a promotion targets a skill id that already
// exists in the active index without learned provenance — a curated seed.
// Learned skills are re-promoted in place as a new version instead.
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

// nextVersion parses a learned skill's integer version and returns it
// incremented. Learned skills always carry plain integer versions ("1", "2",
// ...); anything else marks a corrupt entry, and failing loudly routes the
// proposal through the retryable failed state instead of guessing a version.
func nextVersion(current string) (string, error) {
	n, err := strconv.Atoi(strings.TrimSpace(current))
	if err != nil || n < 1 {
		return "", fmt.Errorf("existing version %q is not a positive integer", current)
	}
	return strconv.Itoa(n + 1), nil
}
