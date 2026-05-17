-- Routers: routers, configurations, targets, features, interceptors, team access, evaluations
CREATE TABLE `routers` (
  `id`                text    NOT NULL,
  `org_id`            text    NOT NULL,
  `name`              text    NOT NULL,
  `description`       text,
  `status`            text    NOT NULL DEFAULT 'draft',
  `strategy`          text    NOT NULL DEFAULT 'round_robin',
  `round_robin_index` integer NOT NULL DEFAULT 0,
  `created_at`        datetime,
  `modified_at`       datetime,
  PRIMARY KEY (`id`),
  FOREIGN KEY (`org_id`) REFERENCES `auth_organizations` (`id`) ON DELETE CASCADE
);
CREATE INDEX `idx_routers_org_id` ON `routers` (`org_id`);

CREATE TABLE `router_configurations` (
  `router_id`      text    NOT NULL,
  `webhook_url`    text    NOT NULL DEFAULT '',
  `prompt_id`      text,
  `store_payloads` integer NOT NULL DEFAULT 0,
  `modified_at`    datetime,
  PRIMARY KEY (`router_id`),
  FOREIGN KEY (`router_id`) REFERENCES `routers`  (`id`) ON DELETE CASCADE,
  FOREIGN KEY (`prompt_id`) REFERENCES `prompts`  (`id`) ON DELETE SET NULL
);

CREATE TABLE `router_targets` (
  `id`          text    NOT NULL,
  `router_id`   text    NOT NULL,
  `model_id`    text    NOT NULL,
  `weight`      integer NOT NULL DEFAULT 1,
  `percentage`  real    NOT NULL DEFAULT 0,
  `priority`    integer NOT NULL DEFAULT 0,
  `is_enabled`  integer NOT NULL DEFAULT 1,
  `prompt_id`   text,
  `created_at`  datetime,
  `modified_at` datetime,
  PRIMARY KEY (`id`),
  FOREIGN KEY (`router_id`) REFERENCES `routers` (`id`) ON DELETE CASCADE,
  FOREIGN KEY (`model_id`)  REFERENCES `models`  (`id`) ON DELETE CASCADE,
  FOREIGN KEY (`prompt_id`) REFERENCES `prompts` (`id`) ON DELETE SET NULL
);
CREATE INDEX `idx_router_targets_router_id` ON `router_targets` (`router_id`);

CREATE TABLE `router_features` (
  `id`              text    NOT NULL,
  `router_id`       text    NOT NULL,
  `feature_type`    text    NOT NULL,
  `config`          text,
  `execution_order` integer NOT NULL DEFAULT 0,
  `is_enabled`      integer NOT NULL DEFAULT 1,
  `created_at`      datetime,
  `modified_at`     datetime,
  PRIMARY KEY (`id`),
  FOREIGN KEY (`router_id`) REFERENCES `routers` (`id`) ON DELETE CASCADE
);
CREATE INDEX `idx_router_features_router_id` ON `router_features` (`router_id`);

CREATE TABLE `router_interceptors` (
  `id`              text    NOT NULL,
  `router_id`       text    NOT NULL,
  `type`            text    NOT NULL,
  `config`          text,
  `execution_order` integer NOT NULL DEFAULT 0,
  `is_enabled`      integer NOT NULL DEFAULT 1,
  `created_at`      datetime,
  `modified_at`     datetime,
  PRIMARY KEY (`id`),
  FOREIGN KEY (`router_id`) REFERENCES `routers` (`id`) ON DELETE CASCADE
);
CREATE INDEX `idx_router_interceptors_router_id` ON `router_interceptors` (`router_id`);

CREATE TABLE `router_team_accesses` (
  `id`        text NOT NULL,
  `router_id` text NOT NULL,
  `team_id`   text NOT NULL,
  `org_id`    text NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE (`router_id`, `team_id`)
);
CREATE INDEX `idx_router_team_accesses_router_id` ON `router_team_accesses` (`router_id`);

CREATE TABLE `router_evaluations` (
  `id`          text     NOT NULL,
  `org_id`      text     NOT NULL,
  `router_id`   text     NOT NULL,
  `name`        text     NOT NULL,
  `description` text,
  `created_at`  datetime,
  `modified_at` datetime,
  PRIMARY KEY (`id`)
);
CREATE INDEX `idx_router_evaluations_org_id`    ON `router_evaluations` (`org_id`);
CREATE INDEX `idx_router_evaluations_router_id` ON `router_evaluations` (`router_id`);

CREATE TABLE `router_evaluation_cases` (
  `id`           text    NOT NULL,
  `eval_id`      text    NOT NULL,
  `fields`       text,
  `expected`     text,
  `score_method` text    NOT NULL DEFAULT 'contains',
  `description`  text,
  `created_at`   datetime,
  PRIMARY KEY (`id`)
);
CREATE INDEX `idx_router_evaluation_cases_eval_id` ON `router_evaluation_cases` (`eval_id`);

CREATE TABLE `router_evaluation_runs` (
  `id`           text    NOT NULL,
  `eval_id`      text    NOT NULL,
  `router_id`    text    NOT NULL,
  `total_cases`  integer NOT NULL DEFAULT 0,
  `passed_cases` integer NOT NULL DEFAULT 0,
  `avg_score`    real    NOT NULL DEFAULT 0,
  `results`      text,
  `created_at`   datetime,
  PRIMARY KEY (`id`)
);
CREATE INDEX `idx_router_evaluation_runs_eval_id` ON `router_evaluation_runs` (`eval_id`);
