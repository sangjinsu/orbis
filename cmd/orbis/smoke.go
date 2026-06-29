package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/sangjinsu/orbis/internal/config"
	"github.com/sangjinsu/orbis/internal/domain"
	"github.com/sangjinsu/orbis/internal/protocol"
)

type wsSmokeConfig struct {
	URL       string
	SessionID string
	Text      string
	Timeout   time.Duration
	// RequireToolCall fails the smoke if the run completes without a successful
	// tool call, proving the real LLM drove a tool through the runtime.
	RequireToolCall bool
	// RequireSkill fails the smoke if the run completes without applying skills,
	// proving skill selection and context injection ran through the runtime.
	RequireSkill bool
}

func runWSSmoke(ctx context.Context, cfg wsSmokeConfig, out io.Writer) error {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 90 * time.Second
	}
	if cfg.SessionID == "" {
		cfg.SessionID = "session_smoke"
	}
	if cfg.Text == "" {
		cfg.Text = "Reply with exactly: orbis-runtime-ws-ok"
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, cfg.URL, nil)
	if err != nil {
		return fmt.Errorf("connect websocket: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	subParams, err := json.Marshal(map[string]string{"session_id": cfg.SessionID})
	if err != nil {
		return fmt.Errorf("marshal subscribe params: %w", err)
	}
	msgParams, err := json.Marshal(map[string]string{"session_id": cfg.SessionID, "text": cfg.Text})
	if err != nil {
		return fmt.Errorf("marshal message params: %w", err)
	}
	if err := wsjson.Write(ctx, conn, protocol.ClientRequest{Type: "req", ID: "smoke_sub", Method: "session.subscribe", Params: subParams}); err != nil {
		return fmt.Errorf("send subscribe: %w", err)
	}
	if err := wsjson.Write(ctx, conn, protocol.ClientRequest{Type: "req", ID: "smoke_msg", Method: "session.message", Params: msgParams}); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	var firstFailureEvent string
	var sawToolSucceeded bool
	var sawSkillApplied bool
	for {
		var raw json.RawMessage
		if err := wsjson.Read(ctx, conn, &raw); err != nil {
			if firstFailureEvent != "" {
				return fmt.Errorf("runtime event %s before terminal failure: %w", firstFailureEvent, err)
			}
			return fmt.Errorf("read smoke response: %w", err)
		}
		var header struct {
			Type  string `json:"type"`
			ID    string `json:"id"`
			OK    bool   `json:"ok"`
			Event string `json:"event"`
		}
		if err := json.Unmarshal(raw, &header); err != nil {
			return fmt.Errorf("decode smoke message: %w", err)
		}
		switch header.Type {
		case "res":
			fmt.Fprintf(out, "res:%s:%t\n", header.ID, header.OK)
			if !header.OK {
				return fmt.Errorf("request %s failed", header.ID)
			}
		case "event":
			fmt.Fprintf(out, "event:%s\n", header.Event)
			switch header.Event {
			case string(domain.EventToolCallSucceeded):
				sawToolSucceeded = true
			case string(domain.EventSkillApplied):
				sawSkillApplied = true
			case string(domain.EventRunCompleted):
				if cfg.RequireToolCall && !sawToolSucceeded {
					return fmt.Errorf("run completed without a successful tool call")
				}
				if cfg.RequireSkill && !sawSkillApplied {
					return fmt.Errorf("run completed without applying skills")
				}
				return nil
			case string(domain.EventRunFailed):
				return fmt.Errorf("runtime event %s", header.Event)
			case string(domain.EventLLMCallFailed), string(domain.EventToolCallRejected), string(domain.EventToolCallTimedOut):
				if firstFailureEvent == "" {
					firstFailureEvent = header.Event
				}
			}
		default:
			fmt.Fprintf(out, "unknown:%s\n", header.Type)
		}
	}
}

func wsURLFromAddr(addr string) string {
	if strings.HasPrefix(addr, "ws://") || strings.HasPrefix(addr, "wss://") {
		addr = strings.TrimRight(addr, "/")
		if strings.HasSuffix(addr, "/ws") {
			return addr
		}
		return addr + "/ws"
	}
	addr = strings.TrimPrefix(addr, "http://")
	addr = strings.TrimPrefix(addr, "https://")
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	addr = strings.TrimRight(addr, "/")
	if strings.HasSuffix(addr, "/ws") {
		return "ws://" + addr
	}
	return "ws://" + addr + "/ws"
}

func smokeConfigFromEnv(cfg config.Config) wsSmokeConfig {
	return wsSmokeConfig{
		URL:       wsURLFromAddr(cfg.Addr),
		SessionID: "session_smoke",
		Text:      "Reply with exactly: orbis-runtime-ws-ok",
		Timeout:   90 * time.Second,
	}
}

func toolSmokeConfigFromEnv(cfg config.Config) wsSmokeConfig {
	return wsSmokeConfig{
		URL:             wsURLFromAddr(cfg.Addr),
		SessionID:       "session_smoke_tool",
		Text:            "Use the math.add tool to add 1 and 2, then reply with the numeric result.",
		Timeout:         90 * time.Second,
		RequireToolCall: true,
	}
}

func skillSmokeConfigFromEnv(cfg config.Config) wsSmokeConfig {
	return wsSmokeConfig{
		URL:          wsURLFromAddr(cfg.Addr),
		SessionID:    "session_smoke_skill",
		Text:         "WebSocket으로 Orbis 런타임 테스트 방법 알려줘",
		Timeout:      90 * time.Second,
		RequireSkill: true,
	}
}
