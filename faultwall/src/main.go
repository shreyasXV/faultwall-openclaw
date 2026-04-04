// FaultWall — Agentic Data Firewall for PostgreSQL
// Demo implementation for AI Tinkerers "Cage the Claw" Hackathon 2025
//
// Architecture:
//   :5433  → TCP proxy that intercepts PostgreSQL wire protocol messages
//   :8080  → REST API serving audit log events to the dashboard
//
// Policy evaluation happens on every Query message before it is forwarded
// to the upstream PostgreSQL server.

package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// ─────────────────────────────────────────────
// Configuration & Policy types
// ─────────────────────────────────────────────

type PolicyFile struct {
	Version string                    `yaml:"version"`
	Global  GlobalConfig              `yaml:"global"`
	Agents  map[string]AgentPolicy   `yaml:"agents"`
	Audit   AuditConfig              `yaml:"audit"`
}

type GlobalConfig struct {
	AuditAll       bool   `yaml:"audit_all"`
	DefaultDenyDDL bool   `yaml:"default_deny_ddl"`
	AuditFormat    string `yaml:"audit_format"`
}

type AgentPolicy struct {
	Description string      `yaml:"description"`
	Allow       AccessRules `yaml:"allow"`
	Deny        AccessRules `yaml:"deny"`
	RateLimit   RateLimit   `yaml:"rate_limit"`
}

type AccessRules struct {
	Tables     []string `yaml:"tables"`
	Operations []string `yaml:"operations"`
}

type RateLimit struct {
	QueriesPerMinute int    `yaml:"queries_per_minute"`
	OnLimit          string `yaml:"on_limit"`
}

type AuditConfig struct {
	BufferSize int      `yaml:"buffer_size"`
	Fields     []string `yaml:"fields"`
}

// ─────────────────────────────────────────────
// Audit Event
// ─────────────────────────────────────────────

type AuditEvent struct {
	ID         int64     `json:"id"`
	Timestamp  time.Time `json:"timestamp"`
	Agent      string    `json:"agent"`
	Operation  string    `json:"operation"`
	Tables     []string  `json:"tables"`
	Query      string    `json:"query"`
	QueryHash  string    `json:"query_hash"`
	Decision   string    `json:"decision"`
	Reason     string    `json:"reason"`
	DurationMs float64   `json:"duration_ms"`
	RemoteAddr string    `json:"remote_addr"`
}

// ─────────────────────────────────────────────
// Rate Limiter
// ─────────────────────────────────────────────

type AgentRateLimiter struct {
	mu       sync.Mutex
	counters map[string][]time.Time
}

func NewRateLimiter() *AgentRateLimiter {
	return &AgentRateLimiter{counters: make(map[string][]time.Time)}
}

// Allow returns true if the agent is within its rate limit.
func (r *AgentRateLimiter) Allow(agent string, limit int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	window := now.Add(-60 * time.Second)
	times := r.counters[agent]
	// Prune old entries
	valid := times[:0]
	for _, t := range times {
		if t.After(window) {
			valid = append(valid, t)
		}
	}
	r.counters[agent] = valid
	if limit > 0 && len(valid) >= limit {
		return false
	}
	r.counters[agent] = append(r.counters[agent], now)
	return true
}

// ─────────────────────────────────────────────
// Audit Log
// ─────────────────────────────────────────────

type AuditLog struct {
	mu         sync.RWMutex
	events     []AuditEvent
	counter    int64
	bufferSize int
	sseClients map[chan AuditEvent]struct{}
	sseMu      sync.Mutex
}

func NewAuditLog(size int) *AuditLog {
	return &AuditLog{
		bufferSize: size,
		sseClients: make(map[chan AuditEvent]struct{}),
	}
}

func (a *AuditLog) Append(ev AuditEvent) {
	a.mu.Lock()
	a.counter++
	ev.ID = a.counter
	if len(a.events) >= a.bufferSize {
		a.events = a.events[1:]
	}
	a.events = append(a.events, ev)
	a.mu.Unlock()

	// Broadcast to SSE clients
	a.sseMu.Lock()
	for ch := range a.sseClients {
		select {
		case ch <- ev:
		default:
		}
	}
	a.sseMu.Unlock()

	b, _ := json.Marshal(ev)
	log.Printf("[AUDIT] %s", string(b))
}

func (a *AuditLog) All() []AuditEvent {
	a.mu.RLock()
	defer a.mu.RUnlock()
	cp := make([]AuditEvent, len(a.events))
	copy(cp, a.events)
	return cp
}

