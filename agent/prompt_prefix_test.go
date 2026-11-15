package agent

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
)

func workerTailFor(runDir string, intent *db.Node) string {
	return workerArtifactSpec(runDir) + renderIntentTask(intent) + "\n\n开始执行上面这条意图：只做它、只产生事实、assets、finding、做完即停。"
}

func TestWorkerSystemHasNoPerIntentData(t *testing.T) {
	orig := PromptOverride
	PromptOverride = nil
	t.Cleanup(func() { PromptOverride = orig })

	a := workerSystem("127.0.0.1:8080", "/ca.pem", "/data")
	b := workerSystem("127.0.0.1:8080", "/ca.pem", "/data")
	if a != b {
		t.Fatalf("worker system body not deterministic across builds")
	}
	if strings.Contains(a, "/data/tasks/") {
		t.Fatalf("worker system body must not carry the per-intent run dir: %q", a)
	}
}

func TestWorkerSystemPrefixStableAcrossIntents(t *testing.T) {
	orig := PromptOverride
	PromptOverride = nil
	t.Cleanup(func() { PromptOverride = orig })

	def := DeferredInfo{
		FindingGuidance: "\n\nFINDING GUIDANCE",
		Deferred:        []string{"mcp__b__x", "mcp__a__y"},
		GlobalNames:     []string{"mcp__b__x", "mcp__a__y"},
	}
	base := workerSystem("127.0.0.1:8080", "/ca.pem", "/data")
	i1 := &db.Node{ID: 11, Payload: json.RawMessage(`{"summary":"probe login"}`)}
	i2 := &db.Node{ID: 22, Payload: json.RawMessage(`{"summary":"probe upload"}`)}
	s1, b1 := deferredSystemWithTail(base, workerTailFor("/data/tasks/1/i11", i1), def)
	s2, b2 := deferredSystemWithTail(base, workerTailFor("/data/tasks/1/i22", i2), def)

	if len(s1) != 3 || len(s2) != 3 {
		t.Fatalf("want [body, deferred block, intent tail], got %d and %d segments", len(s1), len(s2))
	}
	if !reflect.DeepEqual(s1[:2], s2[:2]) {
		t.Fatalf("static segments differ between intents")
	}
	if s1[2] == s2[2] {
		t.Fatalf("intent tails should differ")
	}
	if b1 != 2 || b2 != 2 {
		t.Fatalf("boundary should split static prefix from intent tail, got %d/%d", b1, b2)
	}
	for _, seg := range s1[:2] {
		if strings.Contains(seg, "/data/tasks/1/i11") || strings.Contains(seg, "probe login") {
			t.Fatalf("per-intent data leaked into static prefix: %q", seg)
		}
	}
	if !strings.Contains(s1[2], "/data/tasks/1/i11") || !strings.Contains(s1[2], "probe login") || !strings.Contains(s1[2], "意图 id: 11") {
		t.Fatalf("intent tail missing per-intent content: %q", s1[2])
	}
}

func TestDeferredSystemWithTailEmptyTailMatchesDeferredSystem(t *testing.T) {
	def := DeferredInfo{GlobalNames: []string{"mcp__a__y"}}
	got, gb := deferredSystemWithTail("SYS", "", def)
	want, wb := deferredSystem("SYS", def)
	if !reflect.DeepEqual(got, want) || gb != wb {
		t.Fatalf("empty tail changed output: %v,%d vs %v,%d", got, gb, want, wb)
	}
	got, gb = deferredSystemWithTail("SYS", "TAIL", DeferredInfo{})
	if !reflect.DeepEqual(got, []string{"SYS", "TAIL"}) || gb != 0 {
		t.Fatalf("no deferred block: got %v,%d", got, gb)
	}
}
