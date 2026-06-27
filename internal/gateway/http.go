package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/sangjinsu/orbis/internal/protocol"
)

type Runtime interface {
	HandleClientRequest(ctx context.Context, req protocol.ClientRequest) (protocol.AckPayload, error)
}

func NewHTTPHandler(runtime Runtime) http.Handler {
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
		handleWebSocket(w, r, runtime)
	})
	return mux
}

func handleWebSocket(w http.ResponseWriter, r *http.Request, runtime Runtime) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()

	for {
		readCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		var req protocol.ClientRequest
		err := wsjson.Read(readCtx, conn, &req)
		cancel()
		if err != nil {
			return
		}

		ack, err := runtime.HandleClientRequest(context.Background(), req)
		if err != nil {
			writeResponse(conn, protocol.ServerResponse{
				Type:  "res",
				ID:    req.ID,
				OK:    false,
				Error: err.Error(),
			})
			continue
		}
		payload, err := json.Marshal(ack)
		if err != nil {
			writeResponse(conn, protocol.ServerResponse{
				Type:  "res",
				ID:    req.ID,
				OK:    false,
				Error: err.Error(),
			})
			continue
		}
		writeResponse(conn, protocol.ServerResponse{
			Type:    "res",
			ID:      req.ID,
			OK:      true,
			Payload: payload,
		})
	}
}

func writeResponse(conn *websocket.Conn, res protocol.ServerResponse) {
	writeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = wsjson.Write(writeCtx, conn, res)
}
