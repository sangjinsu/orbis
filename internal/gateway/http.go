package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/sangjinsu/orbis/internal/protocol"
)

type Runtime interface {
	HandleClientRequest(ctx context.Context, req protocol.ClientRequest) (json.RawMessage, error)
}

type Broker interface {
	Subscribe(ctx context.Context, sessionID string) (<-chan protocol.RuntimeEvent, func())
}

// Skills exposes the skill catalog for read-only HTTP inspection and reload. It
// returns wire DTOs so the gateway stays decoupled from the internal store.
type Skills interface {
	ListSkills() protocol.SkillListPayload
	GetSkill(id string) (protocol.SkillDetailPayload, bool)
	ReloadSkills() error
}

type HandlerOption func(*handlerConfig)

type handlerConfig struct {
	broker      Broker
	skills      Skills
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
			if err := cfg.skills.ReloadSkills(); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, protocol.SkillReloadPayload{Count: len(cfg.skills.ListSkills().Skills)})
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
	if params.SessionID == "" {
		writeResponse(conn, writeMu, protocol.ServerResponse{Type: "res", ID: req.ID, OK: false, Error: "session_id is required"})
		return nil
	}
	events, unsubscribe := broker.Subscribe(context.Background(), params.SessionID)
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
