// Package replay provides deterministic comparison of already-recorded HTTP
// exchanges captured under different identities.
//
// It is the first landing for the differential identity work in #13: it sends no
// requests itself, so it cannot change target state. The recording proxy has
// already captured both exchanges; this package normalizes them (query order,
// volatile headers, dynamic JSON fields) and classifies the authorization
// outcome in code rather than leaving it to model guesswork.
package replay

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Verdict is the deterministic authorization outcome of comparing two exchanges.
type Verdict string

const (
	// Shared means both identities received the same normalized response.
	Shared Verdict = "shared"
	// CandidateBOLA means the comparison identity received a different 200
	// response for the same endpoint, which is a BOLA/IDOR candidate.
	CandidateBOLA Verdict = "candidate_bola"
	// Enforced means the comparison identity was denied with 401 or 403.
	Enforced Verdict = "enforced"
	// SessionExpired means the comparison identity was redirected or shown a
	// login page instead of the resource.
	SessionExpired Verdict = "session_expired"
	// NotComparable means the two exchanges are not the same endpoint/method.
	NotComparable Verdict = "not_comparable"
	// Inconclusive means the pair did not match any deterministic rule (for
	// example the baseline was not a 200, or a body was truncated).
	Inconclusive Verdict = "inconclusive"
)

// Exchange is one recorded HTTP exchange in the form returned by the traffic
// store. The request body is intentionally not kept: replay comparison uses the
// endpoint and the response, and never needs to expose credentials.
type Exchange struct {
	TrafficID string
	Method    string
	Host      string
	Path      string
	RawQuery  string
	Status    int
	Headers   http.Header
	Body      []byte

	// BodyTruncated is true when the traffic store returned a @blob preview
	// instead of the complete body. A truncated comparison cannot be treated as
	// proof of identical or different content.
	BodyTruncated bool
}

// Side is the safe, secret-free description of one exchange in a Result.
type Side struct {
	TrafficID   string `json:"traffic_id"`
	Method      string `json:"method"`
	Host        string `json:"host,omitempty"`
	Path        string `json:"path"`
	Status      int    `json:"status"`
	ContentType string `json:"content_type,omitempty"`
	BodyKind    string `json:"body_kind"`
	BodySHA256  string `json:"body_sha256"`
	BodyBytes   int    `json:"body_bytes"`
	BodyEmpty   bool   `json:"body_empty"`
	Truncated   bool   `json:"truncated,omitempty"`
}

// Result is the deterministic comparison of a baseline exchange (identity A)
// and a comparison exchange (identity B).
type Result struct {
	Verdict      Verdict  `json:"verdict"`
	Reason       string   `json:"reason"`
	SameEndpoint bool     `json:"same_endpoint"`
	Baseline     Side     `json:"baseline"`
	Comparison   Side     `json:"comparison"`
	Notes        []string `json:"notes,omitempty"`
}

// ParseRaw parses the raw request and response text produced by the traffic
// store (head, blank line, body). Both CRLF and LF are accepted; body bytes are
// left untouched.
func ParseRaw(id, rawRequest, rawResponse string) (Exchange, error) {
	reqHead, _ := splitRaw(rawRequest)
	reqLines := splitHead(reqHead)
	if len(reqLines) == 0 || strings.TrimSpace(reqLines[0]) == "" {
		return Exchange{}, fmt.Errorf("exchange %s: empty request", id)
	}
	reqParts := strings.SplitN(strings.TrimSpace(reqLines[0]), " ", 3)
	if len(reqParts) < 2 {
		return Exchange{}, fmt.Errorf("exchange %s: malformed request line %q", id, reqLines[0])
	}
	method := strings.ToUpper(reqParts[0])
	target := reqParts[1]
	reqHeaders := parseHeaders(reqLines[1:])
	host, path, rawQuery, err := parseTarget(target, reqHeaders.Get("Host"))
	if err != nil {
		return Exchange{}, fmt.Errorf("exchange %s: %w", id, err)
	}

	respHead, respBody := splitRaw(rawResponse)
	respLines := splitHead(respHead)
	if len(respLines) == 0 || strings.TrimSpace(respLines[0]) == "" {
		return Exchange{}, fmt.Errorf("exchange %s: empty response", id)
	}
	status, err := parseStatusLine(respLines[0])
	if err != nil {
		return Exchange{}, fmt.Errorf("exchange %s: %w", id, err)
	}
	respHeaders := parseHeaders(respLines[1:])

	return Exchange{
		TrafficID:     id,
		Method:        method,
		Host:          host,
		Path:          path,
		RawQuery:      rawQuery,
		Status:        status,
		Headers:       respHeaders,
		Body:          []byte(respBody),
		BodyTruncated: looksTruncated(respBody),
	}, nil
}

