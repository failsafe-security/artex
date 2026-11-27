package oast

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	actool "github.com/Autumn-27/norma/tool"
)

func testDB(t *testing.T) *db.DB {
	t.Helper()
	dsn := os.Getenv("ARTEX_PG_DSN")
	if dsn == "" {
		t.Skip("ARTEX_PG_DSN is not set; skipping OAST database test")
	}
	store, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.EnsureOASTTables(); err != nil {
		t.Fatalf("ensure OAST tables: %v", err)
	}
	return store
}

func cleanupToken(t *testing.T, store *db.DB, token string) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = store.Exec(`DELETE FROM oast_interactions WHERE token=$1`, token)
		_, _ = store.Exec(`DELETE FROM oast_tokens WHERE token=$1`, token)
	})
}

func openTestOAST(t *testing.T, cfg Config) (*OAST, *db.DB) {
	t.Helper()
	store := testDB(t)
	if cfg.Bind == "" {
		cfg.Bind = "127.0.0.1:0"
	}
	o, err := Open(store, cfg)
	if err != nil {
		t.Fatalf("open OAST: %v", err)
	}
	t.Cleanup(func() { _ = o.Close() })
	return o, store
}

func TestRandomTokenFormat(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		token, err := randomToken()
		if err != nil {
			t.Fatal(err)
		}
		if !validToken(token) {
			t.Fatalf("invalid token %q", token)
		}
		if seen[token] {
			t.Fatalf("duplicate token %q", token)
		}
		seen[token] = true
	}
	for _, bad := range []string{"", "short", "AAAAAAAAAAAA", "abcdefghijk1", "abc12345678"} {
		if validToken(bad) {
			t.Fatalf("accepted malformed token %q", bad)
		}
	}
}

func TestTokenFromRequest(t *testing.T) {
	token, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		target string
		host   string
		want   string
	}{
		{"path", "http://example.test/t/" + token + "/cb", "example.test", token},
		{"path query", "http://example.test/t/" + token + "?x=1", "example.test", token},
		{"host label", "http://example.test/", token + ".oast.example.test", token},
		{"host with port", "http://example.test/", token + ".oast.example.test:8080", token},
		{"root path", "http://example.test/", "example.test", ""},
		{"unknown path", "http://example.test/hello", "example.test", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tc.target, nil)
			r.Host = tc.host
			if got := tokenFromRequest(r); got != tc.want {
				t.Fatalf("tokenFromRequest=%q want %q", got, tc.want)
			}
		})
	}
}

