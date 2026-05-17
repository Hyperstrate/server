-- AI: model registrations, configurations, key rotations, jobs, conversations, MCP servers
CREATE TABLE `models` (
  `id`                   text NOT NULL,
  `org_id`               text NOT NULL,
  `model_definition_key` text NOT NULL,
  `alias`                text,
  `created_at`           datetime,
  `modified_at`          datetime,
  PRIMARY KEY (`id`),
  FOREIGN KEY (`org_id`) REFERENCES `auth_organizations` (`id`) ON DELETE CASCADE
);
CREATE INDEX `idx_models_org_id`               ON `models` (`org_id`);
CREATE INDEX `idx_models_model_definition_key` ON `models` (`model_definition_key`);

CREATE TABLE `model_configurations` (
  `id`           text    NOT NULL,
  `model_id`     text    NOT NULL,
  `base_url`     text    NOT NULL,
  `api_key`      text,
  `api_secret`   text,
  `api_key_pool` text,
  `extra_headers` text,
  `timeout_secs` integer NOT NULL DEFAULT 30,
  `created_at`   datetime,
  `modified_at`  datetime,
  PRIMARY KEY (`id`),
  FOREIGN KEY (`model_id`) REFERENCES `models` (`id`) ON DELETE CASCADE
);
CREATE UNIQUE INDEX `idx_model_configurations_model_id` ON `model_configurations` (`model_id`);

CREATE TABLE `model_key_rotations` (
  `id`            text     NOT NULL,
  `model_id`      text     NOT NULL,
  `old_key_hint`  text     NOT NULL DEFAULT '',
  `new_key_hint`  text     NOT NULL DEFAULT '',
  `grace_ends_at` datetime NOT NULL,
  `created_at`    datetime NOT NULL,
  PRIMARY KEY (`id`)
);
CREATE INDEX `idx_model_key_rotations_model_id`   ON `model_key_rotations` (`model_id`);
CREATE INDEX `idx_model_key_rotations_created_at` ON `model_key_rotations` (`created_at`);

CREATE TABLE `jobs` (
  `id`            text    NOT NULL,
  `org_id`        text    NOT NULL,
  `model_id`      text    NOT NULL,
  `status`        text    NOT NULL DEFAULT 'PENDING',
  `fields`        text,
  `options`       text,
  `result`        text,
  `error_message` text,
  `callback_url`  text,
  `started_at`    datetime,
  `finished_at`   datetime,
  `created_at`    datetime,
  `modified_at`   datetime,
  PRIMARY KEY (`id`),
  FOREIGN KEY (`org_id`)   REFERENCES `auth_organizations` (`id`) ON DELETE CASCADE,
  FOREIGN KEY (`model_id`) REFERENCES `models`             (`id`) ON DELETE CASCADE
);
CREATE INDEX `idx_jobs_org_id`   ON `jobs` (`org_id`);
CREATE INDEX `idx_jobs_model_id` ON `jobs` (`model_id`);

CREATE TABLE `conversations` (
  `id`          text NOT NULL,
  `org_id`      text NOT NULL,
  `model_id`    text NOT NULL,
  `title`       text,
  `created_at`  datetime,
  `modified_at` datetime,
  PRIMARY KEY (`id`),
  FOREIGN KEY (`org_id`)   REFERENCES `auth_organizations` (`id`) ON DELETE CASCADE,
  FOREIGN KEY (`model_id`) REFERENCES `models`             (`id`) ON DELETE CASCADE
);
CREATE INDEX `idx_conversations_org_id`   ON `conversations` (`org_id`);
CREATE INDEX `idx_conversations_model_id` ON `conversations` (`model_id`);

CREATE TABLE `conversation_messages` (
  `id`              text NOT NULL,
  `conversation_id` text NOT NULL,
  `role`            text NOT NULL,
  `content`         text NOT NULL,
  `fields`          text,
  `created_at`      datetime,
  PRIMARY KEY (`id`),
  FOREIGN KEY (`conversation_id`) REFERENCES `conversations` (`id`) ON DELETE CASCADE
);
CREATE INDEX `idx_conversation_messages_conversation_id` ON `conversation_messages` (`conversation_id`);

CREATE TABLE `mcp_servers` (
  `id`            text    NOT NULL,
  `org_id`        text    NOT NULL,
  `name`          text    NOT NULL,
  `description`   text,
  `url`           text    NOT NULL,
  `auth_type`     text    NOT NULL DEFAULT 'none',
  `auth_token`    text,
  `auth_header`   text,
  `extra_headers` text,
  `timeout_secs`  integer NOT NULL DEFAULT 30,
  `created_at`    datetime,
  `modified_at`   datetime,
  PRIMARY KEY (`id`),
  FOREIGN KEY (`org_id`) REFERENCES `auth_organizations` (`id`) ON DELETE CASCADE
);
CREATE INDEX `idx_mcp_servers_org_id` ON `mcp_servers` (`org_id`);
