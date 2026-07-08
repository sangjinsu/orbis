package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/sangjinsu/orbis/internal/auth"
	"github.com/sangjinsu/orbis/internal/protocol"
)

type Runtime interface {
	HandleClientRequest(ctx context.Context, req protocol.ClientRequest) (json.RawMessage, error)
}

type Broker interface {
	Subscribe(ctx context.Context, sessionID string) (<-chan protocol.RuntimeEvent, func())
	SubscribeGlobal(ctx context.Context) (<-chan protocol.RuntimeEvent, func())
}

// Skills exposes the skill catalog for read-only HTTP inspection and reload. It
// returns wire DTOs so the gateway stays decoupled from the internal store.
// Mutating calls receive the authenticated principal's name as actor.
type Skills interface {
	ListSkills() protocol.SkillListPayload
	GetSkill(id string) (protocol.SkillDetailPayload, bool)
	ReloadSkills(actor string) error
}

// SkillLearning exposes the reviewable skill-proposal loop (v2) over HTTP. It
// returns wire DTOs; the gateway never touches the skill package. Mutating
// operations are only reachable through the auth gate and receive the
// authenticated principal's name as actor for the audit trail.
type SkillLearning interface {
	ListSkillProposals(status string) (protocol.SkillProposalListPayload, error)
	GetSkillProposal(id string) (protocol.SkillProposalDetailPayload, bool, error)
	CreateSkillProposal(ctx context.Context, runID, actor string) (protocol.SkillProposalDetailPayload, error)
	UpdateSkillProposal(ctx context.Context, id string, fields protocol.SkillProposalUpdateRequest, actor string) (protocol.SkillProposalDetailPayload, error)
	ApproveSkillProposal(ctx context.Context, id, actor string) (protocol.SkillProposalDetailPayload, error)
	RejectSkillProposal(ctx context.Context, id, reason, actor string) (protocol.SkillProposalDetailPayload, error)
}

type HandlerOption func(*handlerConfig)

type handlerConfig struct {
	broker      Broker
	skills      Skills
	learning    SkillLearning
	auth        *auth.Authenticator
	readTimeout time.Duration
}

func WithBroker(broker Broker) HandlerOption {
	return func(cfg *handlerConfig) {
		cfg.broker = broker
	}
}

// WithSkills registers the read-only HTTP skill endpoints (GET /skills,
// GET /skills/{skillID}, POST /skills/reload). Omitting it leaves those routes
// unregistered so they 404.
func WithSkills(skills Skills) HandlerOption {
	return func(cfg *handlerConfig) {
		cfg.skills = skills
	}
}

// WithSkillLearning registers the skill-proposal review endpoints. Omitting it
// leaves those routes unregistered so they 404.
func WithSkillLearning(learning SkillLearning) HandlerOption {
	return func(cfg *handlerConfig) {
		cfg.learning = learning
	}
}

// WithAuth sets the authenticator that guards mutating skill endpoints
// (proposal create/update/approve/reject and skills reload). A nil or empty
// authenticator — the default — disables those endpoints entirely instead of
// leaving them open.
func WithAuth(a *auth.Authenticator) HandlerOption {
	return func(cfg *handlerConfig) {
		cfg.auth = a
	}
}

// requireRole gates a mutating endpoint: 403 when no tokens are configured
// (endpoint disabled), 401 on an unknown bearer token, 403 when the token's
// role does not cover the required role. On success it returns the principal
// whose name becomes the audit actor.
func requireRole(w http.ResponseWriter, r *http.Request, a *auth.Authenticator, required auth.Role) (auth.Principal, bool) {
	if a == nil || !a.Enabled() {
		http.Error(w, "auth endpoints are disabled: no tokens configured", http.StatusForbidden)
		return auth.Principal{}, false
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	principal, err := a.Authenticate(token)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return auth.Principal{}, false
	}
	if !principal.Allows(required) {
		http.Error(w, "insufficient role for this operation", http.StatusForbidden)
		return auth.Principal{}, false
	}
	return principal, true
}

// WithReadTimeout bounds how long a single WebSocket read may block. Zero (the
// default) disables it so idle subscriber connections are not closed.
func WithReadTimeout(timeout time.Duration) HandlerOption {
	return func(cfg *handlerConfig) {
		cfg.readTimeout = timeout
	}
}

