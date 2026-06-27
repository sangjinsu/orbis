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
	HandleClientRequest(ctx context.Context, req protocol.ClientRequest) (protocol.AckPayload, error)
}

type Broker interface {
	Subscribe(ctx context.Context, sessionID string) (<-chan protocol.RuntimeEvent, func())
}

type HandlerOption func(*handlerConfig)

type handlerConfig struct {
	broker Broker
}

func WithBroker(broker Broker) HandlerOption {
	return func(cfg *handlerConfig) {
		cfg.broker = broker
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
		handleWebSocket(w, r, runtime, cfg.broker)
	})
	return mux
}

func handleWebSocket(w http.ResponseWriter, r *http.Request, runtime Runtime, broker Broker) {
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
		if timeout := defaultReadTimeout(); timeout > 0 {
			readCtx, cancel = context.WithTimeout(context.Background(), timeout)
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

		ack, err := runtime.HandleClientRequest(context.Background(), req)
		if err != nil {
			writeResponse(conn, &writeMu, protocol.ServerResponse{
				Type:  "res",
				ID:    req.ID,
				OK:    false,
				Error: err.Error(),
			})
			continue
		}
		payload, err := json.Marshal(ack)
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

func defaultReadTimeout() time.Duration {
	return 0
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
