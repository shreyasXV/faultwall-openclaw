-- ============================================================
-- FaultWall × OpenClaw Demo — Seed Data
-- ============================================================
-- Tables are intentionally partitioned by sensitivity:
--   SAFE    → feedback, products   (agents may read)
--   RESTRICTED → orders            (limited agents only)
--   BLOCKED → users, payments      (no agent access)
-- ============================================================

-- ── SAFE: feedback ──────────────────────────────────────────
CREATE TABLE IF NOT EXISTS public.feedback (
    id          SERIAL PRIMARY KEY,
    agent_id    TEXT        NOT NULL,
    session_id  TEXT        NOT NULL,
    rating      SMALLINT    CHECK (rating BETWEEN 1 AND 5),
    comment     TEXT,
    category    TEXT,
    created_at  TIMESTAMPTZ DEFAULT now()
);

INSERT INTO public.feedback (agent_id, session_id, rating, comment, category) VALUES
    ('openclaw-coder',    'sess-001', 5, 'Found the bug in 30 seconds. Incredible.',           'code-review'),
    ('openclaw-coder',    'sess-002', 4, 'Good refactoring suggestions, minor style issues.',  'code-review'),
    ('openclaw-research', 'sess-003', 5, 'Market analysis was thorough and well-cited.',       'research'),
    ('openclaw-research', 'sess-004', 3, 'Sources were a bit dated. Needs fresher data.',      'research'),
    ('openclaw-coder',    'sess-005', 5, 'Auto-generated unit tests saved hours.',             'testing'),
    ('openclaw-admin',    'sess-006', 4, 'Deployment script worked first try.',                'devops'),
    ('openclaw-coder',    'sess-007', 2, 'Hallucinated a function that does not exist.',       'code-review'),
    ('openclaw-research', 'sess-008', 5, 'Competitor teardown was detailed and actionable.',   'research'),
    ('openclaw-coder',    'sess-009', 4, 'SQL query optimization suggestion was spot on.',     'database'),
    ('openclaw-admin',    'sess-010', 5, 'Monitoring dashboard setup was seamless.',           'devops'),
    ('openclaw-coder',    'sess-011', 3, 'PR description was generic, not project-specific.',  'code-review'),
    ('openclaw-research', 'sess-012', 4, 'Literature review covered all major papers.',        'research'),
    ('openclaw-coder',    'sess-013', 5, 'Identified a race condition I had missed entirely.', 'testing'),
    ('openclaw-admin',    'sess-014', 4, 'Config migration ran cleanly on staging.',           'devops');


-- ── SAFE: products ──────────────────────────────────────────
CREATE TABLE IF NOT EXISTS public.products (
    id           SERIAL PRIMARY KEY,
    sku          TEXT        UNIQUE NOT NULL,
    name         TEXT        NOT NULL,
    description  TEXT,
    category     TEXT,
    price_cents  INTEGER     NOT NULL,
    stock        INTEGER     DEFAULT 0,
    active       BOOLEAN     DEFAULT true,
    created_at   TIMESTAMPTZ DEFAULT now()
);

INSERT INTO public.products (sku, name, description, category, price_cents, stock) VALUES
    ('OC-SKILL-001', 'FaultWall Shield',        'Firewall proxy for AI agent DB access',     'security',    0,    9999),
    ('OC-SKILL-002', 'DataVault Connector',     'Encrypted read-only data connector',        'data',        999,  4200),
    ('OC-SKILL-003', 'MemoryGraph Pro',         'Persistent long-term agent memory',         'memory',      1999, 1800),
    ('OC-SKILL-004', 'CodeReview Turbo',        'Async multi-file code review skill',        'dev',         499,  7500),
    ('OC-SKILL-005', 'ResearchPilot',           'Autonomous research and citation skill',    'research',    799,  3300),
    ('OC-SKILL-006', 'DeployGuard',             'Safe CI/CD execution with rollback',        'devops',      1499, 2100),
    ('OC-SKILL-007', 'SchemaBot',               'Auto schema documentation generator',       'dev',         299,  5600),
    ('OC-SKILL-008', 'AlertStream',             'Real-time anomaly alert forwarding',        'monitoring',  699,  880),
    ('OC-SKILL-009', 'PolicyEditor',            'Visual YAML policy editor for FaultWall',  'security',    1299, 1200),
    ('OC-SKILL-010', 'AuditTrail',              'Immutable query audit log exporter',        'compliance',  899,  3100),
    ('OC-SKILL-011', 'RateLimiter',             'Per-agent query rate limiting middleware',  'security',    599,  4400),
    ('OC-SKILL-012', 'MultiTenantProxy',        'Tenant-isolated DB proxy layer',            'security',    2499, 650);


-- ── RESTRICTED: orders ──────────────────────────────────────
CREATE TABLE IF NOT EXISTS public.orders (
    id             SERIAL PRIMARY KEY,
    order_ref      TEXT        UNIQUE NOT NULL,
    product_sku    TEXT        REFERENCES public.products(sku),
    quantity       INTEGER     NOT NULL DEFAULT 1,
    total_cents    INTEGER     NOT NULL,
    status         TEXT        CHECK (status IN ('pending','confirmed','shipped','delivered','cancelled')),
    customer_ref   TEXT        NOT NULL,   -- anonymised, no PII
    created_at     TIMESTAMPTZ DEFAULT now()
);

