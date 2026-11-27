package server

import (
	"encoding/json"
	"log"

	"github.com/Autumn-27/artex/agent"
)

// refreshScopeGuardToolText pushes the scope-guard wording of the three scope tools
// into already-seeded rows, once. SeedTool is first-insert-only and the model reads a
// tool's description from its DB row, so on an upgraded database the planner would
// otherwise keep being told that add_task_scope can bring in "a whole company" while
// the handler rejects exactly that. Agent bindings and enabled flags are preserved.
func (s *Server) refreshScopeGuardToolText() {
	const flag = "tool_text_scope_guard_v1"
	if v, _, _ := s.m.pg.GetSetting(flag); v == "true" {
		return
	}
	guarded := map[string]bool{"add_task_scope": true, "add_company_scope": true, "list_companies": true}
	for _, seed := range agent.BuiltinToolSeeds() {
		if !guarded[seed.Key] {
			continue
		}
		schema, _ := json.Marshal(seed.Schema)
		if err := s.m.pg.RefreshToolDefaults(seed.Key, seed.Desc, schema); err != nil {
			log.Printf("[tools] refresh %s for the scope guard failed: %v", seed.Key, err)
			return // leave the flag unset so the next start retries
		}
	}
	_ = s.m.pg.SetSetting(flag, "true")
}
