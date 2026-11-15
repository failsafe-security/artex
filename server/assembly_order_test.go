package server

import (
	"context"
	"reflect"
	"testing"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	actool "github.com/Autumn-27/norma/tool"
)

func TestDeferredHostToolsOrderDeterministic(t *testing.T) {
	dsn, _, err := db.DSN()
	if err != nil {
		t.Skipf("no database config (%v) — skipping", err)
	}
	pg, err := db.Open(dsn)
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	defer pg.Close()

	names := []string{"t_zeta", "t_alpha", "t_mu", "t_beta", "t_omega", "t_kappa"}
	binds := map[string][]string{}
	for _, n := range names {
		binds[n] = []string{"planner"}
	}
	binds["t_other"] = []string{"worker"}
	wireAgentAugment(pg, t.TempDir(), func() ([]actool.CoreTool, map[string][]string) { return nil, binds })
	t.Cleanup(func() { agent.ToolAugment = nil })

	want := []string{"t_alpha", "t_beta", "t_kappa", "t_mu", "t_omega", "t_zeta"}
	for range 20 {
		_, def, cleanup := agent.ToolAugment(context.Background(), "planner")
		cleanup()
		if !reflect.DeepEqual(def.GlobalNames, want) {
			t.Fatalf("GlobalNames = %v, want %v", def.GlobalNames, want)
		}
		tail := def.Deferred[len(def.Deferred)-len(want):]
		if !reflect.DeepEqual(tail, want) {
			t.Fatalf("Deferred tail = %v, want %v", tail, want)
		}
	}
}
