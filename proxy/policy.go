package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// ─────────────────────────────────────────────
//  YAML schema
// ─────────────────────────────────────────────

type Policy struct {
	Version string       `yaml:"version"`
	Agents  []AgentPolicy `yaml:"agents"`
	Global  GlobalPolicy  `yaml:"global"`
}

type AgentPolicy struct {
	ID             string   `yaml:"id"`
	AllowedOps     []string `yaml:"allowed_ops"`     // SELECT, INSERT, etc.
	DeniedOps      []string `yaml:"denied_ops"`      // takes priority over allowed_ops
	AllowedTables  []string `yaml:"allowed_tables"`  // empty = all
	DeniedTables   []string `yaml:"denied_tables"`   // takes priority
	RateLimit      int      `yaml:"rate_limit"`      // queries/min (0 = unlimited)
	Description    string   `yaml:"description"`
}

type GlobalPolicy struct {
	DefaultAction  string   `yaml:"default_action"` // "allow" | "deny"
	DeniedOps      []string `yaml:"denied_ops"`
	DeniedTables   []string `yaml:"denied_tables"`
}

// ─────────────────────────────────────────────
//  Decision
// ─────────────────────────────────────────────

type Decision struct {
	Allowed bool
	Reason  string
	Op      string
	Tables  []string
}

// ─────────────────────────────────────────────
//  Engine
// ─────────────────────────────────────────────

type PolicyEngine struct {
	policy Policy
}

func NewPolicyEngine() (*PolicyEngine, error) {
	paths := []string{"/app/policy.yaml", "./policy.yaml"}
	var data []byte
	var loadedFrom string
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err == nil {
			data = b
			loadedFrom = p
			break
		}
	}
	if data == nil {
		logWarn("No policy.yaml found — using built-in default (allow-all except DROP/TRUNCATE/ALTER)")
		return &PolicyEngine{policy: defaultPolicy()}, nil
	}

	var pol Policy
	if err := yaml.Unmarshal(data, &pol); err != nil {
		return nil, fmt.Errorf("parse policy.yaml: %w", err)
	}
	logInfo("Policy loaded from %s  (v%s, %d agent rules)", loadedFrom, pol.Version, len(pol.Agents))
	return &PolicyEngine{policy: pol}, nil
}

func defaultPolicy() Policy {
	return Policy{
		Version: "default",
		Global: GlobalPolicy{
			DefaultAction: "allow",
			DeniedOps:     []string{"DROP", "TRUNCATE", "ALTER"},
		},
	}
}

// ─────────────────────────────────────────────
//  Core check
// ─────────────────────────────────────────────

func (e *PolicyEngine) Check(agentID, query string) Decision {
	op := detectOp(query)
	tables := extractTables(query, op)

	// 1. Global denied ops
	if contains(e.policy.Global.DeniedOps, op) {
		return Decision{false, fmt.Sprintf("operation %s is globally forbidden", op), op, tables}
	}

	// 2. Global denied tables
	for _, t := range tables {
		if containsCI(e.policy.Global.DeniedTables, t) {
			return Decision{false, fmt.Sprintf("table '%s' is globally restricted", t), op, tables}
		}
	}

	// 3. Per-agent rules
	agent := e.findAgent(agentID)
	if agent != nil {
		// 3a. Denied ops per agent
		if containsCI(agent.DeniedOps, op) {
			return Decision{false, fmt.Sprintf("agent %s: operation %s is denied", agentID, op), op, tables}
		}
		// 3b. Denied tables per agent
		for _, t := range tables {
			if containsCI(agent.DeniedTables, t) {
				return Decision{false, fmt.Sprintf("agent %s: table '%s' is off-limits", agentID, t), op, tables}
			}
		}
		// 3c. Must be in allowed_ops (if specified)
		if len(agent.AllowedOps) > 0 && !containsCI(agent.AllowedOps, op) {
			return Decision{false, fmt.Sprintf("agent %s: operation %s not in allowed list %v", agentID, op, agent.AllowedOps), op, tables}
		}
		// 3d. Must target allowed tables (if specified)
		if len(agent.AllowedTables) > 0 {
			for _, t := range tables {
				if !containsCI(agent.AllowedTables, t) {
					return Decision{false, fmt.Sprintf("agent %s: table '%s' not in allowed list %v", agentID, t, agent.AllowedTables), op, tables}
				}
			}
		}
		return Decision{true, fmt.Sprintf("agent %s: permitted by policy", agentID), op, tables}
	}

	// 4. Fall through to global default
	if strings.EqualFold(e.policy.Global.DefaultAction, "deny") {
		return Decision{false, fmt.Sprintf("agent %s not registered — default action is deny", agentID), op, tables}
	}
	return Decision{true, "allowed by default policy", op, tables}
}

func (e *PolicyEngine) findAgent(id string) *AgentPolicy {
	for i := range e.policy.Agents {
		if strings.EqualFold(e.policy.Agents[i].ID, id) {
			return &e.policy.Agents[i]
		}
	}
	return nil
}

// ─────────────────────────────────────────────
//  SQL helpers
// ─────────────────────────────────────────────

var opPrefixes = []string{
	"SELECT", "INSERT", "UPDATE", "DELETE",
	"DROP", "TRUNCATE", "ALTER", "CREATE",
}

func detectOp(query string) string {
	q := strings.TrimSpace(strings.ToUpper(query))
	for _, op := range opPrefixes {
		if strings.HasPrefix(q, op) {
			return op
		}
	}
	return "UNKNOWN"
}

// extractTables uses simple regex heuristics – good enough for demo SQL.
var (
	reFrom    = regexp.MustCompile(`(?i)\bFROM\s+([\w"` + "`" + `]+)`)
	reJoin    = regexp.MustCompile(`(?i)\bJOIN\s+([\w"` + "`" + `]+)`)
	reInto    = regexp.MustCompile(`(?i)\bINTO\s+([\w"` + "`" + `]+)`)
	reUpdate  = regexp.MustCompile(`(?i)^UPDATE\s+([\w"` + "`" + `]+)`)
	reDrop    = regexp.MustCompile(`(?i)\bTABLE\s+([\w"` + "`" + `]+)`)
	reTrunc   = regexp.MustCompile(`(?i)^TRUNCATE\s+(?:TABLE\s+)?([\w"` + "`" + `]+)`)
)

func extractTables(query, op string) []string {
	seen := map[string]bool{}
	add := func(re *regexp.Regexp) {
		for _, m := range re.FindAllStringSubmatch(query, -1) {
			t := strings.Trim(m[1], `"` + "`")
			if t != "" {
				seen[strings.ToLower(t)] = true
			}
		}
	}
	add(reFrom)
	add(reJoin)
	add(reInto)
	add(reUpdate)
	add(reDrop)
	add(reTrunc)

	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	return out
}

// ─────────────────────────────────────────────
//  Slice helpers
// ─────────────────────────────────────────────

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func containsCI(slice []string, s string) bool {
	for _, v := range slice {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}