// Compare classifies two recorded exchanges. The baseline is the response the
// original identity received; the comparison is the response recorded under the
// other identity.
func Compare(baseline, comparison Exchange) Result {
	a := normalize(baseline)
	b := normalize(comparison)

	result := Result{
		SameEndpoint: a.endpointKey() == b.endpointKey(),
		Baseline:     a.side(),
		Comparison:   b.side(),
		Notes: []string{
			"Bodies are compared after JSON canonicalization; volatile response headers and dynamic query/JSON fields are ignored.",
			"Request credentials, cookies and response headers are not included in this result.",
		},
	}
	if !result.SameEndpoint {
		result.Verdict = NotComparable
		result.Reason = "the two exchanges have different methods, hosts, paths or normalized query parameters"
		return result
	}
	if a.truncated || b.truncated {
		if b.status == http.StatusUnauthorized || b.status == http.StatusForbidden {
			result.Verdict = Enforced
			result.Reason = "the comparison identity was denied, although one body was truncated; the status code is conclusive"
			return result
		}
		result.Verdict = Inconclusive
		result.Reason = "at least one response body is truncated; fetch the full body with traffic_blob before judging equality"
		return result
	}
	if a.status != http.StatusOK {
		result.Verdict = Inconclusive
		result.Reason = fmt.Sprintf("baseline status is %d, not 200; the baseline is not a successful authorization reference", a.status)
		return result
	}
	switch {
	case b.status == http.StatusUnauthorized || b.status == http.StatusForbidden:
		result.Verdict = Enforced
		result.Reason = fmt.Sprintf("comparison identity was denied with HTTP %d", b.status)
	case b.status >= 300 && b.status < 400 && b.loginHint:
		result.Verdict = SessionExpired
		result.Reason = "comparison identity was redirected to a login page"
	case b.status != http.StatusOK:
		result.Verdict = Inconclusive
		result.Reason = fmt.Sprintf("comparison status is %d; expected 200, 401, 403 or a login redirect", b.status)
	case b.loginHint && a.bodyHash != b.bodyHash:
		result.Verdict = SessionExpired
		result.Reason = "comparison identity received a login/unauthorized page instead of the resource"
	case a.bodyHash == b.bodyHash:
		result.Verdict = Shared
		result.Reason = "both identities received the same normalized response"
	case b.bodyEmpty && a.bodyEmpty:
		result.Verdict = Shared
		result.Reason = "both identities received an empty 200 response"
	default:
		result.Verdict = CandidateBOLA
		result.Reason = fmt.Sprintf(
			"comparison identity received HTTP 200 with a different %s body (%d bytes) for the same endpoint; baseline had %d bytes",
			b.bodyKind, b.bodyLen, a.bodyLen,
		)
	}
	return result
}

type normalized struct {
	method      string
	host        string
	path        string
	query       string
	status      int
	contentType string
	bodyKind    string
	bodyHash    string
	bodyLen     int
	bodyEmpty   bool
	loginHint   bool
	truncated   bool
	trafficID   string
}

func (n normalized) endpointKey() string {
	return n.method + " " + strings.ToLower(n.host) + " " + n.path + "?" + n.query
}

func (n normalized) side() Side {
	return Side{
		TrafficID:   n.trafficID,
		Method:      n.method,
		Host:        n.host,
		Path:        n.path,
		Status:      n.status,
		ContentType: n.contentType,
		BodyKind:    n.bodyKind,
		BodySHA256:  n.bodyHash,
		BodyBytes:   n.bodyLen,
		BodyEmpty:   n.bodyEmpty,
		Truncated:   n.truncated,
	}
}

func normalize(e Exchange) normalized {
	kind, canonical := canonicalBody(e.Headers.Get("Content-Type"), e.Body)
	sum := sha256.Sum256(canonical)
	return normalized{
		method:      strings.ToUpper(e.Method),
		host:        strings.ToLower(strings.TrimSpace(e.Host)),
		path:        e.Path,
		query:       normalizeQuery(e.RawQuery),
		status:      e.Status,
		contentType: e.Headers.Get("Content-Type"),
		bodyKind:    kind,
		bodyHash:    hex.EncodeToString(sum[:]),
		bodyLen:     len(e.Body),
		bodyEmpty:   len(bytes.TrimSpace(canonical)) == 0,
		loginHint:   looksLikeLogin(e.Status, e.Headers.Get("Location"), canonical),
		truncated:   e.BodyTruncated,
		trafficID:   e.TrafficID,
	}
}

