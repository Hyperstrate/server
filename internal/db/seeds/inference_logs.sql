-- Seed inference_logs with 1,460 synthetic rows spread over the last 365 days (~4/day).
-- Idempotent: re-running clears previous seed rows identified by id LIKE 'seed-%'.
-- org_id is resolved dynamically from auth_organizations so no hardcoding is needed.

DELETE FROM inference_logs WHERE id LIKE 'seed-%';

INSERT INTO inference_logs (
  id, org_id, router_id, model_id, model_def_key, provider,
  input_tokens, output_tokens, total_tokens, cost_usd,
  latency_ms, status, error_message, source, selected_target_id, created_at
)
WITH RECURSIVE
  cnt(i) AS (SELECT 0 UNION ALL SELECT i + 1 FROM cnt WHERE i < 1459)
SELECT
  -- id
  'seed-' || printf('%04d', i),

  -- org_id: resolved from the first organization in the database
  (SELECT id FROM auth_organizations LIMIT 1),

  -- router_id: ~2/3 routed, 1/3 direct (aligned with source below)
  CASE i % 3
    WHEN 0 THEN 'rtr-prod'
    WHEN 1 THEN ''
    ELSE        'rtr-dev'
  END,

  -- model_id
  CASE i % 4
    WHEN 0 THEN 'mdl-gpt-4o'
    WHEN 1 THEN 'mdl-gpt-4o-mini'
    WHEN 2 THEN 'mdl-claude-35-sonnet'
    ELSE        'mdl-claude-3-haiku'
  END,

  -- model_def_key
  CASE i % 4
    WHEN 0 THEN 'openai/gpt-4o'
    WHEN 1 THEN 'openai/gpt-4o-mini'
    WHEN 2 THEN 'anthropic/claude-3-5-sonnet-20241022'
    ELSE        'anthropic/claude-3-haiku-20240307'
  END,

  -- provider
  CASE i % 4
    WHEN 0 THEN 'openai'
    WHEN 1 THEN 'openai'
    ELSE        'anthropic'
  END,

  -- input_tokens (200 – 4000)
  200 + (i * 37 + 11) % 3800,

  -- output_tokens (40 – 740)
  40 + (i * 13 + 7) % 700,

  -- total_tokens
  (200 + (i * 37 + 11) % 3800) + (40 + (i * 13 + 7) % 700),

  -- cost_usd based on model pricing (per token)
  ROUND(
    ((200 + (i * 37 + 11) % 3800) + (40 + (i * 13 + 7) % 700)) *
    CASE i % 4
      WHEN 0 THEN 0.005    -- gpt-4o:              $5 / 1M tokens
      WHEN 1 THEN 0.00015  -- gpt-4o-mini:         $0.15 / 1M tokens
      WHEN 2 THEN 0.003    -- claude-3-5-sonnet:   $3 / 1M tokens
      ELSE       0.00025   -- claude-3-haiku:      $0.25 / 1M tokens
    END / 1000.0,
    6
  ),

  -- latency_ms (150 – 4650 ms)
  150 + (i * 17 + 5) % 4500,

  -- status: ~5% errors (every 19th row)
  CASE WHEN i % 19 = 0 THEN 'error' ELSE 'success' END,

  -- error_message
  CASE WHEN i % 19 = 0
    THEN CASE i % 3
      WHEN 0 THEN 'upstream timeout: context deadline exceeded'
      WHEN 1 THEN 'rate limit exceeded: retry after 60s'
      ELSE        'provider error 500: internal server error'
    END
    ELSE NULL
  END,

  -- source (matches router_id above: i%3=1 → direct, else router)
  CASE i % 3
    WHEN 1 THEN 'direct'
    ELSE        'router'
  END,

  -- selected_target_id
  CASE i % 3
    WHEN 0 THEN 'tgt-prod-' || (i % 3)
    WHEN 1 THEN ''
    ELSE        'tgt-dev-'  || (i % 2)
  END,

  -- created_at: evenly spread over 365 days, varied hour/minute
  datetime(
    'now',
    '-' || (364 - (i * 364 / 1459)) || ' days',
    '+' || ((i * 6 + 3)  % 18 + 6)  || ' hours',
    '+' || ((i * 11 + 7) % 60)      || ' minutes'
  )

FROM cnt;
