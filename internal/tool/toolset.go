package tool

import "strings"

// Toolset groups tools by capability/risk so policy can enable a subset.
type Toolset string

const (
	ToolsetSafe      Toolset = "safe"
	ToolsetRead      Toolset = "read"
	ToolsetWrite     Toolset = "write"
	ToolsetNetwork   Toolset = "network"
	ToolsetRuntime   Toolset = "runtime"
	ToolsetDangerous Toolset = "dangerous"
)

// DefaultEnabledToolsets is the v0.2 default: only the safe toolset is enabled.
func DefaultEnabledToolsets() []Toolset {
	return []Toolset{ToolsetSafe}
}

// ToolsetAllowed reports whether toolset is present in allowed.
func ToolsetAllowed(toolset Toolset, allowed []Toolset) bool {
	for _, a := range allowed {
		if a == toolset {
			return true
		}
	}
	return false
}

// ParseToolsets parses a comma-separated list such as "safe,read" into a slice
// of toolsets. Blank entries are ignored. An empty input yields the default.
func ParseToolsets(raw string) []Toolset {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultEnabledToolsets()
	}
	var result []Toolset
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		result = append(result, Toolset(part))
	}
	if len(result) == 0 {
		return DefaultEnabledToolsets()
	}
	return result
}
