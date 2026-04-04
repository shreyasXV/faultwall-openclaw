package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	_ "github.com/lib/pq"
)

// ─────────────────────────────────────────────
//  ANSI colours
// ─────────────────────────────────────────────

const (
	reset   = "\033[0m"
	bold    = "\033[1m"
	red     = "\033[31m"
	green   = "\033[32m"
	yellow  = "\033[33m"
	blue    = "\033[34m"
	magenta = "\033[35m"
	cyan    = "\033[36m"
	white   = "\033[37m"
	hiCyan  = "\033[96m"
	hiGreen = "\033[92m"
	hiRed   = "\033[91m"
)

func logInfo(format string, args ...any) {
	log.Printf(cyan+"[INFO] "+reset+format, args...)
}
func logWarn(format string, args ...any) {
	log.Printf(yellow+"[WARN] "+reset+format, args...)
}
func logError(format string, args ...any) {
	log.Printf(red+"[ERR ] "+reset+format, args...)
}
func logAllow(agentID, op string, tables []string) {
	log.Printf(hiGreen+bold+"[✔ ALLOW]"+reset+" agent=%-20s  op=%-10s  tables=%v", agentID, op, tables)
}
func logBlock(agentID, op, reason string) {
	log.Printf(hiRed+bold+"[✘ BLOCK]"+reset+" agent=%-20s  op=%-10s  reason=%s", agentID, op, reason)
}

// ─────────────────────────────────────────────
//  Event bus (SSE)
// ─────────────────────────────────────────────

type QueryEvent struct {
	Timestamp time.Time `json:"timestamp"`
	AgentID   string    `json:"agent_id"`
	Query     string    `json:"query"`
	Action    string    `json:"action"` // "allowed" | "blocked"
	Reason    string    `json:"reason"`
	Op        string    `json:"op"`
	Tables    []string  `json:"tables"`
}

type EventBus struct {
	mu          sync.RWMutex
	subscribers map[chan QueryEvent]struct{}
}

func newEventBus() *EventBus {
	return &EventBus{subscribers: make(map[chan QueryEvent]struct{})}
}

func (b *EventBus) Subscribe() chan QueryEvent {
	ch := make(chan QueryEvent, 32)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *EventBus) Unsubscribe(ch chan QueryEvent) {
	b.mu.Lock()
	delete(b.subscribers, ch)
	b.mu.Unlock()
	close(ch)
}

func (b *EventBus) Publish(ev QueryEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subscribers {
		select {
		case ch <- ev:
		default: // drop if subscriber is slow
		}
	}
}

// ─────────────────────────────────────────────
//  Stats
// ─────────────────────────────────────────────

type AgentStats struct {
	Total   int `json:"total"`
	Allowed int `json:"allowed"`
	Blocked int `json:"blocked"`
}

type Stats struct {
	mu         sync.Mutex
	Total      int                    `json:"total"`
	Allowed    int                    `json:"allowed"`
	Blocked    int                    `json:"blocked"`
	PerAgent   map[string]*AgentStats `json:"per_agent"`
	StartedAt  time.Time              `json:"started_at"`
}

func newStats() *Stats {
	return &Stats{PerAgent: make(map[string]*AgentStats), StartedAt: time.Now()}
}

func (s *Stats) Record(agentID string, allowed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Total++
	if _, ok := s.PerAgent[agentID]; !ok {
		s.PerAgent[agentID] = &AgentStats{}
	}
	a := s.PerAgent[agentID]
	a.Total++
	if allowed {
		s.Allowed++
		a.Allowed++
	} else {
		s.Blocked++
		a.Blocked++
	}
}

func (s *Stats) Snapshot() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	agents := make(map[string]AgentStats, len(s.PerAgent))
	for k, v := range s.PerAgent {
		agents[k] = *v
	}
	return map[string]any{
		"total":      s.Total,
		"allowed":    s.Allowed,
		"blocked":    s.Blocked,
		"per_agent":  agents,
		"started_at": s.StartedAt,
		"uptime_sec": int(time.Since(s.StartedAt).Seconds()),
	}
}

// ─────────────────────────────────────────────
//  Server
// ─────────────────────────────────────────────

type Server struct {
	db     *sql.DB
	policy *PolicyEngine
	bus    *EventBus
	stats  *Stats
}

