-- Observability: inference logs, audit logs, provider health, webhooks, payloads, agent tracking
CREATE TABLE `inference_logs` (
  `id`                   text     NOT NULL,
  `org_id`               text     NOT NULL,
  `router_id`            text     NOT NULL DEFAULT '',
  `virtual_key_id`       text     NOT NULL DEFAULT '',
  `team_id`              text     NOT NULL DEFAULT '',
  `user_id`              text     NOT NULL DEFAULT '',
  `model_id`             text     NOT NULL DEFAULT '',
  `model_def_key`        text     NOT NULL DEFAULT '',
  `provider`             text     NOT NULL DEFAULT '',
  `input_tokens`         integer  NOT NULL DEFAULT 0,
  `output_tokens`        integer  NOT NULL DEFAULT 0,
  `cached_input_tokens`  integer  NOT NULL DEFAULT 0,
  `total_tokens`         integer  NOT NULL DEFAULT 0,
  `cost_usd`             real     NOT NULL DEFAULT 0,
  `latency_ms`           integer  NOT NULL DEFAULT 0,
  `ttft_ms`              integer  NOT NULL DEFAULT 0,
  `status`               text     NOT NULL DEFAULT 'success',
  `error_message`        text,
  `source`               text     NOT NULL DEFAULT 'direct',
  `selected_target_id`   text     NOT NULL DEFAULT '',
  `ab_variant`           text     NOT NULL DEFAULT '',
  `pipeline_trace`       text,
  `agent_session_id`     text     NOT NULL DEFAULT '',
  `agent`                text     NOT NULL DEFAULT '',
  `agent_role`           text     NOT NULL DEFAULT '',
  `parent_session_id`    text     NOT NULL DEFAULT '',
  `turn_index`           integer  NOT NULL DEFAULT 0,
  `tool_call_count`      integer  NOT NULL DEFAULT 0,
  `tool_result_chars`    integer  NOT NULL DEFAULT 0,
  `quality_score`        integer  NOT NULL DEFAULT 0,
  `context_fill_pct`     real     NOT NULL DEFAULT 0,
  `loop_detected`        integer  NOT NULL DEFAULT 0,
  `loop_reason`          text,
  `cache_hit`            integer  NOT NULL DEFAULT 0,
  `cache_hit_type`       text     NOT NULL DEFAULT '',
  `feedback`             integer  NOT NULL DEFAULT 0,
  `created_at`           datetime NOT NULL,
  PRIMARY KEY (`id`),
  FOREIGN KEY (`org_id`) REFERENCES `auth_organizations` (`id`) ON DELETE CASCADE
);
CREATE INDEX `idx_inference_logs_created_at`       ON `inference_logs` (`created_at`);
CREATE INDEX `idx_inference_logs_org_id`            ON `inference_logs` (`org_id`);
CREATE INDEX `idx_inference_logs_user_id`           ON `inference_logs` (`user_id`);
CREATE INDEX `idx_inference_logs_agent_session_id`  ON `inference_logs` (`agent_session_id`);
CREATE INDEX `idx_inference_logs_agent`             ON `inference_logs` (`agent`);
CREATE INDEX `idx_inference_logs_parent_session_id` ON `inference_logs` (`parent_session_id`);
CREATE INDEX `idx_inference_logs_loop_detected`     ON `inference_logs` (`loop_detected`);

CREATE TABLE `audit_logs` (
  `id`          text     NOT NULL,
  `org_id`      text     NOT NULL,
  `user_email`  text     NOT NULL DEFAULT '',
  `action`      text     NOT NULL,
  `resource`    text     NOT NULL,
  `resource_id` text     NOT NULL DEFAULT '',
  `details`     text,
  `ip_address`  text     NOT NULL DEFAULT '',
  `created_at`  datetime NOT NULL,
  PRIMARY KEY (`id`),
  FOREIGN KEY (`org_id`) REFERENCES `auth_organizations` (`id`) ON DELETE CASCADE
);
CREATE INDEX `idx_audit_logs_created_at` ON `audit_logs` (`created_at`);
CREATE INDEX `idx_audit_logs_org_id`     ON `audit_logs` (`org_id`);

CREATE TABLE `provider_health` (
  `model_id`      text     NOT NULL,
  `model_def_key` text     NOT NULL DEFAULT '',
  `provider`      text     NOT NULL DEFAULT '',
  `is_healthy`    integer  NOT NULL DEFAULT 1,
  `latency_ms`    integer  NOT NULL DEFAULT 0,
  `error_message` text,
  `checked_at`    datetime NOT NULL,
  PRIMARY KEY (`model_id`),
  FOREIGN KEY (`model_id`) REFERENCES `models` (`id`) ON DELETE CASCADE
);

CREATE TABLE `webhook_deliveries` (
  `id`          text     NOT NULL,
  `router_id`   text     NOT NULL,
  `event`       text     NOT NULL,
  `url`         text     NOT NULL,
  `status_code` integer  NOT NULL DEFAULT 0,
  `success`     integer  NOT NULL DEFAULT 0,
  `error_msg`   text,
  `created_at`  datetime NOT NULL,
  PRIMARY KEY (`id`)
);
CREATE INDEX `idx_webhook_deliveries_router_id`  ON `webhook_deliveries` (`router_id`);
CREATE INDEX `idx_webhook_deliveries_created_at` ON `webhook_deliveries` (`created_at`);

