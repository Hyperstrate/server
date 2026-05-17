-- Prompts: prompt templates and immutable version history
CREATE TABLE `prompts` (
  `id`          text NOT NULL,
  `org_id`      text NOT NULL,
  `name`        text NOT NULL,
  `description` text,
  `content`     text NOT NULL,
  `variables`   text,
  `created_at`  datetime,
  `modified_at` datetime,
  PRIMARY KEY (`id`),
  FOREIGN KEY (`org_id`) REFERENCES `auth_organizations` (`id`) ON DELETE CASCADE
);
CREATE INDEX `idx_prompts_org_id` ON `prompts` (`org_id`);

CREATE TABLE `prompt_versions` (
  `id`         text    NOT NULL,
  `prompt_id`  text    NOT NULL,
  `org_id`     text    NOT NULL,
  `version`    integer NOT NULL DEFAULT 1,
  `name`       text    NOT NULL,
  `content`    text    NOT NULL,
  `variables`  text,
  `created_at` datetime,
  PRIMARY KEY (`id`),
  FOREIGN KEY (`prompt_id`) REFERENCES `prompts`            (`id`) ON DELETE CASCADE,
  FOREIGN KEY (`org_id`)    REFERENCES `auth_organizations` (`id`) ON DELETE CASCADE
);
CREATE INDEX `idx_prompt_versions_prompt_id` ON `prompt_versions` (`prompt_id`);