INSERT INTO public.orders (order_ref, product_sku, quantity, total_cents, status, customer_ref) VALUES
    ('ORD-2024-0001', 'OC-SKILL-001', 1,    0,    'delivered',  'cust-4f91'),
    ('ORD-2024-0002', 'OC-SKILL-003', 2, 3998,    'shipped',    'cust-8b22'),
    ('ORD-2024-0003', 'OC-SKILL-002', 1,  999,    'delivered',  'cust-1d77'),
    ('ORD-2024-0004', 'OC-SKILL-004', 5, 2495,    'confirmed',  'cust-c3e9'),
    ('ORD-2024-0005', 'OC-SKILL-006', 1, 1499,    'delivered',  'cust-5a14'),
    ('ORD-2024-0006', 'OC-SKILL-009', 2, 2598,    'pending',    'cust-7f60'),
    ('ORD-2024-0007', 'OC-SKILL-005', 3, 2397,    'shipped',    'cust-2b88'),
    ('ORD-2024-0008', 'OC-SKILL-011', 1,  599,    'delivered',  'cust-9d35'),
    ('ORD-2024-0009', 'OC-SKILL-010', 1,  899,    'confirmed',  'cust-6e02'),
    ('ORD-2024-0010', 'OC-SKILL-012', 1, 2499,    'pending',    'cust-3a71'),
    ('ORD-2024-0011', 'OC-SKILL-007', 10, 2990,   'delivered',  'cust-0c56'),
    ('ORD-2024-0012', 'OC-SKILL-008', 2, 1398,    'cancelled',  'cust-b1f4');


-- ── BLOCKED: users (PII) ─────────────────────────────────────
CREATE TABLE IF NOT EXISTS public.users (
    id           SERIAL PRIMARY KEY,
    email        TEXT        UNIQUE NOT NULL,
    full_name    TEXT        NOT NULL,
    password_hash TEXT       NOT NULL,
    api_key      TEXT        UNIQUE,
    role         TEXT        DEFAULT 'user',
    mfa_enabled  BOOLEAN     DEFAULT false,
    last_login   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ DEFAULT now()
);

INSERT INTO public.users (email, full_name, password_hash, api_key, role, mfa_enabled) VALUES
    ('alice@example.com',   'Alice Nguyen',    '$2b$12$KIX.fake.hash.alice',   'ak_live_alice001',  'admin', true),
    ('bob@example.com',     'Bob Okafor',      '$2b$12$KIX.fake.hash.bob',     'ak_live_bob002',    'user',  false),
    ('carol@example.com',   'Carol Martinez',  '$2b$12$KIX.fake.hash.carol',   'ak_live_carol003',  'user',  true),
    ('dave@example.com',    'Dave Chen',       '$2b$12$KIX.fake.hash.dave',    'ak_live_dave004',   'user',  false),
    ('eve@example.com',     'Eve Patel',       '$2b$12$KIX.fake.hash.eve',     'ak_live_eve005',    'admin', true),
    ('frank@example.com',   'Frank Kimura',    '$2b$12$KIX.fake.hash.frank',   'ak_live_frank006',  'user',  false),
    ('grace@example.com',   'Grace Liu',       '$2b$12$KIX.fake.hash.grace',   'ak_live_grace007',  'user',  true),
    ('henry@example.com',   'Henry Owens',     '$2b$12$KIX.fake.hash.henry',   'ak_live_henry008',  'user',  false),
    ('iris@example.com',    'Iris Santos',     '$2b$12$KIX.fake.hash.iris',    'ak_live_iris009',   'user',  true),
    ('jake@example.com',    'Jake Bergström',  '$2b$12$KIX.fake.hash.jake',    'ak_live_jake010',   'user',  false);


-- ── BLOCKED: payments (financial) ────────────────────────────
CREATE TABLE IF NOT EXISTS public.payments (
    id              SERIAL PRIMARY KEY,
    order_ref       TEXT        REFERENCES public.orders(order_ref),
    amount_cents    INTEGER     NOT NULL,
    currency        TEXT        DEFAULT 'USD',
    stripe_charge   TEXT        UNIQUE,
    card_last4      TEXT,
    status          TEXT        CHECK (status IN ('pending','succeeded','failed','refunded')),
    processed_at    TIMESTAMPTZ DEFAULT now()
);

INSERT INTO public.payments (order_ref, amount_cents, currency, stripe_charge, card_last4, status) VALUES
    ('ORD-2024-0001',    0,    'USD', 'ch_free_001',        NULL,   'succeeded'),
    ('ORD-2024-0002', 3998,    'USD', 'ch_3NbKl2KZ6eIau',  '4242', 'succeeded'),
    ('ORD-2024-0003',  999,    'USD', 'ch_3NbKl3KZ6eIau',  '0005', 'succeeded'),
    ('ORD-2024-0004', 2495,    'USD', 'ch_3NbKl4KZ6eIau',  '1234', 'pending'),
    ('ORD-2024-0005', 1499,    'USD', 'ch_3NbKl5KZ6eIau',  '9999', 'succeeded'),
    ('ORD-2024-0006', 2598,    'USD', 'ch_3NbKl6KZ6eIau',  '5500', 'pending'),
    ('ORD-2024-0007', 2397,    'USD', 'ch_3NbKl7KZ6eIau',  '1111', 'succeeded'),
    ('ORD-2024-0008',  599,    'USD', 'ch_3NbKl8KZ6eIau',  '3782', 'succeeded'),
    ('ORD-2024-0009',  899,    'USD', 'ch_3NbKl9KZ6eIau',  '6011', 'pending'),
    ('ORD-2024-0010', 2499,    'USD', 'ch_3NbKlAKZ6eIau',  '4000', 'pending'),
    ('ORD-2024-0011', 2990,    'USD', 'ch_3NbKlBKZ6eIau',  '7777', 'succeeded'),
    ('ORD-2024-0012', 1398,    'USD', 'ch_3NbKlCKZ6eIau',  '2222', 'refunded');