CREATE TABLE `inference_payloads` (
  `log_id`           text     NOT NULL,
  `router_id`        text     NOT NULL,
  `request_fields`   text,
  `response_content` text,
  `created_at`       datetime NOT NULL,
  PRIMARY KEY (`log_id`)
);
CREATE INDEX `idx_inference_payloads_router_id`  ON `inference_payloads` (`router_id`);
CREATE INDEX `idx_inference_payloads_created_at` ON `inference_payloads` (`created_at`);

CREATE TABLE `agent_session_events` (
  `id`               text     NOT NULL,
  `org_id`           text     NOT NULL,
  `router_id`        text     NOT NULL DEFAULT '',
  `virtual_key_id`   text     NOT NULL DEFAULT '',
  `team_id`          text     NOT NULL DEFAULT '',
  `user_id`          text     NOT NULL DEFAULT '',
  `agent_session_id` text     NOT NULL,
  `agent`            text     NOT NULL DEFAULT '',
  `event_type`       text     NOT NULL,
  `detail`           text,
  `created_at`       datetime NOT NULL,
  PRIMARY KEY (`id`)
);
CREATE INDEX `idx_agent_session_events_org_id`           ON `agent_session_events` (`org_id`);
CREATE INDEX `idx_agent_session_events_router_id`        ON `agent_session_events` (`router_id`);
CREATE INDEX `idx_agent_session_events_virtual_key_id`   ON `agent_session_events` (`virtual_key_id`);
CREATE INDEX `idx_agent_session_events_team_id`          ON `agent_session_events` (`team_id`);
CREATE INDEX `idx_agent_session_events_user_id`          ON `agent_session_events` (`user_id`);
CREATE INDEX `idx_agent_session_events_agent_session_id` ON `agent_session_events` (`agent_session_id`);
CREATE INDEX `idx_agent_session_events_agent`            ON `agent_session_events` (`agent`);
CREATE INDEX `idx_agent_session_events_event_type`       ON `agent_session_events` (`event_type`);
CREATE INDEX `idx_agent_session_events_created_at`       ON `agent_session_events` (`created_at`);

CREATE TABLE `tool_call_archives` (
  `id`               text     NOT NULL,
  `org_id`           text     NOT NULL,
  `router_id`        text     NOT NULL DEFAULT '',
  `log_id`           text     NOT NULL,
  `agent_session_id` text     NOT NULL DEFAULT '',
  `tool_name`        text     NOT NULL DEFAULT '',
  `tool_call_id`     text     NOT NULL DEFAULT '',
  `request_preview`  text,
  `request_payload`  text,
  `response_preview` text,
  `response_payload` text,
  `response_chars`   integer  NOT NULL DEFAULT 0,
  `error_message`    text,
  `archived`         integer  NOT NULL DEFAULT 0,
  `created_at`       datetime NOT NULL,
  PRIMARY KEY (`id`)
);
CREATE INDEX `idx_tool_call_archives_org_id`            ON `tool_call_archives` (`org_id`);
CREATE INDEX `idx_tool_call_archives_router_id`         ON `tool_call_archives` (`router_id`);
CREATE INDEX `idx_tool_call_archives_log_id`            ON `tool_call_archives` (`log_id`);
CREATE INDEX `idx_tool_call_archives_agent_session_id`  ON `tool_call_archives` (`agent_session_id`);
CREATE INDEX `idx_tool_call_archives_tool_name`         ON `tool_call_archives` (`tool_name`);
CREATE INDEX `idx_tool_call_archives_archived`          ON `tool_call_archives` (`archived`);
CREATE INDEX `idx_tool_call_archives_created_at`        ON `tool_call_archives` (`created_at`);

CREATE TABLE `compression_events` (
  `id`                     text     NOT NULL,
  `org_id`                 text     NOT NULL,
  `router_id`              text     NOT NULL DEFAULT '',
  `log_id`                 text     NOT NULL,
  `agent_session_id`       text     NOT NULL DEFAULT '',
  `feature_name`           text     NOT NULL,
  `before_chars`           integer  NOT NULL DEFAULT 0,
  `after_chars`            integer  NOT NULL DEFAULT 0,
  `saved_chars`            integer  NOT NULL DEFAULT 0,
  `estimated_tokens_saved` integer  NOT NULL DEFAULT 0,
  `exact`                  integer  NOT NULL DEFAULT 0,
  `quality_score`          integer  NOT NULL DEFAULT 0,
  `detail`                 text,
  `created_at`             datetime NOT NULL,
  PRIMARY KEY (`id`)
);
CREATE INDEX `idx_compression_events_org_id`            ON `compression_events` (`org_id`);
CREATE INDEX `idx_compression_events_router_id`         ON `compression_events` (`router_id`);
CREATE INDEX `idx_compression_events_log_id`            ON `compression_events` (`log_id`);
CREATE INDEX `idx_compression_events_agent_session_id`  ON `compression_events` (`agent_session_id`);
CREATE INDEX `idx_compression_events_feature_name`      ON `compression_events` (`feature_name`);
CREATE INDEX `idx_compression_events_created_at`        ON `compression_events` (`created_at`);