var dynamicQueryParams = map[string]bool{
	"_":              true,
	"_t":             true,
	"cache_bust":     true,
	"cachebuster":    true,
	"cb":             true,
	"correlation_id": true,
	"correlationid":  true,
	"nonce":          true,
	"req_id":         true,
	"request_id":     true,
	"requestid":      true,
	"timestamp":      true,
	"trace_id":       true,
	"traceid":        true,
	"ts":             true,
}

var dynamicJSONKeys = map[string]bool{
	"correlation_id": true,
	"correlationid":  true,
	"nonce":          true,
	"request_id":     true,
	"requestid":      true,
	"timestamp":      true,
	"trace_id":       true,
	"traceid":        true,
	"ts":             true,
}

func normalizeQuery(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return raw
	}
	for key := range values {
		if dynamicQueryParams[strings.ToLower(key)] {
			delete(values, key)
			continue
		}
		sort.Strings(values[key])
	}
	return values.Encode()
}

func canonicalBody(contentType string, body []byte) (kind string, canonical []byte) {
	if len(bytes.TrimSpace(body)) == 0 {
		return "empty", nil
	}
	if !looksJSON(contentType, body) || len(body) > 2<<20 {
		return "opaque", body
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return "opaque", body
	}
	canonical, err := json.Marshal(stripDynamicJSON(value))
	if err != nil {
		return "opaque", body
	}
	return "json", canonical
}

func looksJSON(contentType string, body []byte) bool {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	mediaType = strings.ToLower(mediaType)
	if mediaType == "application/json" || strings.HasSuffix(mediaType, "+json") {
		return true
	}
	trimmed := bytes.TrimSpace(body)
	return len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[')
}

func stripDynamicJSON(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key := range typed {
			if dynamicJSONKeys[strings.ToLower(key)] {
				delete(typed, key)
				continue
			}
			typed[key] = stripDynamicJSON(typed[key])
		}
		return typed
	case []any:
		for i := range typed {
			typed[i] = stripDynamicJSON(typed[i])
		}
		return typed
	default:
		return value
	}
}

func looksLikeLogin(status int, location string, body []byte) bool {
	location = strings.ToLower(location)
	if location != "" {
		for _, marker := range []string{"login", "signin", "sign-in", "auth", "session"} {
			if strings.Contains(location, marker) {
				return true
			}
		}
	}
	if status != http.StatusOK {
		return false
	}
	if len(body) > 64<<10 {
		body = body[:64<<10]
	}
	text := strings.ToLower(string(body))
	for _, marker := range []string{
		"please log in",
		"please login",
		"not logged in",
		"sign in to continue",
		"authentication required",
		"unauthorized",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func splitRaw(raw string) (head, body string) {
	crlf := strings.Index(raw, "\r\n\r\n")
	lf := strings.Index(raw, "\n\n")
	switch {
	case crlf >= 0 && (lf < 0 || crlf <= lf):
		return raw[:crlf], raw[crlf+4:]
	case lf >= 0:
		return raw[:lf], raw[lf+2:]
	default:
		return raw, ""
	}
}

func splitHead(head string) []string {
	head = strings.ReplaceAll(head, "\r\n", "\n")
	return strings.Split(head, "\n")
}

func parseHeaders(lines []string) http.Header {
	headers := make(http.Header)
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		headers.Add(strings.TrimSpace(name), strings.TrimSpace(value))
	}
	return headers
}

func parseTarget(target, hostHeader string) (host, path, rawQuery string, err error) {
	if target == "*" {
		return strings.ToLower(strings.TrimSpace(hostHeader)), "*", "", nil
	}
	var parsed *url.URL
	if strings.Contains(target, "://") {
		parsed, err = url.Parse(target)
	} else {
		parsed, err = url.ParseRequestURI(target)
	}
	if err != nil {
		return "", "", "", fmt.Errorf("parse request target: %w", err)
	}
	host = parsed.Host
	if host == "" {
		host = hostHeader
	}
	path = parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	return strings.ToLower(strings.TrimSpace(host)), path, parsed.RawQuery, nil
}

func parseStatusLine(line string) (int, error) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) < 2 {
		return 0, fmt.Errorf("malformed status line %q", line)
	}
	status, err := strconv.Atoi(fields[1])
	if err != nil || status < 100 || status > 599 {
		return 0, fmt.Errorf("malformed status code in %q", line)
	}
	return status, nil
}

func looksTruncated(body string) bool {
	if len(body) < 64 {
		return false
	}
	return strings.Contains(body[max(0, len(body)-200):], "[truncated] @blob sha256:")
}
