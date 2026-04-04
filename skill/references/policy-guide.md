# FaultWall Policy Guide

FaultWall enforces per-agent rules by intercepting PostgreSQL wire-protocol traffic. Policies are defined in a single YAML file (default: `~/.faultwall/policies.yaml`). Changes take effect without a restart — just reload:

```bash
curl -X POST http://localhost:8080/api/policies/reload
```

---

## File structure

```yaml
default_policy: deny          # "deny" or "allow" — what happens to agents not listed

blocked_functions:            # Globally blocked PG built-in functions (all agents)
  - pg_read_file
  - ...

agents:
  - id: my-agent              # Must match the agent_id in application_name
    description: "..."
    token: "secret-token"     # Must match the token in application_name
    blocked_operations: [...]
    allowed_operations: [...]
    blocked_tables: [...]
    missions:                 # Optional scoped permissions per mission
      - id: reporting
        ...
```

---

## Agent identity

Agents identify via the PostgreSQL `application_name` connection parameter:

```
agent:<agent_id>:mission:<mission_id>:token:<token>
```

Example:
```bash
psql "host=localhost port=5433 dbname=mydb \
      application_name=agent:openclaw-agent:mission:data-analysis:token:abc123"
```

FaultWall parses this string and looks up the `agent_id` in the policy file, validating the `token` before allowing any queries.

---

## Operations

FaultWall recognises these SQL operation types:

| Operation   | Covers                              |
|-------------|-------------------------------------|
| `SELECT`    | `SELECT`, `WITH ... SELECT`         |
| `INSERT`    | `INSERT INTO`                       |
| `UPDATE`    | `UPDATE ... SET`                    |
| `DELETE`    | `DELETE FROM`                       |
| `DROP`      | `DROP TABLE`, `DROP DATABASE`, etc. |
| `TRUNCATE`  | `TRUNCATE TABLE`                    |
| `ALTER`     | `ALTER TABLE`, `ALTER COLUMN`, etc. |
| `CREATE`    | `CREATE TABLE`, `CREATE INDEX`, etc.|
| `COPY`      | `COPY TO/FROM`                      |

Use `blocked_operations` to deny specific operations for an agent, or `allowed_operations` to whitelist (everything else is denied).

---

## Adding a new agent

```yaml
agents:
  - id: analyst-bot
    description: "BI analyst agent — read-only, no sensitive tables"
    token: "s3cr3t-t0k3n"
    allowed_operations:
      - SELECT
    blocked_tables:
      - users
      - payments
      - audit_logs
      - secrets
```

Then reload:
```bash
curl -X POST http://localhost:8080/api/policies/reload
```

---

## Restricting tables

`blocked_tables` prevents the agent from touching specific tables regardless of operation:

```yaml
agents:
  - id: reporting-agent
    token: "rpt-token"
    blocked_operations:
      - DROP
      - TRUNCATE
      - DELETE
    blocked_tables:
      - pg_catalog        # Never let agents inspect system catalogs
      - users             # PII
      - payments          # Financial data
      - sessions          # Auth tokens
```

---

## Missions (scoped access)

Missions let you grant an agent a narrower or wider permission set for a specific task, identified by the `mission_id` in `application_name`.

```yaml
agents:
  - id: etl-agent
    token: "etl-tok"
    blocked_operations:        # Default for this agent
      - DROP
      - TRUNCATE
    missions:
      - id: nightly-import     # application_name must include mission:nightly-import
        description: "Bulk import job — allowed to INSERT and UPDATE staging tables"
        allowed_tables:
          - staging_orders
          - staging_products
        allowed_operations:
          - SELECT
          - INSERT
          - UPDATE
      - id: read-only-report
        allowed_operations:
          - SELECT
        blocked_tables:
          - users
          - payments
```

If the agent connects with `mission:nightly-import`, only the mission rules apply; if it connects with an unknown mission, the agent-level defaults apply.

---

## Blocking dangerous PostgreSQL functions

`blocked_functions` is a global list (applies to all agents) that prevents calling specific PostgreSQL server-side functions by name:

```yaml
blocked_functions:
  # Filesystem access
  - pg_read_file
  - pg_write_file
  - pg_read_binary_file
  - pg_ls_dir

  # OS command execution
  - pg_execute_server_program

  # Large objects (file I/O via SQL)
  - lo_import
  - lo_export
  - lo_creat

  # Foreign connections / lateral movement
  - dblink
  - dblink_exec
  - dblink_connect

  # COPY abuse (can read/write server files)
  - copy_from
  - copy_to
```

> **Tip:** Even if an agent has broad SELECT permission, `blocked_functions` prevents them from using `SELECT pg_read_file('/etc/passwd')`.

---

## Starter policy template

Copy this into `~/.faultwall/policies.yaml` and adjust as needed:

```yaml
# ── FaultWall policy template ─────────────────────────────────────
# Reload without restart: POST http://localhost:8080/api/policies/reload

default_policy: deny   # Deny any agent not explicitly listed below

# ── Global function blocklist (all agents) ────────────────────────
blocked_functions:
  - pg_read_file
  - pg_write_file
  - pg_read_binary_file
  - pg_ls_dir
  - pg_execute_server_program
  - lo_import
  - lo_export
  - lo_creat
  - dblink
  - dblink_exec
  - dblink_connect
  - copy_from
  - copy_to

# ── Agents ────────────────────────────────────────────────────────
agents:

  # Read-only analyst — can SELECT everywhere except sensitive tables
  - id: analyst-agent
    description: "BI / reporting agent"
    token: "replace-me-analyst"
    allowed_operations:
      - SELECT
    blocked_tables:
      - users
      - payments
      - sessions
      - audit_logs
      - pg_catalog

  # Application agent — can read/write product tables, not financial
  - id: app-agent
    description: "Main application backend agent"
    token: "replace-me-app"
    blocked_operations:
      - DROP
      - TRUNCATE
      - ALTER
      - CREATE
    blocked_tables:
      - pg_catalog
      - payments
      - users
    missions:
      - id: checkout
        description: "Allowed to read payments during checkout flow"
        allowed_tables:
          - payments
          - orders
          - products
        allowed_operations:
          - SELECT
          - INSERT

  # Admin agent — broad access, still blocked from DROP/TRUNCATE in prod
  - id: admin-agent
    description: "Human-supervised admin operations"
    token: "replace-me-admin"
    blocked_operations:
      - DROP
      - TRUNCATE
```

---

## Reference: demo-policies.yaml

The demo repository ships `demo-policies.yaml` as a working example. Run FaultWall against it with:

```bash
faultwall --proxy --listen 0.0.0.0:5433 --upstream localhost:5432 \
          --policies /path/to/demo-policies.yaml
```

You can inspect the currently loaded policies at any time:
```bash
curl http://localhost:8080/api/policies | python3 -m json.tool
```