func (a *AuditLog) Subscribe() chan AuditEvent {
	ch := make(chan AuditEvent, 20)
	a.sseMu.Lock()
	a.sseClients[ch] = struct{}{}
	a.sseMu.Unlock()
	return ch
}

func (a *AuditLog) Unsubscribe(ch chan AuditEvent) {
	a.sseMu.Lock()
	delete(a.sseClients, ch)
	a.sseMu.Unlock()
}

// ─────────────────────────────────────────────
// Policy Engine
// ─────────────────────────────────────────────

type PolicyEngine struct {
	policy  PolicyFile
	limiter *AgentRateLimiter
}

func NewPolicyEngine(policyPath string) (*PolicyEngine, error) {
	data, err := os.ReadFile(policyPath)
	if err != nil {
		return nil, fmt.Errorf("reading policy file: %w", err)
	}
	var pf PolicyFile
	if err := yaml.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("parsing policy file: %w", err)
	}
	log.Printf("[POLICY] Loaded policy v%s with %d agent(s)", pf.Version, len(pf.Agents))
	return &PolicyEngine{policy: pf, limiter: NewRateLimiter()}, nil
}

// extractOperation returns the SQL operation type from a query string.
func extractOperation(query string) string {
	q := strings.TrimSpace(strings.ToUpper(query))
	for _, op := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "DROP", "TRUNCATE", "ALTER", "CREATE", "GRANT", "REVOKE"} {
		if strings.HasPrefix(q, op) {
			return op
		}
	}
	return "UNKNOWN"
}

// extractTables does a very simplified table extraction from a SQL query.
// A production implementation would use a proper SQL parser.
func extractTables(query string) []string {
	tables := []string{}
	upper := strings.ToUpper(query)
	lower := strings.ToLower(query)

	keywords := []string{"from ", "join ", "into ", "update ", "table "}
	for _, kw := range keywords {
		idx := strings.Index(upper, strings.ToUpper(kw))
		for idx != -1 {
			rest := strings.TrimSpace(lower[idx+len(kw):])
			word := strings.FieldsFunc(rest, func(r rune) bool {
				return r == ' ' || r == ',' || r == ';' || r == '(' || r == '\n' || r == '\t'
			})
			if len(word) > 0 {
				t := strings.Trim(word[0], `"'` )
				if t != "" && !strings.Contains(t, "(") {
					tables = append(tables, t)
				}
			}
			idx = strings.Index(upper[idx+1:], strings.ToUpper(kw))
			if idx != -1 {
				idx += strings.Index(upper, strings.ToUpper(kw)) + 1
			}
		}
	}

	// Deduplicate
	seen := map[string]bool{}
	result := []string{}
	for _, t := range tables {
		if !seen[t] {
			seen[t] = true
			result = append(result, t)
		}
	}
	return result
}

type Decision struct {
	Allowed bool
	Reason  string
}

func (e *PolicyEngine) Evaluate(agent, query string) Decision {
	op := extractOperation(query)
	tables := extractTables(query)

	// Resolve policy: named agent or default
	pol, ok := e.policy.Agents[agent]
	if !ok {
		pol, ok = e.policy.Agents["default"]
		if !ok {
			return Decision{false, "No policy found for agent and no default policy configured"}
		}
		agent = "default"
	}

	// Rate limit check
	limit := pol.RateLimit.QueriesPerMinute
	if limit <= 0 {
		limit = 10 // safe default
	}
	if !e.limiter.Allow(agent, limit) {
		return Decision{false, fmt.Sprintf("Rate limit exceeded: max %d queries/min for agent '%s'", limit, agent)}
	}

	// DDL global block
	ddlOps := map[string]bool{"DROP": true, "ALTER": true, "CREATE": true, "GRANT": true, "REVOKE": true}
	if e.policy.Global.DefaultDenyDDL && ddlOps[op] {
		// Only admin-agent (wildcard) can bypass this
		if !containsWildcard(pol.Allow.Operations) {
			return Decision{false, fmt.Sprintf("DDL operation '%s' is blocked by global policy", op)}
		}
	}

	// Check deny rules first (deny overrides allow)
	for _, denyOp := range pol.Deny.Operations {
		if strings.EqualFold(denyOp, op) || denyOp == "*" {
			return Decision{false, fmt.Sprintf("Operation '%s' is in the deny list for agent '%s'", op, agent)}
		}
	}
	for _, table := range tables {
		for _, denyTable := range pol.Deny.Tables {
			if strings.EqualFold(denyTable, table) || denyTable == "*" {
				return Decision{false, fmt.Sprintf("Table '%s' is in the deny list for agent '%s' (policy: deny.tables)", table, agent)}
			}
		}
	}

	// Check allow rules
	opAllowed := containsWildcard(pol.Allow.Operations)
	if !opAllowed {
		for _, allowOp := range pol.Allow.Operations {
			if strings.EqualFold(allowOp, op) {
				opAllowed = true
				break
			}
		}
	}
	if !opAllowed {
		return Decision{false, fmt.Sprintf("Operation '%s' is not in the allow list for agent '%s'", op, agent)}
	}

	tableAllowed := containsWildcard(pol.Allow.Tables) || len(tables) == 0
	if !tableAllowed {
		for _, table := range tables {
			found := false
			for _, allowTable := range pol.Allow.Tables {
				if strings.EqualFold(allowTable, table) || allowTable == "*" {
					found = true
					break
				}
			}
			if !found {
				return Decision{false, fmt.Sprintf("Table '%s' is not in the allow list for agent '%s'", table, agent)}
			}
		}
		tableAllowed = true
	}

	return Decision{true, "Query allowed by policy"}
}

