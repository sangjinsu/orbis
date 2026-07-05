package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sangjinsu/orbis/internal/auth"
	"github.com/sangjinsu/orbis/internal/domain"
	"github.com/sangjinsu/orbis/internal/protocol"
	"github.com/sangjinsu/orbis/internal/skill"
)

// SkillCatalog is the read-only view of the skill store the runtime needs to
// serve the skill APIs. *skill.Store satisfies it. It is separate from the
// reducer's skill.Index and the dispatcher's skill.Bodies so each consumer
// depends only on the capability it uses.
type SkillCatalog interface {
	List() []skill.Metadata
	Get(id string) (skill.Entry, bool)
	Reload() error
}

type skillGetParams struct {
	SkillID string `json:"skill_id"`
}

// skillSummaryFromMetadata maps internal index metadata to the wire summary, so
// the wire contract stays independent of the store representation.
func skillSummaryFromMetadata(m skill.Metadata) protocol.SkillSummary {
	return protocol.SkillSummary{
		ID:           m.ID,
		Name:         m.Name,
		Title:        m.Title,
		Description:  m.Description,
		Tags:         m.Tags,
		Triggers:     m.Triggers,
		Version:      m.Version,
		Status:       m.Status,
		Priority:     m.Priority,
		RelatedTools: m.RelatedTools,
	}
}

// ListSkills returns all skills as wire summaries. It is nil-safe: with no
// catalog (skills disabled) it returns an empty list. It implements
// gateway.Skills so the HTTP endpoints and the WS handlers share one impl.
func (s *RuntimeService) ListSkills() protocol.SkillListPayload {
	payload := protocol.SkillListPayload{Skills: []protocol.SkillSummary{}}
	if s.skills == nil {
		return payload
	}
	for _, m := range s.skills.List() {
		payload.Skills = append(payload.Skills, skillSummaryFromMetadata(m))
	}
	return payload
}

// GetSkill returns one skill's summary plus its body, or ok=false when the skill
// is unknown or skills are disabled.
func (s *RuntimeService) GetSkill(id string) (protocol.SkillDetailPayload, bool) {
	if s.skills == nil {
		return protocol.SkillDetailPayload{}, false
	}
	entry, ok := s.skills.Get(id)
	if !ok {
		return protocol.SkillDetailPayload{}, false
	}
	return protocol.SkillDetailPayload{
		SkillSummary: skillSummaryFromMetadata(entry.Metadata),
		Body:         entry.Body,
		ContentHash:  entry.ContentHash,
		Chars:        entry.Chars,
	}, true
}

// skillReloadEventPayload is the metadata payload of the global reload events
// emitted by the standalone reload endpoint. Actor is the authenticated
// principal's name; Count is the reloaded skill count.
type skillReloadEventPayload struct {
	Actor string `json:"actor"`
	Count int    `json:"count,omitempty"`
}

// ReloadSkills re-reads the skill index and bodies from disk; actor is the
// authenticated principal's name for the reload events. It errors when skills
// are disabled so the caller can surface a clear failure. The standalone
// reload has no session, so its SkillIndexReloadRequested /
// SkillIndexReloaded events go to the global feed only (the approve flow
// calls s.skills.Reload() directly and emits session-scoped events itself).
func (s *RuntimeService) ReloadSkills(actor string) error {
	if s.skills == nil {
		return errors.New("skills are not enabled")
	}
	if actor == "" {
		actor = skill.ActorUnknown
	}
	s.publishGlobalSkillEvent(domain.EventSkillIndexReloadRequested, skillReloadEventPayload{Actor: actor})
	if err := s.skills.Reload(); err != nil {
		return err
	}
	s.publishGlobalSkillEvent(domain.EventSkillIndexReloaded, skillReloadEventPayload{
		Actor: actor,
		Count: len(s.skills.List()),
	})
	return nil
}

// publishGlobalSkillEvent emits one global-only wire event: no session, no
// sequence, not persisted — a live administrative signal. The payloads are
// fixed structs, so the marshal cannot fail in practice.
func (s *RuntimeService) publishGlobalSkillEvent(typ domain.EventType, payload any) {
	if s.broker == nil {
		return
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return
	}
	s.broker.PublishGlobal(protocol.RuntimeEvent{
		Type:    "event",
		Event:   string(typ),
		Payload: encoded,
	})
}

func (s *RuntimeService) handleSkillList(_ context.Context, _ protocol.ClientRequest) (json.RawMessage, error) {
	return marshalPayload(s.ListSkills())
}

func (s *RuntimeService) handleSkillGet(_ context.Context, req protocol.ClientRequest) (json.RawMessage, error) {
	var params skillGetParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, fmt.Errorf("decode skill.get params: %w", err)
		}
	}
	if params.SkillID == "" {
		return nil, fmt.Errorf("skill_id is required")
	}
	detail, ok := s.GetSkill(params.SkillID)
	if !ok {
		return nil, fmt.Errorf("skill %q not found", params.SkillID)
	}
	return marshalPayload(detail)
}

type skillReloadParams struct {
	Token string `json:"token"`
}

// handleSkillReload is a mutating operation as of v2: it requires the admin
// token in params, and with no token configured it is disabled entirely.
func (s *RuntimeService) handleSkillReload(_ context.Context, req protocol.ClientRequest) (json.RawMessage, error) {
	var params skillReloadParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, fmt.Errorf("decode skill.reload params: %w", err)
		}
	}
	principal, err := s.requireRole(params.Token, auth.RoleAdmin)
	if err != nil {
		return nil, err
	}
	if err := s.ReloadSkills(principal.Name); err != nil {
		return nil, fmt.Errorf("reload skills: %w", err)
	}
	return marshalPayload(protocol.SkillReloadPayload{Count: len(s.ListSkills().Skills)})
}
