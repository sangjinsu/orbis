package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/sangjinsu/orbis/internal/domain"
	"github.com/sangjinsu/orbis/internal/protocol"
	"github.com/spf13/cobra"
)

// chatVerbosity controls how much runtime activity the REPL shows between the
// user's line and the assistant's answer.
type chatVerbosity int

const (
	chatMedium  chatVerbosity = iota // assistant text + one-line tool/skill notes
	chatQuiet                        // assistant text only
	chatVerbose                      // every runtime event
)

type chatConfig struct {
	URL       string
	SessionID string
	Verbosity chatVerbosity
}

// newChatCmd builds `orbis chat`, an interactive conversation with the
// runtime over the WebSocket protocol. Chatting needs no token: the session
// methods are open like every non-admin surface.
func newChatCmd() *cobra.Command {
	var addr, session string
	var verbose, quiet bool
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Chat interactively with the runtime",
		Args:  exactArgs(0, "orbis chat [--session id]"),
		RunE: func(c *cobra.Command, _ []string) error {
			if verbose && quiet {
				return fmt.Errorf("%w: --verbose and --quiet are mutually exclusive", errUsage)
			}
			verbosity := chatMedium
			if verbose {
				verbosity = chatVerbose
			}
			if quiet {
				verbosity = chatQuiet
			}
			if session == "" {
				session = "chat_" + randomHex(4)
			}
			return runChat(c.Context(), chatConfig{
				URL:       wsURLFromAddr(resolveAddr(addr)),
				SessionID: session,
				Verbosity: verbosity,
			}, c.InOrStdin(), c.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "", "server address (default $ORBIS_ADDR or :8080)")
	cmd.Flags().StringVar(&session, "session", "", "session id to attach to (default: a fresh chat_<hex> session)")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "show every runtime event")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "show assistant text only")
	return cmd
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "fallback"
	}
	return hex.EncodeToString(buf)
}

