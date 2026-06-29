package skill

import (
	"encoding/json"
	"fmt"
	"os"
)

// indexFile is the on-disk shape of data/skills/index.json.
type indexFile struct {
	Skills []Metadata `json:"skills"`
}

// parseIndex reads and validates the skill index document at path. It uses only
// the standard library JSON parser (no YAML/front-matter dependency).
func parseIndex(path string) ([]Metadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read skill index %s: %w", path, err)
	}
	var doc indexFile
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse skill index %s: %w", path, err)
	}
	seen := make(map[string]struct{}, len(doc.Skills))
	for i, m := range doc.Skills {
		if m.ID == "" {
			return nil, fmt.Errorf("skill index entry %d: missing id", i)
		}
		if m.Path == "" {
			return nil, fmt.Errorf("skill %q: missing path", m.ID)
		}
		if _, dup := seen[m.ID]; dup {
			return nil, fmt.Errorf("skill %q: duplicate id", m.ID)
		}
		seen[m.ID] = struct{}{}
	}
	return doc.Skills, nil
}
