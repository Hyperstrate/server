-- Functions: apps, deployments, revisions, invocations, and logs
CREATE TABLE IF NOT EXISTS "function_apps" (
  "id"          text        NOT NULL,
  "org_id"      text        NOT NULL,
  "name"        text        NOT NULL,
  "description" text,
  "created_at"  timestamptz,
  "modified_at" timestamptz,
  PRIMARY KEY ("id"),
  FOREIGN KEY ("org_id") REFERENCES "auth_organizations" ("id") ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS "idx_function_apps_org_id" ON "function_apps" ("org_id");

CREATE TABLE IF NOT EXISTS "functions" (
  "id"                 text        NOT NULL,
  "org_id"             text        NOT NULL,
  "app_id"             text        NOT NULL,
  "name"               text        NOT NULL,
  "entrypoint"         text        NOT NULL,
  "status"             text        NOT NULL DEFAULT 'deploying',
  "active_revision_id" text        NOT NULL DEFAULT '',
  "created_at"         timestamptz,
  "modified_at"        timestamptz,
  PRIMARY KEY ("id"),
  FOREIGN KEY ("org_id") REFERENCES "auth_organizations" ("id") ON DELETE CASCADE,
  FOREIGN KEY ("app_id") REFERENCES "function_apps"      ("id") ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS "idx_functions_org_id" ON "functions" ("org_id");
CREATE INDEX IF NOT EXISTS "idx_functions_app_id" ON "functions" ("app_id");

CREATE TABLE IF NOT EXISTS "function_revisions" (
  "id"          text        NOT NULL,
  "org_id"      text        NOT NULL,
  "app_id"      text        NOT NULL,
  "function_id" text        NOT NULL,
  "version"     integer     NOT NULL DEFAULT 1,
  "entrypoint"  text        NOT NULL,
  "image"       jsonb,
  "runtime"     jsonb,
  "autoscaling" jsonb,
  "security"    jsonb,
  "secrets"     jsonb,
  "volumes"     jsonb,
  "provider"    jsonb,
  "build_id"    text        NOT NULL DEFAULT '',
  "created_at"  timestamptz,
  PRIMARY KEY ("id"),
  FOREIGN KEY ("org_id")      REFERENCES "auth_organizations" ("id") ON DELETE CASCADE,
  FOREIGN KEY ("app_id")      REFERENCES "function_apps"      ("id") ON DELETE CASCADE,
  FOREIGN KEY ("function_id") REFERENCES "functions"          ("id") ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS "idx_function_revisions_org_id"      ON "function_revisions" ("org_id");
CREATE INDEX IF NOT EXISTS "idx_function_revisions_app_id"      ON "function_revisions" ("app_id");
CREATE INDEX IF NOT EXISTS "idx_function_revisions_function_id" ON "function_revisions" ("function_id");
CREATE UNIQUE INDEX IF NOT EXISTS "idx_function_revisions_function_version" ON "function_revisions" ("org_id", "function_id", "version");

CREATE TABLE IF NOT EXISTS "function_builds" (
  "id"          text        NOT NULL,
  "org_id"      text        NOT NULL,
  "app_id"      text        NOT NULL,
  "function_id" text        NOT NULL,
  "revision_id" text        NOT NULL,
  "status"      text        NOT NULL DEFAULT 'queued',
  "source"      jsonb,
  "artifact"    jsonb,
  "error"       text,
  "started_at"  timestamptz,
  "finished_at" timestamptz,
  "created_at"  timestamptz,
  "modified_at" timestamptz,
  PRIMARY KEY ("id"),
  FOREIGN KEY ("org_id")      REFERENCES "auth_organizations"  ("id") ON DELETE CASCADE,
  FOREIGN KEY ("app_id")      REFERENCES "function_apps"       ("id") ON DELETE CASCADE,
  FOREIGN KEY ("function_id") REFERENCES "functions"           ("id") ON DELETE CASCADE,
  FOREIGN KEY ("revision_id") REFERENCES "function_revisions"  ("id") ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS "idx_function_builds_org_id"      ON "function_builds" ("org_id");
CREATE INDEX IF NOT EXISTS "idx_function_builds_app_id"      ON "function_builds" ("app_id");
CREATE INDEX IF NOT EXISTS "idx_function_builds_function_id" ON "function_builds" ("function_id");
CREATE INDEX IF NOT EXISTS "idx_function_builds_revision_id" ON "function_builds" ("revision_id");
CREATE INDEX IF NOT EXISTS "idx_function_builds_status"      ON "function_builds" ("status");

CREATE TABLE IF NOT EXISTS "function_invocations" (
  "id"          text        NOT NULL,
  "org_id"      text        NOT NULL,
  "app_id"      text        NOT NULL,
  "function_id" text        NOT NULL,
  "revision_id" text        NOT NULL,
  "runner_id"   text        NOT NULL DEFAULT '',
  "lease_id"    text        NOT NULL DEFAULT '',
  "mode"        text        NOT NULL DEFAULT 'sync',
  "status"      text        NOT NULL DEFAULT 'queued',
  "payload"     jsonb,
  "result"      jsonb,
  "error"       text,
  "attempt"     integer     NOT NULL DEFAULT 0,
  "max_attempts" integer    NOT NULL DEFAULT 1,
  "idempotency_key" text    NOT NULL DEFAULT '',
  "lease_expires_at" timestamptz,
  "started_at"  timestamptz,
  "finished_at" timestamptz,
  "created_at"  timestamptz,
  "modified_at" timestamptz,
  PRIMARY KEY ("id"),
  FOREIGN KEY ("org_id")      REFERENCES "auth_organizations"  ("id") ON DELETE CASCADE,
  FOREIGN KEY ("app_id")      REFERENCES "function_apps"       ("id") ON DELETE CASCADE,
  FOREIGN KEY ("function_id") REFERENCES "functions"           ("id") ON DELETE CASCADE,
  FOREIGN KEY ("revision_id") REFERENCES "function_revisions"  ("id") ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS "idx_function_invocations_org_id"      ON "function_invocations" ("org_id");
CREATE INDEX IF NOT EXISTS "idx_function_invocations_app_id"      ON "function_invocations" ("app_id");
CREATE INDEX IF NOT EXISTS "idx_function_invocations_function_id" ON "function_invocations" ("function_id");
CREATE INDEX IF NOT EXISTS "idx_function_invocations_revision_id" ON "function_invocations" ("revision_id");
CREATE INDEX IF NOT EXISTS "idx_function_invocations_status"      ON "function_invocations" ("status");
CREATE INDEX IF NOT EXISTS "idx_function_invocations_runner_id"   ON "function_invocations" ("runner_id");
CREATE INDEX IF NOT EXISTS "idx_function_invocations_lease_id"    ON "function_invocations" ("lease_id");
CREATE INDEX IF NOT EXISTS "idx_function_invocations_queue"       ON "function_invocations" ("org_id", "status", "lease_expires_at", "created_at");
CREATE INDEX IF NOT EXISTS "idx_function_invocations_runner_lease" ON "function_invocations" ("org_id", "runner_id", "lease_id");
CREATE UNIQUE INDEX IF NOT EXISTS "idx_function_invocations_idempotency_key" ON "function_invocations" ("org_id", "function_id", "idempotency_key") WHERE "idempotency_key" <> '';

CREATE TABLE IF NOT EXISTS "function_invocation_logs" (
  "id"            text        NOT NULL,
  "org_id"        text        NOT NULL,
  "invocation_id" text        NOT NULL,
  "seq"           bigint      NOT NULL DEFAULT 0,
  "stream"        text        NOT NULL,
  "message"       text        NOT NULL,
  "truncated"     boolean     NOT NULL DEFAULT false,
  "created_at"    timestamptz,
  PRIMARY KEY ("id"),
  FOREIGN KEY ("org_id")        REFERENCES "auth_organizations"     ("id") ON DELETE CASCADE,
  FOREIGN KEY ("invocation_id") REFERENCES "function_invocations"   ("id") ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS "idx_function_invocation_logs_org_id"        ON "function_invocation_logs" ("org_id");
CREATE INDEX IF NOT EXISTS "idx_function_invocation_logs_invocation_id" ON "function_invocation_logs" ("invocation_id");
CREATE INDEX IF NOT EXISTS "idx_function_invocation_logs_seq"           ON "function_invocation_logs" ("seq");
CREATE UNIQUE INDEX IF NOT EXISTS "idx_function_invocation_logs_invocation_seq" ON "function_invocation_logs" ("invocation_id", "seq");

CREATE TABLE IF NOT EXISTS "function_runner_pools" (
  "id"                   text        NOT NULL,
  "org_id"               text        NOT NULL,
  "name"                 text        NOT NULL,
  "provider"             text        NOT NULL,
  "region"               text        NOT NULL DEFAULT '',
  "status"               text        NOT NULL DEFAULT 'active',
  "capabilities"         jsonb,
  "bootstrap_token_hash" text        NOT NULL,
  "bootstrap_token_consumed_at" timestamptz,
  "created_at"           timestamptz,
  "modified_at"          timestamptz,
  PRIMARY KEY ("id"),
  FOREIGN KEY ("org_id") REFERENCES "auth_organizations" ("id") ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS "idx_function_runner_pools_org_id" ON "function_runner_pools" ("org_id");
CREATE UNIQUE INDEX IF NOT EXISTS "idx_function_runner_pools_bootstrap_token_hash" ON "function_runner_pools" ("bootstrap_token_hash");

CREATE TABLE IF NOT EXISTS "function_runner_agents" (
  "id"                 text        NOT NULL,
  "org_id"             text        NOT NULL,
  "pool_id"            text        NOT NULL,
  "hostname"           text        NOT NULL DEFAULT '',
  "public_key"         text        NOT NULL,
  "status"             text        NOT NULL DEFAULT 'online',
  "capabilities"       jsonb,
  "session_token_hash" text        NOT NULL,
  "session_expires_at" timestamptz NOT NULL,
  "last_heartbeat_at"  timestamptz,
  "created_at"         timestamptz,
  "modified_at"        timestamptz,
  PRIMARY KEY ("id"),
  FOREIGN KEY ("org_id")  REFERENCES "auth_organizations"      ("id") ON DELETE CASCADE,
  FOREIGN KEY ("pool_id") REFERENCES "function_runner_pools"   ("id") ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS "idx_function_runner_agents_org_id"  ON "function_runner_agents" ("org_id");
CREATE INDEX IF NOT EXISTS "idx_function_runner_agents_pool_id" ON "function_runner_agents" ("pool_id");
CREATE UNIQUE INDEX IF NOT EXISTS "idx_function_runner_agents_session_token_hash" ON "function_runner_agents" ("session_token_hash");
