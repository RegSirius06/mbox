-- Authoritative SQLite schema used by mboxctl.
CREATE TABLE users (
  id           TEXT PRIMARY KEY,
  psk          BLOB NOT NULL CHECK(length(psk) = 32),
  enabled      INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
  label        TEXT NOT NULL DEFAULT '',
  note         TEXT NOT NULL DEFAULT '',
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL,
  rotated_at   TEXT,
  revoked_at   TEXT,
  -- 0 = server default, -1 = unlimited, N > 0 = N concurrent MBox sessions.
  max_sessions INTEGER NOT NULL DEFAULT 0,
  -- RFC3339 UTC timestamp. NULL means no expiry.
  expires_at   TEXT
);
CREATE INDEX idx_users_enabled ON users(enabled);
