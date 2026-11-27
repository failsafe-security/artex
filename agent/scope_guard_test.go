package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
	actool "github.com/Autumn-27/norma/tool"
)

func callToolText(t *testing.T, tool actool.CoreTool, input any) string {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	result, err := tool.Call(context.Background(), raw, nil)
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	return result.Flatten()
}

func decodeToolJSON(t *testing.T, text string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("decode tool result: %v; raw=%s", err, text)
	}
	return out
}

// A task's agents share one asset store with every other task. They must not be able
// to see another engagement's company, nor pull it (or its domains) into their own
// test scope; scope may only grow from what the task was given or found itself.
func TestTaskAgentCannotPullOtherCompanyIntoScope(t *testing.T) {
	d := testDB(t)
	defer d.Close()
	companies, assets := d.Companies(), d.Assets()
	stamp := time.Now().UnixNano()

	ownID, _, err := companies.UpsertCompany(fmt.Sprintf("guard-own-%d", stamp), "")
	if err != nil {
		t.Fatal(err)
	}
	otherName := fmt.Sprintf("guard-other-%d", stamp)
	otherID, _, err := companies.UpsertCompany(otherName, "")
	if err != nil {
		t.Fatal(err)
	}
	otherDomain := fmt.Sprintf("guard-other-%d.invalid", stamp)
	if added, _, _, errs := companies.AddScope(otherID, []string{otherDomain}, "other engagement"); added != 1 {
		t.Fatalf("seed other company scope: %v", errs)
	}
	otherAssetID, err := assets.UpsertRootDomain(db.UpsertRootDomainReq{Domain: otherDomain})
	if err != nil {
		t.Fatal(err)
	}

	namedHost := fmt.Sprintf("app.guard-target-%d.invalid", stamp)
	namedRoot := fmt.Sprintf("guard-target-%d.invalid", stamp)
	task, err := d.CreateTaskWithOptions("Test https://"+namedHost+"/login only", "find an injection point", db.TaskCreateOptions{
		CompanyIDs: []int64{ownID},
	})
	if err != nil {
		t.Fatal(err)
	}

	// What a Worker of this task actually found and recorded.
	foundHost := fmt.Sprintf("api.guard-found-%d.invalid", stamp)
	foundRoot := fmt.Sprintf("guard-found-%d.invalid", stamp)
	foundHostID, err := assets.UpsertSubdomain(db.UpsertSubdomainReq{Domain: foundHost, TaskID: task.ID})
	if err != nil {
		t.Fatal(err)
	}
	foundIP := fmt.Sprintf("198.51.%d.7", stamp%200)
	foundNet := fmt.Sprintf("198.51.%d.0/24", stamp%200)
	foundIPID, err := assets.UpsertIP(db.UpsertIPReq{IP: foundIP, TaskID: task.ID})
	if err != nil {
		t.Fatal(err)
	}
	newCompany := fmt.Sprintf("guard-new-%d", stamp)
	t.Cleanup(func() {
		_ = d.DeleteTask(task.ID)
		_, _ = assets.DeleteByIDs([]int64{otherAssetID, foundHostID, foundIPID})
		_ = companies.DeleteCompany(ownID)
		_ = companies.DeleteCompany(otherID)
		if c, _ := companies.GetCompanyByName(newCompany); c != nil {
			_ = companies.DeleteCompany(c.ID)
		}
	})

	tools := NewToolSet(d.Exploration(task.ExplorationID), "planner")
	tools.SetTaskID(task.ID)
	tools.SetAssetStore(assets, companies)

	visibleCompanies := func() map[float64]bool {
		t.Helper()
		out := map[float64]bool{}
		listed := decodeToolJSON(t, callToolText(t, tools.listCompanies(), map[string]any{}))
		for _, row := range listed["companies"].([]any) {
			out[row.(map[string]any)["id"].(float64)] = true
		}
		return out
	}
	scopeAdd := func(kind, value string) (added bool, reason string) {
		t.Helper()
		out := decodeToolJSON(t, callToolText(t, tools.addTaskScope(), map[string]any{"kind": kind, "value": value, "reason": "test"}))
		if errs, ok := out["errors"].(map[string]any); ok && len(errs) > 0 {
			return false, fmt.Sprint(errs["0"])
		}
		rows, _ := out["added"].([]any)
		return len(rows) == 1, ""
	}

	// Visibility: only the company the operator linked to the task.
	if seen := visibleCompanies(); len(seen) != 1 || !seen[float64(ownID)] {
		t.Fatalf("list_companies leaked other companies to a task agent: %v", seen)
	}

	// A company can never be added by an agent, by name or by id, and a company that
	// does not exist gets the same answer (no existence oracle).
	for _, value := range []string{otherName, fmt.Sprint(otherID), fmt.Sprintf("guard-missing-%d", stamp)} {
		if ok, reason := scopeAdd("company", value); ok || !strings.Contains(reason, "cannot add a company") {
			t.Fatalf("company %q accepted or wrong refusal: ok=%v reason=%q", value, ok, reason)
		}
	}
	// Nor can its domain be pulled in just because it sits in the shared store.
	for _, kind := range []string{"root_domain", "subdomain"} {
		if ok, reason := scopeAdd(kind, otherDomain); ok || !strings.Contains(reason, "neither named") {
			t.Fatalf("%s %s accepted or wrong refusal: ok=%v reason=%q", kind, otherDomain, ok, reason)
		}
	}
	if ok, _ := scopeAdd("ip", "203.0.113.9"); ok {
		t.Fatal("an address the task never saw was accepted")
	}

	// Named by the operator in the task itself.
	for _, entry := range [][2]string{{"subdomain", namedHost}, {"root_domain", namedRoot}} {
		if ok, reason := scopeAdd(entry[0], entry[1]); !ok {
			t.Fatalf("operator-named %s %s refused: %s", entry[0], entry[1], reason)
		}
	}
	// Found by this task.
	for _, entry := range [][2]string{{"subdomain", foundHost}, {"root_domain", foundRoot}, {"ip", foundIP}, {"cidr", foundNet}} {
		if ok, reason := scopeAdd(entry[0], entry[1]); !ok {
			t.Fatalf("discovered %s %s refused: %s", entry[0], entry[1], reason)
		}
	}
	// One found address does not justify a wide range.
	if ok, reason := scopeAdd("cidr", "198.51.0.0/16"); ok || !strings.Contains(reason, "wider") {
		t.Fatalf("wide range accepted or wrong refusal: ok=%v reason=%q", ok, reason)
	}

	// add_company_scope: not on another company, not to create one, and per line only
	// what the task has a basis for.
	for _, name := range []string{otherName, newCompany} {
		text := callToolText(t, tools.addCompanyScope(), map[string]any{"company": name, "scope": foundRoot, "reason": "test"})
		if !strings.Contains(text, "linked to this task") {
			t.Fatalf("add_company_scope on %q not refused: %s", name, text)
		}
	}
	if c, err := companies.GetCompanyByName(newCompany); err != nil || c != nil {
		t.Fatalf("a task agent created a company: %+v %v", c, err)
	}
	ownName := companiesName(t, companies, ownID)
	mixed := decodeToolJSON(t, callToolText(t, tools.addCompanyScope(), map[string]any{
		"company": ownName, "scope": foundRoot + "\n" + otherDomain, "reason": "test",
	}))
	if mixed["added"] != float64(1) || mixed["rejected"] != float64(1) {
		t.Fatalf("add_company_scope did not filter by task evidence: %v", mixed)
	}

	// insert_assets: an app cannot be attributed to a company outside the task.
	apps := decodeToolJSON(t, callToolText(t, tools.insertAssets(), map[string]any{"assets": []map[string]any{
		{"type": "app", "app_name": fmt.Sprintf("guard-app-other-%d", stamp), "company_id": otherID},
		{"type": "app", "app_name": fmt.Sprintf("guard-app-own-%d", stamp), "company_id": ownID},
	}}))
	appErrors, _ := apps["errors"].([]any)
	appResults, _ := apps["results"].([]any)
	if len(appErrors) != 1 || len(appResults) != 1 || !strings.Contains(fmt.Sprint(appErrors[0]), "not linked to this task") {
		t.Fatalf("app attribution not limited to the task's companies: %v", apps)
	}
	t.Cleanup(func() {
		for _, row := range appResults {
			_, _ = assets.DeleteByIDs([]int64{int64(row.(map[string]any)["id"].(float64))})
		}
	})

	// Nothing of the other company ended up in the task's scope.
	scope, err := assets.ListTaskScope(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range scope {
		if (row.CompanyID != nil && *row.CompanyID == otherID) || row.Domain == otherDomain {
			t.Fatalf("other company leaked into the task scope: %+v", row)
		}
	}

	// The operator is not restricted: a manual link works, and the company then
	// becomes visible to the task's agents.
	if _, err := assets.AddAgentScope(task.ID, "company", fmt.Sprint(otherID), "operator decision", "manual"); err != nil {
		t.Fatalf("manual company link refused: %v", err)
	}
	if seen := visibleCompanies(); len(seen) != 2 || !seen[float64(otherID)] {
		t.Fatalf("operator-linked company not visible: %v", seen)
	}
}

// Outside a task (platform assistants working from a plain conversation) the company
// list is unchanged.
func TestListCompaniesUnfilteredOutsideTask(t *testing.T) {
	d := testDB(t)
	defer d.Close()
	companies := d.Companies()
	name := fmt.Sprintf("guard-global-%d", time.Now().UnixNano())
	id, _, err := companies.UpsertCompany(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = companies.DeleteCompany(id) })

	tools := NewToolSet(nil, "auto")
	tools.SetAssetStore(d.Assets(), companies)
	listed := decodeToolJSON(t, callToolText(t, tools.listCompanies(), map[string]any{"search": name}))
	if rows := listed["companies"].([]any); len(rows) != 1 {
		t.Fatalf("company not listed outside a task: %v", listed)
	}
}
