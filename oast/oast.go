// Package oast implements the receiving half of the out-of-band callback fabric
// proposed in #12.
//
// Phase 1 is an HTTP-only listener. It mints unguessable per-engagement tokens,
// records callbacks that carry a known token, and exposes them to agents through
// the oast_token and oast_poll tools. DNS and SMTP listeners, automatic finding
// promotion and an external/interactsh mode are deliberate follow-ups; keeping
// this landing small lets the listener and its storage be reviewed on their own.
//
// Safety properties:
//   - Disabled by default; ARTEX_OAST=1 enables it.
//   - The listener refuses to bind anything except a loopback address. Public
//     reachability is an operator concern (a tunnel or port-forward), never a
//     direct bind.
//   - Unknown, expired or rate-limited callbacks receive a normal response but
//     are not persisted, so the database stays free of unauthenticated noise.
//   - Bodies are capped before they reach the database. Nothing is relayed.
package oast

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/db"
)

// TokenLength is the number of base32 characters in a callback token. 12
// characters carry 60 bits of entropy, which is far beyond guessing range for a
// short-lived engagement token.
const TokenLength = 12

const (
	defaultTTL     = time.Hour
	maxTTL         = 24 * time.Hour
	maxBodyBytes   = 64 << 10
	maxTokenPerSec = 20
	maxGlobalPerS  = 200
)

var tokenAlphabet = base32.StdEncoding.WithPadding(base32.NoPadding)

// Config controls the listener and callback URL construction.
type Config struct {
	// Bind is the listener address. Only a loopback host is accepted.
	Bind string
	// PublicURL is the externally reachable base URL (for example the HTTPS
	// endpoint of an operator-managed tunnel). When empty, minted URLs point at
	// the loopback listener and are useful only for local testing.
	PublicURL string
	// Zone, when set, adds a host-style callback (token.zone) for payloads that
	// need a hostname rather than a URL. It does not start a DNS listener; the
	// operator must point the wildcard at the tunnel.
	Zone string
}

