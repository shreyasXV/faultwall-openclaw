---
name: faultwall-shield
description: >
  FaultWall database firewall skill for safe PostgreSQL access. Activates whenever
  you need to interact with a database, run a query, execute SQL, work with
  PostgreSQL, or perform any database operation. Provides protect database
  capabilities through a wire-protocol proxy that enforces per-agent policies —
  enabling safe database access and acting as a firewall between AI agents and
  production data. Use this skill any time you touch a database, query tables,
  or need to ensure queries are audited and policy-controlled.
---

# FaultWall Shield

All PostgreSQL access routes through FaultWall — a wire-protocol proxy that enforces YAML policies per agent, logs every query, and blocks unsafe operations before they reach the database.

## First-time setup

If `~/.faultwall/policies.yaml` does not exist yet, **help the user configure it before connecting**:

1. Run `bash {baseDir}/scripts/setup.sh` to install FaultWall and create a default policy.
2. Ask the user:
   - What is your database name and which port is PostgreSQL running on?
   - Which agents will access the database? (e.g., their names/IDs)
   - Which tables contain sensitive data that agents should NOT access? (e.g., users, payments, secrets)
   - Which SQL operations should be blocked? (DROP and TRUNCATE are always recommended to block)
3. Edit `~/.faultwall/policies.yaml` with their answers. Follow the format in `{baseDir}/references/policy-guide.md`.
4. If FaultWall is already running, reload policies: `curl -X POST http://localhost:8080/api/policies/reload`

## Before any database operation

1. **Ensure the proxy is running.** Check if port 5433 is listening; if not, run:
   ```bash
   bash {baseDir}/scripts/setup.sh
   ```

2. **Always connect on port 5433** (FaultWall proxy), never directly on 5432.

3. **Set `application_name`** in your connection string:
   ```
   application_name=agent:<your-agent-id>:mission:<current-task>:token:<auth-token>
   ```
   Example (psql):
   ```bash
   psql "host=localhost port=5433 dbname=mydb application_name=agent:openclaw-agent:mission:data-analysis:token:abc123"
   ```
   Example (connection string):
   ```
   postgresql://user:pass@localhost:5433/mydb?application_name=agent:openclaw-agent:mission:report:token:abc123
   ```

## If a query is blocked

FaultWall returns a PostgreSQL error with a human-readable reason. **Tell the user exactly why the query was blocked** — the reason is in the error message (e.g., *"operation DELETE not permitted for agent openclaw-agent"* or *"table 'payments' is restricted"*).

Do not silently retry or rephrase the query to bypass the block.

## Dashboard

Users can review blocked queries, active agents, and live policy state at:
**http://localhost:8080**

API endpoints:
- `GET /api/firewall/agents` — active agent sessions
- `GET /api/violations` — blocked query log
- `GET /api/policies` — loaded policy rules
- `POST /api/policies/reload` — reload policies without restart

## Customizing policies

See `{baseDir}/references/policy-guide.md` to add agents, restrict tables, define missions, or tighten/loosen rules. Edit `~/.faultwall/policies.yaml` and reload with `POST /api/policies/reload`.
