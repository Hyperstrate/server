-- Auth: organizations, users, teams, OIDC group mappings
CREATE TABLE `auth_organizations` (
  `id`          text    NOT NULL,
  `name`        text    NOT NULL,
  `slug`        text    NOT NULL,
  `is_enabled`  integer NOT NULL DEFAULT 1,
  `created_at`  datetime,
  `modified_at` datetime,
  PRIMARY KEY (`id`)
);

CREATE TABLE `auth_users` (
  `id`            text    NOT NULL,
  `org_id`        text    NOT NULL DEFAULT '',
  `email`         text    NOT NULL,
  `name`          text    NOT NULL DEFAULT '',
  `avatar`        text    NOT NULL DEFAULT '',
  `role`          text    NOT NULL DEFAULT 'member',
  `last_login_at` datetime,
  `created_at`    datetime,
  `modified_at`   datetime,
  PRIMARY KEY (`id`)
);
CREATE UNIQUE INDEX `idx_auth_users_email` ON `auth_users` (`email`);

CREATE TABLE `auth_teams` (
  `id`           text    NOT NULL,
  `org_id`       text    NOT NULL,
  `name`         text    NOT NULL,
  `description`  text,
  `max_requests` integer NOT NULL DEFAULT 0,
  `max_cost_usd` real    NOT NULL DEFAULT 0,
  `is_enabled`   integer NOT NULL DEFAULT 1,
  `created_at`   datetime,
  `modified_at`  datetime,
  PRIMARY KEY (`id`),
  FOREIGN KEY (`org_id`) REFERENCES `auth_organizations` (`id`) ON DELETE CASCADE
);

CREATE TABLE `auth_user_teams` (
  `user_id`    text NOT NULL,
  `team_id`    text NOT NULL,
  `created_at` datetime,
  PRIMARY KEY (`user_id`, `team_id`),
  FOREIGN KEY (`user_id`) REFERENCES `auth_users` (`id`) ON DELETE CASCADE,
  FOREIGN KEY (`team_id`) REFERENCES `auth_teams` (`id`) ON DELETE CASCADE
);
CREATE INDEX `idx_auth_user_teams_team_id` ON `auth_user_teams` (`team_id`);

CREATE TABLE `auth_oidc_group_mappings` (
  `id`         text NOT NULL,
  `org_id`     text NOT NULL,
  `group_name` text NOT NULL,
  `team_id`    text NOT NULL,
  `created_at` datetime,
  PRIMARY KEY (`id`),
  FOREIGN KEY (`org_id`) REFERENCES `auth_organizations` (`id`) ON DELETE CASCADE
);
CREATE INDEX `idx_auth_oidc_group_mappings_org_id` ON `auth_oidc_group_mappings` (`org_id`);
