package traffic

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	actool "github.com/Autumn-27/norma/tool"
)

func seedIdentityExchange(t *testing.T, tr *Traffic, id, method, host, target string, status int, responseHeaders, responseBody string) {
	t.Helper()
	requestHead := method + " " + target + " HTTP/1.1\nHost: " + host + "\nAuthorization: Bearer secret-" + id + "\n"
	responseHead := "HTTP/1.1 " + strconv.Itoa(status) + " Status\n" + responseHeaders
	if _, err := tr.DB().Exec(`INSERT INTO exchanges(id,ts,host,method,url_template,url,status,content_type,req_len,resp_len,path)
VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		id, 1, host, method, target, "http://"+host+target, status, "application/json", len(requestHead), len(responseBody), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.DB().Exec(`INSERT INTO exchange_bodies(id,req_head,req_body,resp_head,resp_body)
VALUES(?,?,?,?,?)`,
		id, requestHead, []byte(nil), responseHead, []byte(responseBody)); err != nil {
		t.Fatal(err)
	}
}

func findTrafficTool(t *testing.T, tr *Traffic, name string) actool.CoreTool {
	t.Helper()
	for _, tool := range tr.Tools() {
		if tool.Name() == name {
			return tool
		}
	}
	t.Fatalf("tool %q is not registered", name)
	return nil
}

func TestDiffIdentityToolIsRegistered(t *testing.T) {
	tool := findTrafficTool(t, &Traffic{}, "diff_identity")
	if tool.Description() == "" {
		t.Fatal("diff_identity has no description")
	}
	schema := tool.InputSchema()
	required, _ := schema["required"].([]any)
	if len(required) != 2 {
		t.Fatalf("unexpected required fields: %#v", schema["required"])
	}
}

func TestDiffIdentityCandidateBOLA(t *testing.T) {
	tr, err := Open(t.TempDir(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()

	seedIdentityExchange(t, tr, "1-0001", "GET", "app.example", "/api/users/7", 200,
		"Content-Type: application/json\n", `{"id":7,"email":"alice@example.test"}`)
	seedIdentityExchange(t, tr, "1-0002", "GET", "app.example", "/api/users/7", 200,
		"Content-Type: application/json\n", `{"id":7,"email":"bob@example.test"}`)

	tool := findTrafficTool(t, tr, "diff_identity")
	result, err := tool.Call(context.Background(), json.RawMessage(`{"baseline_id":"1-0001","comparison_id":"1-0002"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("tool returned an error: %s", result.Flatten())
	}
	text := result.Flatten()
	if !strings.Contains(text, `"verdict":"candidate_bola"`) {
		t.Fatalf("unexpected result: %s", text)
	}
	if strings.Contains(text, "secret-") || strings.Contains(text, "Authorization") {
		t.Fatalf("result leaked request credentials: %s", text)
	}
}

func TestDiffIdentityEnforced(t *testing.T) {
	tr, err := Open(t.TempDir(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()

	seedIdentityExchange(t, tr, "1-0001", "GET", "app.example", "/api/users/7", 200,
		"Content-Type: application/json\n", `{"id":7}`)
	seedIdentityExchange(t, tr, "1-0002", "GET", "app.example", "/api/users/7", 403,
		"Content-Type: application/json\n", `{"error":"forbidden"}`)

	tool := findTrafficTool(t, tr, "diff_identity")
	result, err := tool.Call(context.Background(), json.RawMessage(`{"baseline_id":"1-0001","comparison_id":"1-0002"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("tool returned an error: %s", result.Flatten())
	}
	if !strings.Contains(result.Flatten(), `"verdict":"enforced"`) {
		t.Fatalf("unexpected result: %s", result.Flatten())
	}
}

func TestDiffIdentityRejectsMissingIDs(t *testing.T) {
	tr, err := Open(t.TempDir(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()

	tool := findTrafficTool(t, tr, "diff_identity")
	result, err := tool.Call(context.Background(), json.RawMessage(`{"baseline_id":"1-0001"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected an error result for a missing comparison_id")
	}
}
