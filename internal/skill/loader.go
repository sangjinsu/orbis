package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// loadEntry loads a single skill body from dir using its index metadata and
// computes the content hash and character count used for budgeting and run
// snapshots. A missing or empty body is a clear, named error so a broken index
// fails loudly at load time rather than silently at dispatch time.
func loadEntry(dir string, m Metadata) (Entry, error) {
	bodyPath := filepath.Join(dir, m.Path)
	data, err := os.ReadFile(bodyPath)
	if err != nil {
		return Entry{}, fmt.Errorf("read skill body %q (%s): %w", m.ID, bodyPath, err)
	}
	body := string(data)
	if strings.TrimSpace(body) == "" {
		return Entry{}, fmt.Errorf("skill body %q is empty: %s", m.ID, bodyPath)
	}
	sum := sha256.Sum256(data)
	return Entry{
		Metadata:    m,
		Body:        body,
		ContentHash: hex.EncodeToString(sum[:]),
		Chars:       utf8.RuneCountInString(body),
	}, nil
}
