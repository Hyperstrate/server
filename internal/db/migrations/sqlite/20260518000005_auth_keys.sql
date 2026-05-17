-- Auth keys: virtual keys and API keys
CREATE TABLE `auth_virtual_keys` (
  `id`             text    NOT NULL,
  `org_id`         text    NOT NULL,
  `router_id`      text    NOT NULL,
  `team_id`        text,
  `name`           text    NOT NULL,
  `description`    text,
  `key_hash`       text    NOT NULL,
  `max_requests`   integer NOT NULL DEFAULT 0,
  `max_cost_usd`   real    NOT NULL DEFAULT 0,
  `reset_period`   text    NOT NULL DEFAULT '',
  `rate_limit_rps` real    NOT NULL DEFAULT 0,
  `is_enabled`     integer NOT NULL DEFAULT 1,
  `created_at`     datetime,
  `modified_at`    datetime,
  PRIMARY KEY (`id`),
  FOREIGN KEY (`org_id`)    REFERENCES `auth_organizations` (`id`) ON DELETE CASCADE,
  FOREIGN KEY (`router_id`) REFERENCES `routers`            (`id`) ON DELETE CASCADE,
  FOREIGN KEY (`team_id`)   REFERENCES `auth_teams`         (`id`) ON DELETE SET NULL
);
CREATE UNIQUE INDEX `idx_auth_virtual_keys_key_hash` ON `auth_virtual_keys` (`key_hash`);

CREATE TABLE `auth_api_keys` (
  `id`             text    NOT NULL,
  `org_id`         text    NOT NULL,
  `team_id`        text    NOT NULL,
  `router_id`      text    NOT NULL DEFAULT '',
  `virtual_key_id` text    NOT NULL DEFAULT '',
  `name`           text    NOT NULL,
  `description`    text,
  `key_hash`       text    NOT NULL,
  `scope`          text    NOT NULL DEFAULT 'router',
  `expires_at`     datetime,
  `last_used_at`   datetime,
  `is_enabled`     integer NOT NULL DEFAULT 1,
  `created_at`     datetime,
  `modified_at`    datetime,
  PRIMARY KEY (`id`),
  FOREIGN KEY (`org_id`)  REFERENCES `auth_organizations` (`id`) ON DELETE CASCADE,
  FOREIGN KEY (`team_id`) REFERENCES `auth_teams`         (`id`) ON DELETE CASCADE
);
CREATE UNIQUE INDEX `idx_auth_api_keys_key_hash`  ON `auth_api_keys` (`key_hash`);
CREATE INDEX        `idx_auth_api_keys_expires_at` ON `auth_api_keys` (`expires_at`);
