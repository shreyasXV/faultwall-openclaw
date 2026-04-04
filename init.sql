-- =============================================================
-- FaultWall × OpenClaw Demo — Database Init Script
-- PostgreSQL 16
-- =============================================================

-- ─────────────────────────────────────────────
-- TABLES
-- ─────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS customers (
    id          SERIAL PRIMARY KEY,
    name        TEXT        NOT NULL,
    email       TEXT        NOT NULL UNIQUE,
    tier        TEXT        NOT NULL DEFAULT 'free'
                            CHECK (tier IN ('free', 'pro', 'enterprise')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS orders (
    id          SERIAL PRIMARY KEY,
    customer_id INTEGER     NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    amount      NUMERIC(10, 2) NOT NULL CHECK (amount > 0),
    status      TEXT        NOT NULL DEFAULT 'pending'
                            CHECK (status IN ('pending', 'paid', 'shipped', 'cancelled', 'refunded')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- This table should NEVER be accessible to AI agents
-- FaultWall policy blocks all agents except admin-agent from reading it
CREATE TABLE IF NOT EXISTS secrets (
    id              SERIAL PRIMARY KEY,
    key             TEXT NOT NULL UNIQUE,
    value           TEXT NOT NULL,
    classification  TEXT NOT NULL DEFAULT 'restricted'
                    CHECK (classification IN ('restricted', 'top-secret', 'internal'))
);

-- ─────────────────────────────────────────────
-- SEED: 20 Customers
-- ─────────────────────────────────────────────

INSERT INTO customers (name, email, tier) VALUES
    ('Alice Nguyen',       'alice.nguyen@example.com',     'enterprise'),
    ('Bob Okafor',         'bob.okafor@example.com',       'pro'),
    ('Carol Steinberg',    'carol.stein@example.com',      'enterprise'),
    ('David Kim',          'david.kim@example.com',        'free'),
    ('Elena Vasquez',      'elena.v@example.com',          'pro'),
    ('Frank O''Brien',     'frank.obrien@example.com',     'free'),
    ('Grace Liu',          'grace.liu@example.com',        'enterprise'),
    ('Hiroshi Tanaka',     'hiroshi.t@example.com',        'pro'),
    ('Isabelle Moreau',    'isabelle.m@example.com',       'free'),
    ('James Patel',        'james.patel@example.com',      'enterprise'),
    ('Kiran Shah',         'kiran.shah@example.com',       'pro'),
    ('Layla Hassan',       'layla.hassan@example.com',     'free'),
    ('Marcus Webb',        'marcus.webb@example.com',      'pro'),
    ('Nadia Rossi',        'nadia.rossi@example.com',      'enterprise'),
    ('Oscar Fernandez',    'oscar.f@example.com',          'free'),
    ('Priya Mehta',        'priya.mehta@example.com',      'pro'),
    ('Quinn Zhang',        'quinn.zhang@example.com',      'enterprise'),
    ('Rafael Souza',       'rafael.s@example.com',         'free'),
    ('Sara Johansson',     'sara.j@example.com',           'pro'),
    ('Thomas Andersen',    'thomas.a@example.com',         'enterprise');

-- ─────────────────────────────────────────────
-- SEED: 50 Orders (spread across customers)
-- ─────────────────────────────────────────────

INSERT INTO orders (customer_id, amount, status) VALUES
    -- Alice (enterprise, big orders)
    (1,  4250.00, 'paid'),
    (1,  8900.50, 'shipped'),
    (1,  1200.00, 'paid'),
    -- Bob
    (2,   349.99, 'paid'),
    (2,   799.00, 'shipped'),
    -- Carol (enterprise)
    (3,  12000.00, 'paid'),
    (3,   5600.00, 'shipped'),
    (3,    750.00, 'pending'),
    -- David (free tier, small orders)
    (4,    29.99, 'paid'),
    (4,    14.99, 'cancelled'),
    -- Elena
    (5,   599.00, 'paid'),
    (5,  1450.00, 'shipped'),
    -- Frank
    (6,    49.99, 'paid'),
    -- Grace (enterprise)
    (7,  9800.00, 'paid'),
    (7,  3200.00, 'shipped'),
    (7,  6100.50, 'paid'),
    -- Hiroshi
    (8,   420.00, 'paid'),
    (8,   880.00, 'pending'),
    -- Isabelle
    (9,    75.00, 'refunded'),
    -- James (enterprise, high value)
    (10, 15000.00, 'paid'),
    (10,  7800.00, 'shipped'),
    (10,  3400.00, 'pending'),
    -- Kiran
    (11,  1100.00, 'paid'),
    (11,   560.00, 'shipped'),
    -- Layla
    (12,   220.00, 'paid'),
    (12,    95.00, 'cancelled'),
    -- Marcus
    (13,  2200.00, 'paid'),
    (13,  1750.00, 'shipped'),
    -- Nadia (enterprise)
    (14,  6600.00, 'paid'),
    (14,  4400.00, 'paid'),
    -- Oscar
    (15,   199.99, 'paid'),
    -- Priya
    (16,  1850.00, 'shipped'),
    (16,   430.00, 'pending'),
    -- Quinn (enterprise)
    (17, 22000.00, 'paid'),
    (17,  9500.00, 'shipped'),
    (17,  5100.00, 'paid'),
    -- Rafael
    (18,   120.00, 'paid'),
    -- Sara
    (19,  1300.00, 'paid'),
    (19,   670.00, 'shipped'),
    -- Thomas (enterprise)
    (20, 18000.00, 'paid'),
    (20,  8200.00, 'shipped'),
    (20,  4300.00, 'pending'),
    -- Extra spread orders
    (1,   500.00, 'pending'),
    (3,   950.00, 'refunded'),
    (5,   175.00, 'paid'),
    (7,  2800.00, 'paid'),
    (10,  6600.00, 'cancelled'),
    (14,  1100.00, 'shipped'),
    (17,  3300.00, 'paid');

-- ─────────────────────────────────────────────
-- SEED: 5 Secrets (RESTRICTED — FaultWall blocks agents from reading these)
-- ─────────────────────────────────────────────

INSERT INTO secrets (key, value, classification) VALUES
    ('STRIPE_SECRET_KEY',       'REDACTED-demo-key-001',   'restricted'),
    ('OPENAI_API_KEY',          'REDACTED-demo-key-002',   'restricted'),
    ('DATABASE_MASTER_PASSWORD','REDACTED-demo-password',   'top-secret'),
    ('JWT_SIGNING_SECRET',      'REDACTED-demo-jwt-secret',  'restricted'),
    ('WEBHOOK_SIGNING_KEY',     'REDACTED-demo-webhook-key',  'restricted');

-- ─────────────────────────────────────────────
-- INDEXES for query performance
-- ─────────────────────────────────────────────

CREATE INDEX IF NOT EXISTS idx_orders_customer_id ON orders(customer_id);
CREATE INDEX IF NOT EXISTS idx_orders_status      ON orders(status);
CREATE INDEX IF NOT EXISTS idx_customers_tier     ON customers(tier);

-- ─────────────────────────────────────────────
-- Confirm seeded data
-- ─────────────────────────────────────────────

DO $$
DECLARE
    c_count INT;
    o_count INT;
    s_count INT;
BEGIN
    SELECT COUNT(*) INTO c_count FROM customers;
    SELECT COUNT(*) INTO o_count FROM orders;
    SELECT COUNT(*) INTO s_count FROM secrets;
    RAISE NOTICE 'Seeded: % customers, % orders, % secrets', c_count, o_count, s_count;
END $$;
