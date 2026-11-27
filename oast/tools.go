package oast

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

const maxToolBody = 8 << 10

// Tools exposes the OAST fabric to agents. oast_token mints a callback token
// for the current task; oast_poll returns callbacks already received for it.
// Polling never changes state, so a repeated poll is safe.
func (o *OAST) Tools() []actool.CoreTool {
	allow := func(context.Context, json.RawMessage, permission.Context) permission.Decision {
		return permission.Allowed()
	}
	readOnly := func(json.RawMessage) bool { return true }
	concurrent := func(json.RawMessage) bool { return true }

	mint := actool.Build(actool.Spec{
		Name: "oast_token",
		Description: "Mint a unique out-of-band callback token for the current task. Inject the returned " +
			"callback_url (or callback_host when present) into a payload that makes the target issue an " +
			"HTTP request — blind SSRF, XXE, blind SQLi/RCE that supports HTTP exfiltration. Then call " +
			"oast_poll with the token to check for interactions. The token is bound to this task and " +
			"expires; unknown or expired callbacks are not recorded.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"label": map[string]any{"type": "string", "description": "optional note describing where the token will be injected"},
				"ttl_minutes": map[string]any{
					"type":        "integer",
					"description": "token lifetime in minutes, default 60, maximum 1440",
				},
			},
		},
		ReadOnly:    func(json.RawMessage) bool { return false },
		Concurrent:  concurrent,
		Permissions: allow,
		Run: func(ctx context.Context, in json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			var a struct {
				Label      string `json:"label"`
				TTLMinutes int    `json:"ttl_minutes"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf(fmt.Sprintf("decode arguments: %v", err)), nil
			}
			ttl := time.Duration(a.TTLMinutes) * time.Minute
			if a.TTLMinutes <= 0 {
				ttl = defaultTTL
			}
			taskID := agent.RunInfoFrom(ctx).TaskID
			tok, err := o.Mint(taskID, a.Label, ttl)
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			return actool.Text(marshal(tok)), nil
		},
	})

	poll := actool.Build(actool.Spec{
		Name: "oast_poll",
		Description: "List out-of-band callbacks already received for a token minted by oast_token. " +
			"Read-only and safe to repeat: pass the previous next_after_id to fetch only new callbacks. " +
			"An interaction proves the target made the request; correlate it with the injected payload " +
			"before recording a fact or finding. Callback bodies are previews, capped at 8 KiB.",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"token":    map[string]any{"type": "string", "description": "token returned by oast_token"},
				"after_id": map[string]any{"type": "integer", "description": "return interactions with id greater than this; use next_after_id from the previous poll"},
				"limit":    map[string]any{"type": "integer", "description": "maximum interactions to return, default 20, max 100"},
			},
			"required": []any{"token"},
		},
		ReadOnly:    readOnly,
		Concurrent:  concurrent,
		Permissions: allow,
		Run: func(ctx context.Context, in json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			var a struct {
				Token   string `json:"token"`
				AfterID int64  `json:"after_id"`
				Limit   int    `json:"limit"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf(fmt.Sprintf("decode arguments: %v", err)), nil
			}
			if a.Limit <= 0 {
				a.Limit = 20
			}
			taskID := agent.RunInfoFrom(ctx).TaskID
			items, err := o.Poll(taskID, strings.TrimSpace(a.Token), a.AfterID, a.Limit)
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			type view struct {
				db.OASTInteraction
				BodyTruncated bool `json:"body_truncated,omitempty"`
			}
			out := make([]view, 0, len(items))
			next := a.AfterID
			for _, item := range items {
				v := view{OASTInteraction: item}
				if len(v.Body) > maxToolBody {
					v.Body = v.Body[:maxToolBody]
					v.BodyTruncated = true
				}
				out = append(out, v)
				if item.ID > next {
					next = item.ID
				}
			}
			return actool.Text(marshal(map[string]any{
				"token":         a.Token,
				"interactions":  out,
				"count":         len(out),
				"next_after_id": next,
			})), nil
		},
	})

	return []actool.CoreTool{mint, poll}
}

// SeedToolMetas returns the OAST tools built on a zero receiver, for seeding the
// tool catalog. Callers must only use the returned metadata, never Call.
func SeedToolMetas() []actool.CoreTool { return (&OAST{}).Tools() }

func marshal(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"error":"encode result"}`
	}
	return string(b)
}
