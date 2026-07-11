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
	"github.com/sangjinsu/orbis/internal/protocol"
	"github.com/spf13/cobra"
)

type watchConfig struct {
	URL string
	// JSON prints each RuntimeEvent as one raw NDJSON line.
	JSON bool
	// Timeout stops watching after the duration; 0 watches until interrupted.
	Timeout time.Duration
}

// newWatchCmd builds `orbis watch`. The global feed is not auth-gated, so
// there is no --token flag.
func newWatchCmd() *cobra.Command {
	var addr string
	var asJSON bool
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Stream the global skill lifecycle feed",
		Args:  exactArgs(0, "orbis watch"),
		RunE: func(c *cobra.Command, _ []string) error {
			return runWatch(c.Context(), watchConfig{
				URL:     wsURLFromAddr(resolveAddr(addr)),
				JSON:    asJSON,
				Timeout: timeout,
			}, c.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "", "server address (default $ORBIS_ADDR or :8080)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print raw runtime events as NDJSON")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "stop watching after this duration (0 = until Ctrl-C)")
	return cmd
}

// runWatch subscribes to the session-independent global feed (the eleven
// skill-learning lifecycle events) and streams them until the context ends.
func runWatch(ctx context.Context, cfg watchConfig, out io.Writer) error {
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	conn, _, err := websocket.Dial(ctx, cfg.URL, nil)
	if err != nil {
		return fmt.Errorf("connect websocket: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	params, err := json.Marshal(map[string]string{"scope": "global"})
	if err != nil {
		return fmt.Errorf("marshal subscribe params: %w", err)
	}
	if err := wsjson.Write(ctx, conn, protocol.ClientRequest{Type: "req", ID: "watch_sub", Method: "session.subscribe", Params: params}); err != nil {
		return fmt.Errorf("send subscribe: %w", err)
	}
	var ack protocol.ServerResponse
	if err := wsjson.Read(ctx, conn, &ack); err != nil {
		return fmt.Errorf("read subscribe ack: %w", err)
	}
	if !ack.OK {
		return fmt.Errorf("subscribe rejected: %s", ack.Error)
	}
	fmt.Fprintf(out, "watching the global skill feed at %s (Ctrl-C to stop)\n", cfg.URL)

	for {
		var raw json.RawMessage
		if err := wsjson.Read(ctx, conn, &raw); err != nil {
			// Interrupt or -timeout ends the stream; that is a normal exit.
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read event: %w", err)
		}
		if cfg.JSON {
			printRawJSON(out, raw)
			continue
		}
		var event protocol.RuntimeEvent
		if err := json.Unmarshal(raw, &event); err != nil || event.Event == "" {
			fmt.Fprintf(out, "unknown: %s\n", strings.TrimSpace(string(raw)))
			continue
		}
		payload := strings.TrimSpace(string(event.Payload))
		fmt.Fprintf(out, "%-26s seq=%d session=%s run=%s %s\n", event.Event, event.Seq, event.SessionID, event.RunID, payload)
	}
}