func NewHTTPHandler(runtime Runtime, opts ...HandlerOption) http.Handler {
	cfg := handlerConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /debug", handleDebug)
	mux.HandleFunc("GET /debug/", handleDebug)
	mux.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {
		handleWebSocket(w, r, runtime, cfg.broker, cfg.readTimeout)
	})
	if cfg.skills != nil {
		mux.HandleFunc("GET /skills", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, cfg.skills.ListSkills())
		})
		mux.HandleFunc("GET /skills/{skillID}", func(w http.ResponseWriter, r *http.Request) {
			detail, ok := cfg.skills.GetSkill(r.PathValue("skillID"))
			if !ok {
				http.Error(w, "skill not found", http.StatusNotFound)
				return
			}
			writeJSON(w, http.StatusOK, detail)
		})
		mux.HandleFunc("POST /skills/reload", func(w http.ResponseWriter, r *http.Request) {
			principal, ok := requireRole(w, r, cfg.auth, auth.RoleAdmin)
			if !ok {
				return
			}
			if err := cfg.skills.ReloadSkills(principal.Name); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, protocol.SkillReloadPayload{Count: len(cfg.skills.ListSkills().Skills)})
		})
	}
	if cfg.learning != nil {
		mux.HandleFunc("GET /skill-proposals", func(w http.ResponseWriter, r *http.Request) {
			payload, err := cfg.learning.ListSkillProposals(r.URL.Query().Get("status"))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, payload)
		})
		mux.HandleFunc("GET /skill-proposals/{proposalID}", func(w http.ResponseWriter, r *http.Request) {
			detail, found, err := cfg.learning.GetSkillProposal(r.PathValue("proposalID"))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if !found {
				http.Error(w, "skill proposal not found", http.StatusNotFound)
				return
			}
			writeJSON(w, http.StatusOK, detail)
		})
		mux.HandleFunc("POST /runs/{runID}/skill-proposals", func(w http.ResponseWriter, r *http.Request) {
			principal, ok := requireRole(w, r, cfg.auth, auth.RoleReviewer)
			if !ok {
				return
			}
			detail, err := cfg.learning.CreateSkillProposal(r.Context(), r.PathValue("runID"), principal.Name)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusCreated, detail)
		})
		mux.HandleFunc("PATCH /skill-proposals/{proposalID}", func(w http.ResponseWriter, r *http.Request) {
			principal, ok := requireRole(w, r, cfg.auth, auth.RoleReviewer)
			if !ok {
				return
			}
			var fields protocol.SkillProposalUpdateRequest
			if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
				http.Error(w, "invalid request body", http.StatusBadRequest)
				return
			}
			detail, err := cfg.learning.UpdateSkillProposal(r.Context(), r.PathValue("proposalID"), fields, principal.Name)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusOK, detail)
		})
		mux.HandleFunc("POST /skill-proposals/{proposalID}/approve", func(w http.ResponseWriter, r *http.Request) {
			principal, ok := requireRole(w, r, cfg.auth, auth.RoleReviewer)
			if !ok {
				return
			}
			detail, err := cfg.learning.ApproveSkillProposal(r.Context(), r.PathValue("proposalID"), principal.Name)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusOK, detail)
		})
		mux.HandleFunc("POST /skill-proposals/{proposalID}/reject", func(w http.ResponseWriter, r *http.Request) {
			principal, ok := requireRole(w, r, cfg.auth, auth.RoleReviewer)
			if !ok {
				return
			}
			var body struct {
				Reason string `json:"reason"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			detail, err := cfg.learning.RejectSkillProposal(r.Context(), r.PathValue("proposalID"), body.Reason, principal.Name)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusOK, detail)
		})
	}
	return mux
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func handleWebSocket(w http.ResponseWriter, r *http.Request, runtime Runtime, broker Broker, readTimeout time.Duration) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	var writeMu sync.Mutex
	var unsubscribers []func()
	defer func() {
		for _, unsubscribe := range unsubscribers {
			unsubscribe()
		}
	}()

	for {
		readCtx := context.Background()
		cancel := func() {}
		if readTimeout > 0 {
			readCtx, cancel = context.WithTimeout(context.Background(), readTimeout)
		}
		var req protocol.ClientRequest
		err := wsjson.Read(readCtx, conn, &req)
		cancel()
		if err != nil {
			return
		}

		if req.Method == "session.subscribe" {
			if unsubscribe := handleSubscribe(conn, &writeMu, broker, req); unsubscribe != nil {
				unsubscribers = append(unsubscribers, unsubscribe)
			}
			continue
		}

		payload, err := runtime.HandleClientRequest(context.Background(), req)
		if err != nil {
			writeResponse(conn, &writeMu, protocol.ServerResponse{
				Type:  "res",
				ID:    req.ID,
				OK:    false,
				Error: err.Error(),
			})
			continue
		}
		writeResponse(conn, &writeMu, protocol.ServerResponse{
			Type:    "res",
			ID:      req.ID,
			OK:      true,
			Payload: payload,
		})
	}
}

type subscribeParams struct {
	SessionID string `json:"session_id"`
	// Scope "global" subscribes to the session-independent feed (skill
	// lifecycle events); empty means a session subscription.
	Scope string `json:"scope,omitempty"`
}

func handleSubscribe(conn *websocket.Conn, writeMu *sync.Mutex, broker Broker, req protocol.ClientRequest) func() {
	if broker == nil {
		writeResponse(conn, writeMu, protocol.ServerResponse{Type: "res", ID: req.ID, OK: false, Error: "broker is not configured"})
		return nil
	}
	var params subscribeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeResponse(conn, writeMu, protocol.ServerResponse{Type: "res", ID: req.ID, OK: false, Error: err.Error()})
		return nil
	}
	var events <-chan protocol.RuntimeEvent
	var unsubscribe func()
	switch params.Scope {
	case "global":
		events, unsubscribe = broker.SubscribeGlobal(context.Background())
	case "":
		if params.SessionID == "" {
			writeResponse(conn, writeMu, protocol.ServerResponse{Type: "res", ID: req.ID, OK: false, Error: "session_id is required"})
			return nil
		}
		events, unsubscribe = broker.Subscribe(context.Background(), params.SessionID)
	default:
		writeResponse(conn, writeMu, protocol.ServerResponse{Type: "res", ID: req.ID, OK: false, Error: "unknown scope: " + params.Scope})
		return nil
	}
	writeResponse(conn, writeMu, protocol.ServerResponse{Type: "res", ID: req.ID, OK: true, Payload: json.RawMessage(`{}`)})
	go func() {
		for event := range events {
			writeRuntimeEvent(conn, writeMu, event)
		}
	}()
	return unsubscribe
}

func writeResponse(conn *websocket.Conn, writeMu *sync.Mutex, res protocol.ServerResponse) {
	writeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	writeMu.Lock()
	defer writeMu.Unlock()
	_ = wsjson.Write(writeCtx, conn, res)
}

func writeRuntimeEvent(conn *websocket.Conn, writeMu *sync.Mutex, event protocol.RuntimeEvent) {
	writeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	writeMu.Lock()
	defer writeMu.Unlock()
	_ = wsjson.Write(writeCtx, conn, event)
}
