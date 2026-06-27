package tool

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// ErrDuplicateTool is returned when registering a name that already exists.
var ErrDuplicateTool = errors.New("tool already registered")

// ErrUnknownTool is returned when a requested tool name is not registered.
var ErrUnknownTool = errors.New("unknown tool")

// Registry stores the available tools and exposes them by name or toolset.
type Registry interface {
	Register(t Tool) error
	Get(name string) (Tool, bool)
	List() []Tool
	ListByToolset(allowed []Toolset) []Tool
	// SchemasForLLM returns the LLM-facing schemas for the tools whose toolset
	// is enabled, so they can be forwarded to a provider's tool-calling API.
	SchemasForLLM(allowed []Toolset) []ToolSchema
}

type registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry returns an empty in-memory registry.
func NewRegistry() Registry {
	return &registry{tools: map[string]Tool{}}
}

func (r *registry) Register(t Tool) error {
	if t == nil {
		return errors.New("tool is nil")
	}
	name := t.Name()
	if name == "" {
		return errors.New("tool name is empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateTool, name)
	}
	r.tools[name] = t
	return nil
}

func (r *registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

func (r *registry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tools := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		tools = append(tools, t)
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name() < tools[j].Name() })
	return tools
}

func (r *registry) ListByToolset(allowed []Toolset) []Tool {
	all := r.List()
	filtered := make([]Tool, 0, len(all))
	for _, t := range all {
		if ToolsetAllowed(t.Metadata().Toolset, allowed) {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

func (r *registry) SchemasForLLM(allowed []Toolset) []ToolSchema {
	tools := r.ListByToolset(allowed)
	schemas := make([]ToolSchema, 0, len(tools))
	for _, t := range tools {
		schemas = append(schemas, t.Schema())
	}
	return schemas
}