// runChat drives the REPL: subscribe once, then per user line send a
// session.message and stream that run's events until it reaches a terminal
// state. The server owns run timeouts, so a stuck run still ends the wait
// with RunFailed.
func runChat(ctx context.Context, cfg chatConfig, in io.Reader, out io.Writer) error {
	conn, _, err := websocket.Dial(ctx, cfg.URL, nil)
	if err != nil {
		return fmt.Errorf("connect websocket: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	subParams, err := json.Marshal(map[string]string{"session_id": cfg.SessionID})
	if err != nil {
		return fmt.Errorf("marshal subscribe params: %w", err)
	}
	if err := wsjson.Write(ctx, conn, protocol.ClientRequest{Type: "req", ID: "chat_sub", Method: "session.subscribe", Params: subParams}); err != nil {
		return fmt.Errorf("send subscribe: %w", err)
	}
	var ack protocol.ServerResponse
	if err := wsjson.Read(ctx, conn, &ack); err != nil {
		return fmt.Errorf("read subscribe ack: %w", err)
	}
	if !ack.OK {
		return fmt.Errorf("subscribe rejected: %s", ack.Error)
	}

	fmt.Fprintf(out, "session %s — /quit to leave (reattach later with --session %s)\n", cfg.SessionID, cfg.SessionID)

	// Reading stdin in a goroutine lets the prompt loop also honor Ctrl-C
	// (context cancellation) while blocked on user input.
	lines := make(chan string)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(in)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
	}()

	for turn := 1; ; turn++ {
		fmt.Fprint(out, "you> ")
		var line string
		select {
		case l, ok := <-lines:
			if !ok {
				fmt.Fprintln(out, "\nbye")
				return nil
			}
			line = strings.TrimSpace(l)
		case <-ctx.Done():
			fmt.Fprintln(out, "\nbye")
			return nil
		}

		switch line {
		case "":
			continue
		case "/quit", "/exit":
			fmt.Fprintln(out, "bye")
			return nil
		case "/session":
			fmt.Fprintln(out, cfg.SessionID)
			continue
		}

		msgParams, err := json.Marshal(map[string]string{"session_id": cfg.SessionID, "text": line})
		if err != nil {
			return fmt.Errorf("marshal message params: %w", err)
		}
		reqID := fmt.Sprintf("chat_msg_%d", turn)
		if err := wsjson.Write(ctx, conn, protocol.ClientRequest{Type: "req", ID: reqID, Method: "session.message", Params: msgParams}); err != nil {
			return fmt.Errorf("send message: %w", err)
		}
		if err := streamRun(ctx, conn, reqID, cfg.Verbosity, out); err != nil {
			if ctx.Err() != nil {
				fmt.Fprintln(out, "\nbye")
				return nil
			}
			return err
		}
	}
}

// streamRun consumes events until the run started by reqID reaches a terminal
// state, rendering assistant deltas as they arrive.
func streamRun(ctx context.Context, conn *websocket.Conn, reqID string, verbosity chatVerbosity, out io.Writer) error {
	runID := ""
	midLine := false // an assistant line is open (deltas printed, no newline yet)
	sawDelta := false

	// breakLine closes an open assistant line before printing a note.
	breakLine := func() {
		if midLine {
			fmt.Fprintln(out)
			midLine = false
		}
	}

	for {
		var raw json.RawMessage
		if err := wsjson.Read(ctx, conn, &raw); err != nil {
			return fmt.Errorf("read runtime message: %w", err)
		}
		var header struct {
			Type    string          `json:"type"`
			ID      string          `json:"id"`
			OK      bool            `json:"ok"`
			Error   string          `json:"error"`
			Event   string          `json:"event"`
			RunID   string          `json:"run_id"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(raw, &header); err != nil {
			return fmt.Errorf("decode runtime message: %w", err)
		}

		switch header.Type {
		case "res":
			if header.ID != reqID {
				continue
			}
			if !header.OK {
				return fmt.Errorf("message rejected: %s", header.Error)
			}
			var ack protocol.AckPayload
			if err := json.Unmarshal(header.Payload, &ack); err == nil {
				runID = ack.RunID
			}
		case "event":
			// Only render events for the run this turn started; the session may
			// also carry other runs' events (e.g. a parallel reviewer flow).
			if runID != "" && header.RunID != "" && header.RunID != runID {
				continue
			}
			if verbosity == chatVerbose {
				breakLine()
				fmt.Fprintf(out, "· %s %s\n", header.Event, strings.TrimSpace(string(header.Payload)))
			}
			switch header.Event {
			case string(domain.EventAssistantDelta):
				var delta struct {
					Delta string `json:"delta"`
				}
				if err := json.Unmarshal(header.Payload, &delta); err != nil || delta.Delta == "" {
					continue
				}
				if !midLine {
					fmt.Fprint(out, "orbis> ")
					midLine = true
				}
				fmt.Fprint(out, delta.Delta)
				sawDelta = true
			case string(domain.EventFinalAnswerEmitted):
				if !sawDelta {
					var final struct {
						Text string `json:"text"`
					}
					if err := json.Unmarshal(header.Payload, &final); err == nil && final.Text != "" {
						fmt.Fprintf(out, "orbis> %s", final.Text)
						midLine = true
					}
				}
			case string(domain.EventToolCallStarted):
				if verbosity == chatMedium {
					var tool struct {
						Name string          `json:"tool_name"`
						Args json.RawMessage `json:"args"`
					}
					_ = json.Unmarshal(header.Payload, &tool)
					breakLine()
					fmt.Fprintf(out, "[tool] %s %s\n", tool.Name, strings.TrimSpace(string(tool.Args)))
				}
			case string(domain.EventToolCallSucceeded):
				if verbosity == chatMedium {
					var tool struct {
						Name       string `json:"tool_name"`
						DurationMS int64  `json:"duration_ms"`
					}
					_ = json.Unmarshal(header.Payload, &tool)
					breakLine()
					fmt.Fprintf(out, "[tool] %s ok (%dms)\n", tool.Name, tool.DurationMS)
				}
			case string(domain.EventToolCallFailed), string(domain.EventToolCallRejected), string(domain.EventToolCallTimedOut):
				if verbosity != chatQuiet {
					var tool struct {
						Name  string `json:"tool_name"`
						Error string `json:"error"`
					}
					_ = json.Unmarshal(header.Payload, &tool)
					breakLine()
					fmt.Fprintf(out, "[tool] %s %s: %s\n", tool.Name, strings.TrimPrefix(header.Event, "ToolCall"), tool.Error)
				}
			case string(domain.EventSkillApplied):
				if verbosity == chatMedium {
					var applied struct {
						SkillIDs []string `json:"skill_ids"`
					}
					_ = json.Unmarshal(header.Payload, &applied)
					breakLine()
					fmt.Fprintf(out, "[skill] %s\n", strings.Join(applied.SkillIDs, ", "))
				}
			case string(domain.EventRunCompleted):
				breakLine()
				return nil
			case string(domain.EventRunFailed):
				var failure struct {
					Error string `json:"error"`
				}
				_ = json.Unmarshal(header.Payload, &failure)
				breakLine()
				fmt.Fprintf(out, "[error] run failed: %s\n", failure.Error)
				return nil
			}
		}
	}
}