func TestValidateConfigRejectsNonLoopback(t *testing.T) {
	cfg := Config{Bind: "0.0.0.0:0"}
	if err := validateConfig(&cfg); err == nil {
		t.Fatal("expected a non-loopback bind to be rejected")
	}
	for _, bind := range []string{"127.0.0.1:0", "[::1]:0", "localhost:0"} {
		cfg = Config{Bind: bind}
		if err := validateConfig(&cfg); err != nil {
			t.Fatalf("bind %s: %v", bind, err)
		}
	}
	if err := validateConfig(&Config{Bind: "127.0.0.1:0", PublicURL: "ftp://example.test"}); err == nil {
		t.Fatal("expected a non-http public URL to be rejected")
	}
	if err := validateConfig(&Config{Bind: "127.0.0.1:0", Zone: "not-a-zone"}); err == nil {
		t.Fatal("expected an invalid zone to be rejected")
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv("ARTEX_OAST", "")
	if _, ok := FromEnv(); ok {
		t.Fatal("OAST should be off by default")
	}
	t.Setenv("ARTEX_OAST", "1")
	t.Setenv("ARTEX_OAST_BIND", "")
	cfg, ok := FromEnv()
	if !ok {
		t.Fatal("expected OAST to be enabled")
	}
	if cfg.Bind != "127.0.0.1:0" {
		t.Fatalf("default bind=%q", cfg.Bind)
	}
}

func TestRateLimiter(t *testing.T) {
	l := newRateLimiter(3, 2)
	now := time.Unix(100, 0)
	if !l.allow("a", now) || !l.allow("a", now) {
		t.Fatal("per-token budget should allow two")
	}
	if l.allow("a", now) {
		t.Fatal("per-token budget should reject the third")
	}
	if !l.allow("b", now) {
		t.Fatal("global budget should still allow another token")
	}
	if l.allow("c", now) {
		t.Fatal("global budget should reject the fourth request")
	}
	if !l.allow("a", now.Add(time.Second)) {
		t.Fatal("a new second should reset the budget")
	}
}

func TestListenerRecordsKnownToken(t *testing.T) {
	o, store := openTestOAST(t, Config{})
	tok, err := o.Mint(42, "ssrf payload", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cleanupToken(t, store, tok.Token)
	if !strings.HasPrefix(tok.CallbackURL, "http://127.0.0.1:") {
		t.Fatalf("unexpected callback URL %q", tok.CallbackURL)
	}

	req, err := http.NewRequest(http.MethodPost, tok.CallbackURL+"/collect?x=1", strings.NewReader("hello callback"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("User-Agent", "oast-test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "ok\n" {
		t.Fatalf("status=%d body=%q", resp.StatusCode, body)
	}

	items, err := o.Poll(42, tok.Token, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("interactions=%d", len(items))
	}
	got := items[0]
	if got.Proto != "http" || got.Method != http.MethodPost || got.TaskID != 42 || got.Body != "hello callback" {
		t.Fatalf("unexpected interaction: %+v", got)
	}
	if !strings.HasPrefix(got.Path, "/t/"+tok.Token) || !strings.Contains(got.Path, "x=1") {
		t.Fatalf("path=%q", got.Path)
	}
	if got.UserAgent != "oast-test" {
		t.Fatalf("user agent=%q", got.UserAgent)
	}

	// A repeated poll with after_id returns only newer rows.
	more, err := o.Poll(42, tok.Token, got.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(more) != 0 {
		t.Fatalf("expected no new interactions, got %d", len(more))
	}

	if _, err := o.Poll(99, tok.Token, 0, 10); err == nil {
		t.Fatal("expected cross-task polling to be rejected")
	}
}

func TestListenerIgnoresUnknownAndExpiredTokens(t *testing.T) {
	o, store := openTestOAST(t, Config{})
	unknown, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}

	expired, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	cleanupToken(t, store, expired)
	if err := store.InsertOASTToken(expired, 7, "expired", time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}

	for _, token := range []string{unknown, expired} {
		resp, err := http.Get("http://" + o.Addr() + "/t/" + token)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("token %s: status=%d", token, resp.StatusCode)
		}
	}
	for _, token := range []string{unknown, expired} {
		items, err := store.ListOASTInteractions(token, 0, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 0 {
			t.Fatalf("token %s recorded %d interactions", token, len(items))
		}
	}
}

func TestListenerCapsBody(t *testing.T) {
	o, store := openTestOAST(t, Config{})
	tok, err := o.Mint(1, "", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cleanupToken(t, store, tok.Token)

	big := strings.Repeat("A", maxBodyBytes+4096)
	resp, err := http.Post(tok.CallbackURL, "text/plain", strings.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	items, err := o.Poll(1, tok.Token, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("interactions=%d", len(items))
	}
	if len(items[0].Body) != maxBodyBytes {
		t.Fatalf("body len=%d want %d", len(items[0].Body), maxBodyBytes)
	}
}

func TestHostStyleCallback(t *testing.T) {
	o, store := openTestOAST(t, Config{Zone: "oast.example.test"})
	tok, err := o.Mint(1, "", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cleanupToken(t, store, tok.Token)
	if tok.CallbackHost != tok.Token+".oast.example.test" {
		t.Fatalf("callback host=%q", tok.CallbackHost)
	}

	req, err := http.NewRequest(http.MethodGet, "http://"+o.Addr()+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = tok.CallbackHost
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	items, err := o.Poll(1, tok.Token, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Host != tok.CallbackHost {
		t.Fatalf("unexpected interactions: %+v", items)
	}
}

func TestToolsMintAndPoll(t *testing.T) {
	o, store := openTestOAST(t, Config{})
	ctx := agent.WithRunInfo(context.Background(), agent.RunInfo{TaskID: 11})

	var mint, poll actool.CoreTool
	for _, tool := range o.Tools() {
		switch tool.Name() {
		case "oast_token":
			mint = tool
		case "oast_poll":
			poll = tool
		}
	}
	if mint == nil || poll == nil {
		t.Fatal("expected oast_token and oast_poll to be registered")
	}
	if mint.IsReadOnly(nil) {
		t.Fatal("oast_token should not be read-only")
	}
	if !poll.IsReadOnly(nil) {
		t.Fatal("oast_poll should be read-only")
	}

	res, err := mint.Call(ctx, json.RawMessage(`{"label":"blind ssrf"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("mint failed: %s", res.Flatten())
	}
	var tok Token
	if err := json.Unmarshal([]byte(res.Flatten()), &tok); err != nil {
		t.Fatalf("decode mint result: %v (%s)", err, res.Flatten())
	}
	cleanupToken(t, store, tok.Token)

	resp, err := http.Get(tok.CallbackURL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	res, err = poll.Call(ctx, json.RawMessage(`{"token":"`+tok.Token+`"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("poll failed: %s", res.Flatten())
	}
	var out struct {
		Count        int   `json:"count"`
		NextAfterID  int64 `json:"next_after_id"`
		Interactions []struct {
			ID int64 `json:"id"`
		} `json:"interactions"`
	}
	if err := json.Unmarshal([]byte(res.Flatten()), &out); err != nil {
		t.Fatalf("decode poll result: %v (%s)", err, res.Flatten())
	}
	if out.Count != 1 || len(out.Interactions) != 1 || out.NextAfterID != out.Interactions[0].ID {
		t.Fatalf("unexpected poll result: %s", res.Flatten())
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	o, _ := openTestOAST(t, Config{})
	if err := o.Close(); err != nil {
		t.Fatal(err)
	}
	if err := o.Close(); err != nil {
		t.Fatal(err)
	}
}