func newServer() (*Server, error) {
	pe, err := NewPolicyEngine()
	if err != nil {
		return nil, fmt.Errorf("policy engine: %w", err)
	}

	var db *sql.DB
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		logInfo("Connecting to Postgres…")
		db, err = sql.Open("postgres", dsn)
		if err != nil {
			return nil, fmt.Errorf("sql.Open: %w", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err = db.PingContext(ctx); err != nil {
			logWarn("Postgres ping failed (%v) — queries will be policy-checked but not executed", err)
			db = nil
		} else {
			logInfo(hiGreen + "Postgres connected" + reset)
		}
	} else {
		logWarn("DATABASE_URL not set — running in policy-only (dry-run) mode")
	}

	return &Server{
		db:     db,
		policy: pe,
		bus:    newEventBus(),
		stats:  newStats(),
	}, nil
}

// ─────────────────────────────────────────────
//  CORS middleware
// ─────────────────────────────────────────────

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ─────────────────────────────────────────────
//  Handlers
// ─────────────────────────────────────────────

// POST /query
func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		AgentID string `json:"agent_id"`
		Query   string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.AgentID == "" || req.Query == "" {
		http.Error(w, "agent_id and query are required", http.StatusBadRequest)
		return
	}

	dec := s.policy.Check(req.AgentID, req.Query)

	action := "allowed"
	if !dec.Allowed {
		action = "blocked"
	}

	ev := QueryEvent{
		Timestamp: time.Now().UTC(),
		AgentID:   req.AgentID,
		Query:     req.Query,
		Action:    action,
		Reason:    dec.Reason,
		Op:        dec.Op,
		Tables:    dec.Tables,
	}

	s.stats.Record(req.AgentID, dec.Allowed)
	s.bus.Publish(ev)

	w.Header().Set("Content-Type", "application/json")

	if !dec.Allowed {
		logBlock(req.AgentID, dec.Op, dec.Reason)
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]any{
			"allowed": false,
			"action":  "blocked",
			"reason":  dec.Reason,
			"op":      dec.Op,
			"tables":  dec.Tables,
		})
		return
	}

	logAllow(req.AgentID, dec.Op, dec.Tables)

	// Execute against Postgres if available
	if s.db != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		rows, err := s.db.QueryContext(ctx, req.Query)
		if err != nil {
			logError("Query execution failed: %v", err)
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]any{
				"allowed": true,
				"action":  "allowed",
				"error":   err.Error(),
			})
			return
		}
		defer rows.Close()

		cols, _ := rows.Columns()
		var results []map[string]any
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				continue
			}
			row := make(map[string]any, len(cols))
			for i, col := range cols {
				row[col] = vals[i]
			}
			results = append(results, row)
		}

		json.NewEncoder(w).Encode(map[string]any{
			"allowed":  true,
			"action":   "allowed",
			"op":       dec.Op,
			"tables":   dec.Tables,
			"columns":  cols,
			"rows":     results,
			"row_count": len(results),
		})
		return
	}

	// Dry-run mode
	json.NewEncoder(w).Encode(map[string]any{
		"allowed": true,
		"action":  "allowed",
		"op":      dec.Op,
		"tables":  dec.Tables,
		"note":    "dry-run: no database connected",
	})
}

// GET /events  (SSE)
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering

	// Send a heartbeat comment immediately so the browser knows it's alive
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	ch := s.bus.Subscribe()
	defer s.bus.Unsubscribe(ch)

	logInfo("SSE client connected  remote=%s", r.RemoteAddr)

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			logInfo("SSE client disconnected  remote=%s", r.RemoteAddr)
			return
		case <-ticker.C:
			// Heartbeat keeps the connection alive through proxies
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: query\n")
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// GET /stats
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.stats.Snapshot())
}

// GET /health
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	dbStatus := "disconnected"
	if s.db != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := s.db.PingContext(ctx); err == nil {
			dbStatus = "connected"
		}
	}
	json.NewEncoder(w).Encode(map[string]any{
		"status":    "ok",
		"db":        dbStatus,
		"timestamp": time.Now().UTC(),
	})
}

// ─────────────────────────────────────────────
//  Banner
// ─────────────────────────────────────────────

func printBanner() {
	fmt.Println()
	fmt.Println(bold + magenta + "  ███████╗ █████╗ ██╗   ██╗██╗  ████████╗██╗    ██╗ █████╗ ██╗     ██╗" + reset)
	fmt.Println(bold + magenta + "  ██╔════╝██╔══██╗██║   ██║██║  ╚══██╔══╝██║    ██║██╔══██╗██║     ██║" + reset)
	fmt.Println(bold + magenta + "  █████╗  ███████║██║   ██║██║     ██║   ██║ █╗ ██║███████║██║     ██║" + reset)
	fmt.Println(bold + magenta + "  ██╔══╝  ██╔══██║██║   ██║██║     ██║   ██║███╗██║██╔══██║██║     ██║" + reset)
	fmt.Println(bold + magenta + "  ██║     ██║  ██║╚██████╔╝███████╗██║   ╚███╔███╔╝██║  ██║███████╗███████╗" + reset)
	fmt.Println(bold + magenta + "  ╚═╝     ╚═╝  ╚═╝ ╚═════╝ ╚══════╝╚═╝    ╚══╝╚══╝ ╚═╝  ╚═╝╚══════╝╚══════╝" + reset)
	fmt.Println()
	fmt.Println(bold + cyan + "  OpenClaw Hackathon Demo — AI Database Firewall" + reset)
	fmt.Println(bold + cyan + "  ─────────────────────────────────────────────" + reset)
	fmt.Println()
}

// ─────────────────────────────────────────────
//  Main
// ─────────────────────────────────────────────

func main() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	printBanner()

	srv, err := newServer()
	if err != nil {
		logError("Startup failed: %v", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/query", srv.handleQuery)
	mux.HandleFunc("/events", srv.handleEvents)
	mux.HandleFunc("/stats", srv.handleStats)
	mux.HandleFunc("/health", srv.handleHealth)

	// Friendly 404 for unknown routes
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"service": "FaultWall Proxy",
			"version": "demo-1.0",
			"routes":  "POST /query  GET /events  GET /stats  GET /health",
		})
	})

	handler := corsMiddleware(mux)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	logInfo("Listening on "+bold+":%s"+reset, port)
	logInfo("Routes: POST /query  |  GET /events  |  GET /stats  |  GET /health")
	fmt.Println()

	httpSrv := &http.Server{
		Addr:         ":" + port,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // 0 = no timeout (needed for SSE long-poll)
		IdleTimeout:  120 * time.Second,
	}

	if err := httpSrv.ListenAndServe(); err != nil {
		logError("Server exited: %v", err)
		os.Exit(1)
	}
}
