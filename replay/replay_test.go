package replay

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func requestRaw(method, target string, headers map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s HTTP/1.1\r\n", method, target)
	for name, value := range headers {
		fmt.Fprintf(&b, "%s: %s\r\n", name, value)
	}
	b.WriteString("\r\n")
	return b.String()
}

func responseRaw(status int, headers map[string]string, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "HTTP/1.1 %d %s\r\n", status, statusText(status))
	for name, value := range headers {
		fmt.Fprintf(&b, "%s: %s\r\n", name, value)
	}
	b.WriteString("\r\n")
	b.WriteString(body)
	return b.String()
}

func statusText(status int) string {
	switch status {
	case 200:
		return "OK"
	case 302:
		return "Found"
	case 401:
		return "Unauthorized"
	case 403:
		return "Forbidden"
	case 500:
		return "Internal Server Error"
	default:
		return "Status"
	}
}

func exchange(t *testing.T, id, method, target string, status int, headers map[string]string, body string) Exchange {
	t.Helper()
	got, err := ParseRaw(id, requestRaw(method, target, map[string]string{"Host": "app.example", "Authorization": "Bearer secret-" + id}), responseRaw(status, headers, body))
	if err != nil {
		t.Fatalf("ParseRaw(%s): %v", id, err)
	}
	return got
}

func TestParseRaw(t *testing.T) {
	got, err := ParseRaw("1-0001", requestRaw("get", "/api/users/7?b=2&a=1", map[string]string{"Host": "App.Example:8443"}), responseRaw(200, map[string]string{"Content-Type": "application/json"}, `{"id":7}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Method != "GET" || got.Host != "app.example:8443" || got.Path != "/api/users/7" || got.RawQuery != "b=2&a=1" || got.Status != 200 {
		t.Fatalf("unexpected exchange: %+v", got)
	}
	if string(got.Body) != `{"id":7}` {
		t.Fatalf("body=%q", got.Body)
	}
}

func TestCompareSharedDespiteQueryOrderHeadersAndDynamicFields(t *testing.T) {
	a := exchange(t, "a", "GET", "/api/users?id=1&b=2&request_id=aaa", 200, map[string]string{"Content-Type": "application/json", "ETag": `"one"`}, `{"id":1,"name":"A","ts":100}`)
	b := exchange(t, "b", "GET", "/api/users?b=2&id=1&request_id=bbb", 200, map[string]string{"Content-Type": "application/json", "ETag": `"two"`}, `{"name":"A","id":1,"ts":200}`)

	got := Compare(a, b)
	if got.Verdict != Shared {
		t.Fatalf("verdict=%s reason=%s", got.Verdict, got.Reason)
	}
	if !got.SameEndpoint {
		t.Fatal("expected the same endpoint")
	}
}

func TestCompareCandidateBOLA(t *testing.T) {
	a := exchange(t, "a", "GET", "/api/users/7", 200, map[string]string{"Content-Type": "application/json"}, `{"id":7,"email":"a@example.test"}`)
	b := exchange(t, "b", "GET", "/api/users/7", 200, map[string]string{"Content-Type": "application/json"}, `{"id":7,"email":"b@example.test"}`)

	got := Compare(a, b)
	if got.Verdict != CandidateBOLA {
		t.Fatalf("verdict=%s reason=%s", got.Verdict, got.Reason)
	}
	if got.Baseline.BodySHA256 == got.Comparison.BodySHA256 {
		t.Fatal("candidate bodies should hash differently")
	}
}

func TestCompareEnforced(t *testing.T) {
	a := exchange(t, "a", "GET", "/api/users/7", 200, map[string]string{"Content-Type": "application/json"}, `{"id":7}`)
	b := exchange(t, "b", "GET", "/api/users/7", 403, nil, `forbidden`)

	got := Compare(a, b)
	if got.Verdict != Enforced {
		t.Fatalf("verdict=%s reason=%s", got.Verdict, got.Reason)
	}
}

func TestCompareSessionExpired(t *testing.T) {
	a := exchange(t, "a", "GET", "/account", 200, map[string]string{"Content-Type": "text/html"}, `<h1>Account</h1>`)
	b := exchange(t, "b", "GET", "/account", 302, map[string]string{"Location": "/login?next=/account"}, ``)

	got := Compare(a, b)
	if got.Verdict != SessionExpired {
		t.Fatalf("verdict=%s reason=%s", got.Verdict, got.Reason)
	}
}

func TestCompareLoginPageWithStatus200(t *testing.T) {
	a := exchange(t, "a", "GET", "/account", 200, map[string]string{"Content-Type": "text/html"}, `<h1>Account</h1>`)
	b := exchange(t, "b", "GET", "/account", 200, map[string]string{"Content-Type": "text/html"}, `<h1>Please log in</h1>`)

	got := Compare(a, b)
	if got.Verdict != SessionExpired {
		t.Fatalf("verdict=%s reason=%s", got.Verdict, got.Reason)
	}
}

func TestCompareNotComparable(t *testing.T) {
	a := exchange(t, "a", "GET", "/api/users/7", 200, nil, `one`)
	b := exchange(t, "b", "GET", "/api/users/8", 200, nil, `two`)

	got := Compare(a, b)
	if got.Verdict != NotComparable {
		t.Fatalf("verdict=%s reason=%s", got.Verdict, got.Reason)
	}
}

func TestCompareInconclusiveWhenBaselineFailed(t *testing.T) {
	a := exchange(t, "a", "GET", "/api/users/7", 500, nil, `boom`)
	b := exchange(t, "b", "GET", "/api/users/7", 200, nil, `data`)

	got := Compare(a, b)
	if got.Verdict != Inconclusive {
		t.Fatalf("verdict=%s reason=%s", got.Verdict, got.Reason)
	}
}

func TestCompareTruncatedBodies(t *testing.T) {
	body := strings.Repeat("x", 300) + "\n…[truncated] @blob sha256:abc (len=999999)"
	a := exchange(t, "a", "GET", "/api/users/7", 200, nil, body)
	b := exchange(t, "b", "GET", "/api/users/7", 200, nil, body)
	if !a.BodyTruncated || !b.BodyTruncated {
		t.Fatal("expected both bodies to be marked truncated")
	}
	got := Compare(a, b)
	if got.Verdict != Inconclusive {
		t.Fatalf("verdict=%s reason=%s", got.Verdict, got.Reason)
	}
}

func TestResultDoesNotExposeCredentials(t *testing.T) {
	a := exchange(t, "a", "GET", "/api/users/7", 200, map[string]string{"Set-Cookie": "session=secret", "Content-Type": "application/json"}, `{"id":7}`)
	b := exchange(t, "b", "GET", "/api/users/7", 200, map[string]string{"Content-Type": "application/json"}, `{"id":8}`)

	raw, err := json.Marshal(Compare(a, b))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, secret := range []string{"secret-a", "secret-b", "session=secret", "Authorization", "Set-Cookie"} {
		if strings.Contains(text, secret) {
			t.Fatalf("result leaked %q: %s", secret, text)
		}
	}
}