// FromEnv reads the OAST configuration. ok is false unless ARTEX_OAST is truthy.
func FromEnv() (Config, bool) {
	if !truthy(os.Getenv("ARTEX_OAST")) {
		return Config{}, false
	}
	cfg := Config{
		Bind:      strings.TrimSpace(os.Getenv("ARTEX_OAST_BIND")),
		PublicURL: strings.TrimSpace(os.Getenv("ARTEX_OAST_PUBLIC_URL")),
		Zone:      strings.TrimSpace(os.Getenv("ARTEX_OAST_ZONE")),
	}
	if cfg.Bind == "" {
		cfg.Bind = "127.0.0.1:0"
	}
	return cfg, true
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// Token is a minted callback token and the URLs an agent should inject.
type Token struct {
	Token        string    `json:"token"`
	CallbackURL  string    `json:"callback_url"`
	CallbackHost string    `json:"callback_host,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// OAST is a running HTTP callback listener backed by the OAST tables.
type OAST struct {
	store   *db.DB
	cfg     Config
	server  *http.Server
	ln      net.Listener
	limiter *rateLimiter
	now     func() time.Time

	closeOnce sync.Once
	done      chan struct{}
}

// Open validates cfg, ensures the OAST tables exist, and starts the loopback
// listener. Call Close to stop it.
func Open(store *db.DB, cfg Config) (*OAST, error) {
	if store == nil {
		return nil, errors.New("oast: database is required")
	}
	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}
	if err := store.EnsureOASTTables(); err != nil {
		return nil, fmt.Errorf("oast: ensure tables: %w", err)
	}
	ln, err := net.Listen("tcp", cfg.Bind)
	if err != nil {
		return nil, fmt.Errorf("oast: listen on %s: %w", cfg.Bind, err)
	}
	o := &OAST{
		store:   store,
		cfg:     cfg,
		ln:      ln,
		limiter: newRateLimiter(maxGlobalPerS, maxTokenPerSec),
		now:     time.Now,
		done:    make(chan struct{}),
	}
	o.server = &http.Server{
		Handler:           http.HandlerFunc(o.handle),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	go func() {
		defer close(o.done)
		if err := o.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("[oast] listener stopped: %v", err)
		}
	}()
	return o, nil
}

func validateConfig(cfg *Config) error {
	host, port, err := net.SplitHostPort(cfg.Bind)
	if err != nil {
		return fmt.Errorf("oast: invalid bind %q: %w", cfg.Bind, err)
	}
	if !isLoopbackHost(host) {
		return fmt.Errorf("oast: bind host %q is not loopback; expose the listener through a tunnel instead", host)
	}
	if port != "" {
		p, err := strconv.Atoi(port)
		if err != nil || p < 0 || p > 65535 {
			return fmt.Errorf("oast: invalid bind port %q", port)
		}
	}
	cfg.Bind = net.JoinHostPort(host, port)

	if cfg.PublicURL != "" {
		u, err := url.Parse(cfg.PublicURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("oast: ARTEX_OAST_PUBLIC_URL must be an absolute http(s) URL")
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("oast: ARTEX_OAST_PUBLIC_URL must use http or https")
		}
		cfg.PublicURL = strings.TrimRight(cfg.PublicURL, "/")
	}
	if cfg.Zone != "" {
		if strings.ContainsAny(cfg.Zone, "/: \t") || !strings.Contains(cfg.Zone, ".") {
			return fmt.Errorf("oast: ARTEX_OAST_ZONE must be a bare DNS zone such as oast.example.com")
		}
		cfg.Zone = strings.TrimSuffix(strings.ToLower(cfg.Zone), ".")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// Close stops the listener. It is safe to call more than once.
func (o *OAST) Close() error {
	var err error
	o.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err = o.server.Shutdown(ctx)
		<-o.done
	})
	return err
}

// Addr returns the listener address actually bound (useful when Bind used :0).
func (o *OAST) Addr() string { return o.ln.Addr().String() }

// PublicURL returns the configured external base URL, or "" for loopback-only.
func (o *OAST) PublicURL() string { return o.cfg.PublicURL }

// Mint creates a new token for a task. ttl is clamped to [1 minute, 24 hours].
func (o *OAST) Mint(taskID int64, label string, ttl time.Duration) (Token, error) {
	if ttl <= 0 {
		ttl = defaultTTL
	}
	if ttl > maxTTL {
		ttl = maxTTL
	}
	if ttl < time.Minute {
		ttl = time.Minute
	}
	label = strings.TrimSpace(label)
	if len(label) > 200 {
		label = label[:200]
	}

	var lastErr error
	for range 5 {
		token, err := randomToken()
		if err != nil {
			return Token{}, err
		}
		expires := o.now().Add(ttl)
		if err := o.store.InsertOASTToken(token, taskID, label, expires); err != nil {
			lastErr = err
			continue
		}
		out := Token{Token: token, ExpiresAt: expires}
		if o.cfg.PublicURL != "" {
			out.CallbackURL = o.cfg.PublicURL + "/t/" + token
		} else {
			out.CallbackURL = "http://" + o.Addr() + "/t/" + token
		}
		if o.cfg.Zone != "" {
			out.CallbackHost = token + "." + o.cfg.Zone
		}
		return out, nil
	}
	return Token{}, fmt.Errorf("oast: could not allocate a token: %w", lastErr)
}

// Poll returns callbacks for token after afterID. When taskID is non-zero the
// token must belong to that task.
func (o *OAST) Poll(taskID int64, token string, afterID int64, limit int) ([]db.OASTInteraction, error) {
	if !validToken(token) {
		return nil, errors.New("oast: malformed token")
	}
	row, ok, err := o.store.LookupOASTToken(token)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("oast: unknown token")
	}
	if taskID != 0 && row.TaskID != 0 && row.TaskID != taskID {
		return nil, errors.New("oast: token belongs to another task")
	}
	return o.store.ListOASTInteractions(token, afterID, limit)
}

func randomToken() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("oast: random token: %w", err)
	}
	out := strings.ToLower(tokenAlphabet.EncodeToString(buf))
	if len(out) < TokenLength {
		return "", errors.New("oast: random token is too short")
	}
	return out[:TokenLength], nil
}

func validToken(token string) bool {
	if len(token) != TokenLength {
		return false
	}
	for _, r := range token {
		if (r < 'a' || r > 'z') && (r < '2' || r > '7') {
			return false
		}
	}
	return true
}

func (o *OAST) handle(w http.ResponseWriter, r *http.Request) {
	// Read at most maxBodyBytes+1 so an oversized body is detectable without
	// buffering the whole request.
	body, _ := readLimited(r.Body, maxBodyBytes)
	token := tokenFromRequest(r)
	if !validToken(token) {
		ok(w)
		return
	}
	if !o.limiter.allow(token, o.now()) {
		ok(w)
		return
	}

	row, found, err := o.store.LookupOASTToken(token)
	if err != nil {
		log.Printf("[oast] token lookup: %v", err)
		ok(w)
		return
	}
	if !found || o.now().After(row.ExpiresAt) {
		ok(w)
		return
	}

	bodyText := strings.ToValidUTF8(string(body), "�")
	if len(bodyText) > maxBodyBytes {
		bodyText = bodyText[:maxBodyBytes]
	}
	_, err = o.store.InsertOASTInteraction(db.OASTInteraction{
		Token:     token,
		TaskID:    row.TaskID,
		Proto:     "http",
		Method:    truncate(r.Method, 16),
		Host:      truncate(r.Host, 255),
		Path:      truncate(r.URL.RequestURI(), 2048),
		Peer:      truncate(remoteHost(r.RemoteAddr), 64),
		UserAgent: truncate(r.UserAgent(), 512),
		Body:      bodyText,
	})
	if err != nil {
		log.Printf("[oast] record interaction: %v", err)
	}
	ok(w)
}

func ok(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func tokenFromRequest(r *http.Request) string {
	path := strings.Trim(r.URL.Path, "/")
	if strings.HasPrefix(path, "t/") {
		first, _, _ := strings.Cut(strings.TrimPrefix(path, "t/"), "/")
		return strings.ToLower(first)
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	first, _, _ := strings.Cut(strings.ToLower(host), ".")
	if validToken(first) {
		return first
	}
	return ""
}

func remoteHost(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}

func readLimited(r io.Reader, limit int) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, int64(limit)))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// rateLimiter enforces a global and a per-token per-second budget. Excess
// callbacks are dropped without a database write.
type rateLimiter struct {
	mu      sync.Mutex
	second  int64
	global  int
	per     map[string]int
	globalM int
	tokenM  int
}

func newRateLimiter(globalMax, tokenMax int) *rateLimiter {
	return &rateLimiter{per: map[string]int{}, globalM: globalMax, tokenM: tokenMax}
}

func (l *rateLimiter) allow(token string, now time.Time) bool {
	sec := now.Unix()
	l.mu.Lock()
	defer l.mu.Unlock()
	if sec != l.second {
		l.second = sec
		l.global = 0
		l.per = map[string]int{}
	}
	if l.global >= l.globalM || l.per[token] >= l.tokenM {
		return false
	}
	l.global++
	l.per[token]++
	return true
}