func containsWildcard(list []string) bool {
	for _, v := range list {
		if v == "*" {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────
// PostgreSQL Wire Protocol Proxy
//
// This implements a simplified PostgreSQL message proxy.
// It handles:
//   - Startup / auth negotiation (pass-through after capturing username)
//   - Query messages (evaluate policy, block or forward)
//   - All other messages (transparent pass-through)
// ─────────────────────────────────────────────

type ProxySession struct {
	clientConn net.Conn
	serverConn net.Conn
	agent      string
	engine     *PolicyEngine
	audit      *AuditLog
	remoteAddr string
}

func handleClientConn(clientConn net.Conn, engine *PolicyEngine, audit *AuditLog, upstreamAddr string) {
	defer clientConn.Close()
	remoteAddr := clientConn.RemoteAddr().String()
	log.Printf("[PROXY] New connection from %s", remoteAddr)

	// ── Step 1: Read the startup message from the client ──────────────────
	// PostgreSQL startup message format:
	//   int32 length (including self)
	//   int32 protocol version (196608 = 3.0)
	//   key\0value\0 pairs, terminated by \0

	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(clientConn, lenBuf); err != nil {
		log.Printf("[PROXY] Failed to read startup length from %s: %v", remoteAddr, err)
		return
	}
	msgLen := int(binary.BigEndian.Uint32(lenBuf))
	if msgLen < 8 || msgLen > 65535 {
		log.Printf("[PROXY] Invalid startup message length %d from %s", msgLen, remoteAddr)
		return
	}
	body := make([]byte, msgLen-4)
	if _, err := io.ReadFull(clientConn, body); err != nil {
		log.Printf("[PROXY] Failed to read startup body from %s: %v", remoteAddr, err)
		return
	}

	// Parse protocol version
	// protoVer := binary.BigEndian.Uint32(body[:4]) // 196608 = 3.0

	// Extract username from startup parameters
	agent := "default"
	params := body[4:]
	for {
		nullIdx := strings.IndexByte(string(params), 0)
		if nullIdx <= 0 {
			break
		}
		key := string(params[:nullIdx])
		params = params[nullIdx+1:]
		nullIdx = strings.IndexByte(string(params), 0)
		if nullIdx < 0 {
			break
		}
		val := string(params[:nullIdx])
		params = params[nullIdx+1:]
		if key == "user" {
			agent = val
			break
		}
	}
	log.Printf("[PROXY] Agent identified as '%s' from %s", agent, remoteAddr)

	// ── Step 2: Connect to upstream PostgreSQL ─────────────────────────────
	serverConn, err := net.DialTimeout("tcp", upstreamAddr, 10*time.Second)
	if err != nil {
		log.Printf("[PROXY] Cannot connect to upstream %s: %v", upstreamAddr, err)
		// Send error to client
		sendErrorToClient(clientConn, "FaultWall: cannot connect to upstream database")
		return
	}
	defer serverConn.Close()

	// Forward the startup message to upstream (server uses the real PG credentials)
	if _, err := serverConn.Write(lenBuf); err != nil {
		return
	}
	if _, err := serverConn.Write(body); err != nil {
		return
	}

	// ── Step 3: Pass-through auth negotiation ─────────────────────────────
	// Forward messages between client and server until we get ReadyForQuery
	session := &ProxySession{
		clientConn: clientConn,
		serverConn: serverConn,
		agent:      agent,
		engine:     engine,
		audit:      audit,
		remoteAddr: remoteAddr,
	}

	if !session.handleAuthPhase() {
		return
	}

	// ── Step 4: Main query loop ────────────────────────────────────────────
	session.handleQueryLoop()
}

// handleAuthPhase proxies messages between client and server until ReadyForQuery.
func (s *ProxySession) handleAuthPhase() bool {
	for {
		// Read from server, forward to client
		msgType, data, err := readPGMessage(s.serverConn)
		if err != nil {
			return false
		}
		if err := writePGMessage(s.clientConn, msgType, data); err != nil {
			return false
		}
		// 'Z' = ReadyForQuery — auth phase is done
		if msgType == 'Z' {
			return true
		}
		// 'E' = ErrorResponse — auth failed
		if msgType == 'E' {
			log.Printf("[PROXY] Auth failed for agent '%s'", s.agent)
			return false
		}
	}
}

// handleQueryLoop is the main event loop after auth.
// Reads Query messages from client, evaluates policy, forwards or blocks.
func (s *ProxySession) handleQueryLoop() {
	for {
		msgType, data, err := readPGMessage(s.clientConn)
		if err != nil {
			return
		}

		switch msgType {
		case 'Q': // Simple Query
			query := ""
			if len(data) > 0 {
				// Query message body is a null-terminated string
				query = strings.TrimRight(string(data), "\x00")
			}
			s.handleQuery(query)

		case 'X': // Terminate
			serverConn := s.serverConn
			writePGMessage(serverConn, msgType, data) //nolint
			return

		default:
			// Forward all other messages (Bind, Execute, Parse, Sync, etc.)
			if err := writePGMessage(s.serverConn, msgType, data); err != nil {
				return
			}
			// Read and forward the response
			s.forwardUntilReady()
		}
	}
}

// handleQuery evaluates policy for a query and either forwards it or blocks it.
func (s *ProxySession) handleQuery(query string) {
	start := time.Now()
	op := extractOperation(query)
	tables := extractTables(query)

	decision := s.engine.Evaluate(s.agent, query)
	elapsed := time.Since(start).Seconds() * 1000

	// Build a simple hash (for dedup in dashboard)
	hash := fmt.Sprintf("%x", len(query))

	ev := AuditEvent{
		Timestamp:  time.Now().UTC(),
		Agent:      s.agent,
		Operation:  op,
		Tables:     tables,
		Query:      truncate(query, 500),
		QueryHash:  hash,
		DurationMs: elapsed,
		RemoteAddr: s.remoteAddr,
	}

	if decision.Allowed {
		ev.Decision = "ALLOW"
		ev.Reason = decision.Reason

		// Forward query to upstream
		if err := writePGMessage(s.serverConn, 'Q', []byte(query+"\x00")); err != nil {
			s.audit.Append(ev)
			return
		}
		s.forwardUntilReady()
	} else {
		ev.Decision = "BLOCK"
		ev.Reason = decision.Reason

		log.Printf("[BLOCK] agent=%s op=%s tables=%v reason=%s", s.agent, op, tables, decision.Reason)

		// Send a PostgreSQL ErrorResponse to the client
		errMsg := fmt.Sprintf("[FaultWall] Query blocked by policy. Agent: %s | Reason: %s", s.agent, decision.Reason)
		sendErrorToClient(s.clientConn, errMsg)

		// Send ReadyForQuery so client knows we're done
		sendReadyForQuery(s.clientConn)
	}

	s.audit.Append(ev)
}

// forwardUntilReady reads from server and forwards to client until ReadyForQuery.
func (s *ProxySession) forwardUntilReady() {
	for {
		msgType, data, err := readPGMessage(s.serverConn)
		if err != nil {
			return
		}
		if err := writePGMessage(s.clientConn, msgType, data); err != nil {
			return
		}
		if msgType == 'Z' { // ReadyForQuery
			return
		}
	}
}

// ─────────────────────────────────────────────
// PostgreSQL message helpers
// ─────────────────────────────────────────────

// readPGMessage reads one PostgreSQL message: type byte + int32 length + body.
func readPGMessage(conn net.Conn) (byte, []byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(conn, header); err != nil {
		return 0, nil, err
	}
	msgType := header[0]
	msgLen := int(binary.BigEndian.Uint32(header[1:5]))
	if msgLen < 4 {
		return msgType, nil, nil
	}
	body := make([]byte, msgLen-4)
	if len(body) > 0 {
		if _, err := io.ReadFull(conn, body); err != nil {
			return 0, nil, err
		}
	}
	return msgType, body, nil
}

// writePGMessage writes one PostgreSQL message to conn.
func writePGMessage(conn net.Conn, msgType byte, body []byte) error {
	msg := make([]byte, 5+len(body))
	msg[0] = msgType
	binary.BigEndian.PutUint32(msg[1:5], uint32(4+len(body)))
	copy(msg[5:], body)
	_, err := conn.Write(msg)
	return err
}

// sendErrorToClient sends a PostgreSQL ErrorResponse message.
func sendErrorToClient(conn net.Conn, msg string) {
	// ErrorResponse: 'E' + length + 'S' + severity + '\0' + 'M' + message + '\0' + '\0'
	severity := "ERROR\x00"
	body := []byte("S" + severity + "M" + msg + "\x00\x00")
	writePGMessage(conn, 'E', body) //nolint
}

// sendReadyForQuery sends a ReadyForQuery message with status 'I' (idle).
func sendReadyForQuery(conn net.Conn) {
	writePGMessage(conn, 'Z', []byte("I")) //nolint
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// ─────────────────────────────────────────────
// Dashboard API (REST + SSE)
// ─────────────────────────────────────────────

func startAPIServer(audit *AuditLog, port string) {
	mux := http.NewServeMux()

	// CORS middleware
	cors := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			h.ServeHTTP(w, r)
		})
	}

	// GET /events — returns all audit events as JSON
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		events := audit.All()
		json.NewEncoder(w).Encode(events) //nolint
	})

	// GET /stream — Server-Sent Events for live dashboard updates
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		ch := audit.Subscribe()
		defer audit.Unsubscribe(ch)

		ctx := r.Context()
		for {
			select {
			case ev := <-ch:
				data, _ := json.Marshal(ev)
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			case <-ctx.Done():
				return
			}
		}
	})

	// GET /health — health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "service": "faultwall"}) //nolint
	})

	// GET /stats — aggregate stats
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		events := audit.All()
		allowed, blocked := 0, 0
		agentCounts := map[string]int{}
		for _, ev := range events {
			if ev.Decision == "ALLOW" {
				allowed++
			} else {
				blocked++
			}
			agentCounts[ev.Agent]++
		}
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint
			"total":       len(events),
			"allowed":     allowed,
			"blocked":     blocked,
			"block_rate":  fmt.Sprintf("%.1f%%", float64(blocked)/max(float64(len(events)), 1)*100),
			"agents":      agentCounts,
		})
	})

	log.Printf("[API] Dashboard API listening on :%s", port)
	srv := &http.Server{
		Addr:    ":" + port,
		Handler: cors(mux),
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("[API] Server error: %v", err)
	}
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// ─────────────────────────────────────────────
// Main
// ─────────────────────────────────────────────

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("╔══════════════════════════════════════╗")
	log.Println("║   FaultWall — Agentic Data Firewall  ║")
	log.Println("║   AI Tinkerers Cage the Claw 2025    ║")
	log.Println("╚══════════════════════════════════════╝")

	// Config from environment
	policyFile := getEnv("POLICY_FILE", "/etc/faultwall/policy.yaml")
	proxyPort := getEnv("PROXY_PORT", "5433")
	apiPort := getEnv("API_PORT", "8080")
	pgHost := getEnv("PG_HOST", "localhost")
	pgPort := getEnv("PG_PORT", "5432")

	upstreamAddr := fmt.Sprintf("%s:%s", pgHost, pgPort)

	// Load policy
	engine, err := NewPolicyEngine(policyFile)
	if err != nil {
		log.Fatalf("[FATAL] Cannot load policy: %v", err)
	}

	// Audit log
	audit := NewAuditLog(1000)

	// Start API server in background
	go startAPIServer(audit, apiPort)

	// Start TCP proxy
	listener, err := net.Listen("tcp", ":"+proxyPort)
	if err != nil {
		log.Fatalf("[FATAL] Cannot bind proxy port :%s: %v", proxyPort, err)
	}
	log.Printf("[PROXY] FaultWall proxy listening on :%s → upstream %s", proxyPort, upstreamAddr)

	ctx := context.Background()
	_ = ctx

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("[PROXY] Accept error: %v", err)
			continue
		}
		go handleClientConn(conn, engine, audit, upstreamAddr)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
