-- Routers: routers, configurations, targets, features, interceptors, team access, evaluations
CREATE TABLE IF NOT EXISTS "routers" (
  "id"                text    NOT NULL,
  "org_id"            text    NOT NULL,
  "name"              text    NOT NULL,
  "description"       text,
  "status"            text    NOT NULL DEFAULT 'draft',
  "strategy"          text    NOT NULL DEFAULT 'round_robin',
  "round_robin_index" integer NOT NULL DEFAULT 0,
  "created_at"        timestamptz,
  "modified_at"       timestamptz,
  PRIMARY KEY ("id"),
  FOREIGN KEY ("org_id") REFERENCES "auth_organizations" ("id") ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS "idx_routers_org_id" ON "routers" ("org_id");

CREATE TABLE IF NOT EXISTS "router_configurations" (
  "router_id"      text    NOT NULL,
  "webhook_url"    text    NOT NULL DEFAULT '',
  "prompt_id"      text,
  "store_payloads" boolean NOT NULL DEFAULT false,
  "modified_at"    timestamptz,
  PRIMARY KEY ("router_id"),
  FOREIGN KEY ("router_id") REFERENCES "routers"  ("id") ON DELETE CASCADE,
  FOREIGN KEY ("prompt_id") REFERENCES "prompts"  ("id") ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS "router_targets" (
  "id"          text    NOT NULL,
  "router_id"   text    NOT NULL,
  "model_id"    text    NOT NULL,
  "weight"      integer NOT NULL DEFAULT 1,
  "percentage"  numeric NOT NULL DEFAULT 0,
  "priority"    integer NOT NULL DEFAULT 0,
  "is_enabled"  boolean NOT NULL DEFAULT true,
  "prompt_id"   text,
  "created_at"  timestamptz,
  "modified_at" timestamptz,
  PRIMARY KEY ("id"),
  FOREIGN KEY ("router_id") REFERENCES "routers" ("id") ON DELETE CASCADE,
  FOREIGN KEY ("model_id")  REFERENCES "models"  ("id") ON DELETE CASCADE,
  FOREIGN KEY ("prompt_id") REFERENCES "prompts" ("id") ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS "idx_router_targets_router_id" ON "router_targets" ("router_id");

CREATE TABLE IF NOT EXISTS "router_features" (
  "id"              text    NOT NULL,
  "router_id"       text    NOT NULL,
  "feature_type"    text    NOT NULL,
  "config"          jsonb,
  "execution_order" integer NOT NULL DEFAULT 0,
  "is_enabled"      boolean NOT NULL DEFAULT true,
  "created_at"      timestamptz,
  "modified_at"     timestamptz,
  PRIMARY KEY ("id"),
  FOREIGN KEY ("router_id") REFERENCES "routers" ("id") ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS "idx_router_features_router_id" ON "router_features" ("router_id");

CREATE TABLE IF NOT EXISTS "router_interceptors" (
  "id"              text    NOT NULL,
  "router_id"       text    NOT NULL,
  "type"            text    NOT NULL,
  "config"          jsonb,
  "execution_order" integer NOT NULL DEFAULT 0,
  "is_enabled"      boolean NOT NULL DEFAULT true,
  "created_at"      timestamptz,
  "modified_at"     timestamptz,
  PRIMARY KEY ("id"),
  FOREIGN KEY ("router_id") REFERENCES "routers" ("id") ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS "idx_router_interceptors_router_id" ON "router_interceptors" ("router_id");

CREATE TABLE IF NOT EXISTS "router_team_accesses" (
  "id"        text NOT NULL,
  "router_id" text NOT NULL,
  "team_id"   text NOT NULL,
  "org_id"    text NOT NULL,
  PRIMARY KEY ("id"),
  UNIQUE ("router_id", "team_id")
);
CREATE INDEX IF NOT EXISTS "idx_router_team_accesses_router_id" ON "router_team_accesses" ("router_id");

CREATE TABLE IF NOT EXISTS "router_evaluations" (
  "id"          text NOT NULL,
  "org_id"      text NOT NULL,
  "router_id"   text NOT NULL,
  "name"        text NOT NULL,
  "description" text,
  "created_at"  timestamptz,
  "modified_at" timestamptz,
  PRIMARY KEY ("id")
);
CREATE INDEX IF NOT EXISTS "idx_router_evaluations_org_id"    ON "router_evaluations" ("org_id");
CREATE INDEX IF NOT EXISTS "idx_router_evaluations_router_id" ON "router_evaluations" ("router_id");

CREATE TABLE IF NOT EXISTS "router_evaluation_cases" (
  "id"           text NOT NULL,
  "eval_id"      text NOT NULL,
  "fields"       jsonb,
  "expected"     text,
  "score_method" text NOT NULL DEFAULT 'contains',
  "description"  text,
  "created_at"   timestamptz,
  PRIMARY KEY ("id")
);
CREATE INDEX IF NOT EXISTS "idx_router_evaluation_cases_eval_id" ON "router_evaluation_cases" ("eval_id");

CREATE TABLE IF NOT EXISTS "router_evaluation_runs" (
  "id"           text    NOT NULL,
  "eval_id"      text    NOT NULL,
  "router_id"    text    NOT NULL,
  "total_cases"  integer NOT NULL DEFAULT 0,
  "passed_cases" integer NOT NULL DEFAULT 0,
  "avg_score"    numeric NOT NULL DEFAULT 0,
  "results"      jsonb,
  "created_at"   timestamptz,
  PRIMARY KEY ("id")
);
CREATE INDEX IF NOT EXISTS "idx_router_evaluation_runs_eval_id" ON "router_evaluation_runs" ("eval_id");
