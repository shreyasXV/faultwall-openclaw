# 🛡️ FaultWall for OpenClaw

**ClawHub skill that gives AI agents safe PostgreSQL access through identity-aware policy enforcement.**

[![FaultWall](https://img.shields.io/badge/powered%20by-FaultWall-orange)](https://github.com/shreyasXV/faultwall)
[![ClawHub Skill](https://img.shields.io/badge/ClawHub-faultwall--shield-blue)](https://clawhub.ai)
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

---

## The Problem

Your OpenClaw agent has database access. One prompt injection, and it runs:

```sql
DROP TABLE users;
DELETE FROM payments;
SELECT * FROM secrets;
```

**FaultWall stops this at the wire level.** It's a PostgreSQL proxy that intercepts every query, checks per-agent policies, and blocks anything destructive — before it ever reaches your database.

## Architecture

```
┌─────────────────┐     ┌──────────────────────┐     ┌──────────────┐
│                  │     │                      │     │              │
│  OpenClaw Agent  │────▶│  FaultWall Proxy      │────▶│  PostgreSQL  │
│  (port 5433)     │     │  :5433               │     │  :5432       │
│                  │     │                      │     │              │
└─────────────────┘     │  ✅ SELECT feedback   │     └──────────────┘
                        │  🚫 DROP TABLE users  │
                        │  🚫 SELECT payments   │
                        │  ✅ INSERT products   │
                        └──────┬───────────────┘
                               │
                        ┌──────▼───────────────┐
                        │  FaultWall Dashboard  │
                        │  :8080               │
                        │  Live query feed      │
                        │  Agent breakdown      │
                        │  Violation log        │
                        └──────────────────────┘
```

## Quick Start (ClawHub Skill)

### 1. Install the skill

```bash
openclaw skills install faultwall-shield
```

### 2. Configure your policy

Edit `~/.faultwall/policies.yaml`:

```yaml
default_policy: deny

agents:
  openclaw-coder:
    description: "OpenClaw coding agent"
    blocked_operations: [DROP, TRUNCATE, DELETE, ALTER]
    blocked_tables: [public.users, public.payments]
    missions:
      code-review:
        tables: ["public.feedback", "public.products"]
        max_rows: 1000
```

### 3. Done

Your OpenClaw agent now routes all DB queries through FaultWall. Blocked queries return a clear reason. Check the dashboard at `http://localhost:8080`.

## Docker Demo

Try it locally with the full stack:

```bash
git clone --recursive https://github.com/shreyasXV/faultwall-openclaw.git
cd faultwall-openclaw
docker-compose up
```

This starts:
- **PostgreSQL** on port 5432 (with seed data)
- **FaultWall Proxy** on port 5433 (wire-protocol interception)
- **FaultWall Dashboard** on port 8080 (built-in monitoring)
- **Live Dashboard** on port 3000 (real-time attack visualization)

### Run the attack demo

```bash
chmod +x demo/attack-demo.sh
./demo/attack-demo.sh
```

Runs 5 scenarios through the proxy — legitimate reads, DROP TABLE attacks, rogue agents, PII access attempts.

## How It Works

FaultWall is a **wire-protocol PostgreSQL proxy** (not a REST wrapper). It speaks the actual PostgreSQL protocol, so:

1. **Transparent to the agent** — the agent connects to port 5433 thinking it's Postgres
2. **Identity-aware** — agents identify via `application_name`: `agent:<id>:mission:<task>:token:<secret>`
3. **Policy enforcement** — YAML policies define what each agent can do (tables, operations, missions)
4. **Real-time blocking** — destructive queries are killed before they reach the database
5. **Full observability** — dashboard shows every query, every agent, every violation

## Why This Matters

The **Confused Deputy Problem**: Your AI agent has legitimate database credentials. It's supposed to read feedback data. But via prompt injection, it gets tricked into running `DROP TABLE users`. The credentials are valid — Postgres can't tell the difference.

FaultWall can. It knows *which agent* is running *which query* for *which mission*, and enforces policies at the SQL level.

## Project Structure

```
faultwall-openclaw/
├── skill/                    # ClawHub skill (installable)
│   ├── SKILL.md              # Skill definition
│   ├── scripts/
│   │   ├── setup.sh          # Downloads & starts FaultWall
│   │   ├── status.sh         # Health check & stats
│   │   └── default-policy.yaml
│   └── references/
│       └── policy-guide.md   # Policy authoring guide
├── faultwall/                # Real FaultWall (git submodule)
├── dashboard/                # Live visualization dashboard
│   ├── index.html            # Real-time query feed & attack sim
│   └── nginx.conf
├── demo/                     # Docker demo setup
│   ├── init.sql              # Seed data
│   ├── policies.yaml         # Demo agent policies
│   └── attack-demo.sh        # 5 attack scenarios
└── docker-compose.yml
```

## Links

- **FaultWall** (core): [github.com/shreyasXV/faultwall](https://github.com/shreyasXV/faultwall)
- **ClawHub**: [clawhub.ai](https://clawhub.ai)
- **Built at**: [Cage the Claw](https://sf.aitinkerers.org/p/build-night-cage-the-claw) — AI Tinkerers SF, April 4, 2026

## License

MIT
