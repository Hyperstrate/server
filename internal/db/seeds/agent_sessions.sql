-- Seed rich coding-agent sessions for the Analytics → Agents dashboard.
-- Idempotent: re-running clears rows identified by id/log_id/session id LIKE 'seed-agent-%'.

DELETE FROM tool_call_archives WHERE id LIKE 'seed-agent-%' OR log_id LIKE 'seed-agent-log-%' OR agent_session_id LIKE 'seed-agent-%';
DELETE FROM compression_events WHERE id LIKE 'seed-agent-%' OR log_id LIKE 'seed-agent-log-%' OR agent_session_id LIKE 'seed-agent-%';
DELETE FROM agent_session_events WHERE id LIKE 'seed-agent-%' OR agent_session_id LIKE 'seed-agent-%';
DELETE FROM inference_payloads WHERE log_id LIKE 'seed-agent-log-%';
DELETE FROM inference_logs WHERE id LIKE 'seed-agent-log-%';

INSERT INTO inference_logs (
  id, org_id, router_id, virtual_key_id, team_id, user_id,
  model_id, model_def_key, provider,
  input_tokens, output_tokens, cached_input_tokens, total_tokens, cost_usd,
  latency_ms, ttft_ms, status, error_message, source, selected_target_id, ab_variant,
  pipeline_trace, agent_session_id, agent, agent_role, parent_session_id, turn_index,
  tool_call_count, tool_result_chars, quality_score, context_fill_pct,
  loop_detected, loop_reason, cache_hit, cache_hit_type, feedback, created_at
)
WITH
  ctx AS (
    SELECT
      (SELECT id FROM auth_organizations ORDER BY created_at DESC LIMIT 1) AS org_id,
      COALESCE((SELECT id FROM routers ORDER BY created_at DESC LIMIT 1), 'seed-router') AS router_id,
      COALESCE((SELECT id FROM auth_virtual_keys ORDER BY created_at DESC LIMIT 1), '') AS virtual_key_id,
      COALESCE((SELECT id FROM auth_teams ORDER BY created_at DESC LIMIT 1), '') AS team_id,
      COALESCE((SELECT id FROM models WHERE model_definition_key LIKE '%gpt-4o%' ORDER BY created_at DESC LIMIT 1), (SELECT id FROM models ORDER BY created_at DESC LIMIT 1), 'seed-model-gpt') AS gpt_model_id,
      COALESCE((SELECT model_definition_key FROM models WHERE model_definition_key LIKE '%gpt-4o%' ORDER BY created_at DESC LIMIT 1), 'openai/gpt-4o') AS gpt_model_def,
      COALESCE((SELECT id FROM models WHERE model_definition_key LIKE '%claude%' ORDER BY created_at DESC LIMIT 1), (SELECT id FROM models ORDER BY created_at DESC LIMIT 1), 'seed-model-claude') AS claude_model_id,
      COALESCE((SELECT model_definition_key FROM models WHERE model_definition_key LIKE '%claude%' ORDER BY created_at DESC LIMIT 1), 'anthropic/claude-3-5-sonnet-20241022') AS claude_model_def
  ),
  rows(id, session_id, client, role, parent_id, turn_index, user_id, model_family, input_tokens, output_tokens, cached_tokens, cost_usd, latency_ms, ttft_ms, status, err, tools, tool_chars, quality, ctx_fill, looped, loop_reason, cache_hit, cache_type, feedback, minutes_ago, ab_variant) AS (
    VALUES
      ('seed-agent-log-codex-001','seed-agent-codex-refactor','codex','main','',1,'eduard','gpt',6200,840,1800,0.041200,4100,620,'success','',2,18400,91,38.4,0,'',0,'',1,170,'optimizer-a'),
      ('seed-agent-log-codex-002','seed-agent-codex-refactor','codex','main','',2,'eduard','gpt',9800,1120,3600,0.063500,5300,710,'success','',3,42800,84,52.0,0,'',0,'',0,160,'optimizer-a'),
      ('seed-agent-log-codex-003','seed-agent-codex-refactor','codex','main','',3,'eduard','gpt',4200,690,2100,0.025900,2800,410,'success','',1,9700,93,24.4,0,'',1,'semantic',1,150,'optimizer-a'),
      ('seed-agent-log-codex-004','seed-agent-codex-refactor','codex','worker','seed-agent-codex-refactor',4,'eduard','gpt',14200,740,0,0.074700,7300,990,'success','',4,61200,72,74.7,0,'',0,'',0,138,'optimizer-b'),
      ('seed-agent-log-codex-005','seed-agent-codex-refactor','codex','worker','seed-agent-codex-refactor',5,'eduard','gpt',14750,720,0,0.076800,7600,1010,'error','tool fs.read failed: path not found after retry',4,60100,48,77.3,1,'repeated tool-call failure',0,'',-1,126,'optimizer-b'),
      ('seed-agent-log-codex-006','seed-agent-codex-refactor','codex','worker','seed-agent-codex-refactor',6,'eduard','gpt',15100,700,0,0.078900,7800,1050,'error','tool fs.read failed: path not found after retry',4,60900,44,79.0,1,'repeated error: tool fs.read failed: path not found after retry',0,'',-1,114,'optimizer-b'),
      ('seed-agent-log-codex-007','seed-agent-codex-refactor','codex','reviewer','seed-agent-codex-refactor',7,'eduard','gpt',6400,530,2200,0.034600,3600,540,'success','',1,5200,88,34.6,0,'',1,'exact',0,102,'optimizer-a'),
      ('seed-agent-log-codex-008','seed-agent-codex-refactor','codex','main','',8,'eduard','gpt',5900,610,1800,0.032500,3400,500,'success','',0,0,94,32.5,0,'',0,'',1,90,'optimizer-a'),

      ('seed-agent-log-claude-001','seed-agent-claude-debug','claude_code','main','',1,'frontend-dev','claude',8800,970,2600,0.048900,5200,760,'success','',2,21800,86,48.8,0,'',0,'',0,80,''),
      ('seed-agent-log-claude-002','seed-agent-claude-debug','claude_code','main','',2,'frontend-dev','claude',18300,1210,0,0.097500,9100,1210,'success','',5,78200,67,97.6,0,'',0,'',0,70,''),
      ('seed-agent-log-claude-003','seed-agent-claude-debug','claude_code','main','',3,'frontend-dev','claude',18550,1180,0,0.098600,9300,1230,'error','provider timeout while summarizing oversized tool response',5,80100,39,98.7,1,'token spike versus recent turns',0,'',-1,60,''),
      ('seed-agent-log-claude-004','seed-agent-claude-debug','claude_code','main','',4,'frontend-dev','claude',7600,890,3100,0.042400,4300,640,'success','',2,16400,82,42.4,0,'',1,'semantic',0,50,''),
      ('seed-agent-log-claude-005','seed-agent-claude-debug','claude_code','subagent','seed-agent-claude-debug',5,'frontend-dev','claude',9400,660,1200,0.050300,4900,690,'success','',3,27800,77,50.3,0,'',0,'',0,40,''),
      ('seed-agent-log-claude-006','seed-agent-claude-debug','claude_code','main','',6,'frontend-dev','claude',5200,540,1800,0.028700,3100,430,'success','',1,8200,90,28.7,0,'',0,'',1,30,''),

      ('seed-agent-log-cursor-001','seed-agent-cursor-docs','cursor','main','',1,'docs-owner','gpt',3600,430,900,0.020100,1900,260,'success','',0,0,96,20.2,0,'',1,'exact',1,55,'docs'),
      ('seed-agent-log-cursor-002','seed-agent-cursor-docs','cursor','main','',2,'docs-owner','gpt',4100,480,1200,0.022900,2200,300,'success','',1,3100,95,22.9,0,'',1,'semantic',1,45,'docs'),
      ('seed-agent-log-cursor-003','seed-agent-cursor-docs','cursor','main','',3,'docs-owner','gpt',3900,520,1500,0.022100,2100,280,'success','',0,0,97,22.1,0,'',1,'exact',1,35,'docs'),

      ('seed-agent-log-windsurf-001','seed-agent-windsurf-ui-polish','windsurf','main','',1,'designer-dev','gpt',5400,620,1800,0.030100,2600,390,'success','',3,11200,92,30.1,0,'',1,'semantic',1,28,'ui'),
      ('seed-agent-log-windsurf-002','seed-agent-windsurf-ui-polish','windsurf','worker','seed-agent-windsurf-ui-polish',2,'designer-dev','gpt',7900,760,2400,0.043300,3400,520,'success','',4,24800,87,43.3,0,'',0,'',0,24,'ui'),
      ('seed-agent-log-windsurf-003','seed-agent-windsurf-ui-polish','windsurf','reviewer','seed-agent-windsurf-ui-polish',3,'designer-dev','gpt',4600,390,1600,0.024900,2100,310,'success','',2,6600,95,24.9,0,'',1,'exact',1,20,'ui'),

      ('seed-agent-log-openclaw-001','seed-agent-openclaw-terminal-loop','openclaw','main','',1,'platform-eng','claude',11200,930,0,0.060600,5100,780,'success','',5,39100,78,60.7,0,'',0,'',0,26,'terminal'),
      ('seed-agent-log-openclaw-002','seed-agent-openclaw-terminal-loop','openclaw','main','',2,'platform-eng','claude',11800,890,0,0.063500,5400,800,'error','shell command exited 127: missing binary after retry',6,45200,43,63.4,1,'repeated terminal command failure',0,'',-1,18,'terminal'),
      ('seed-agent-log-openclaw-003','seed-agent-openclaw-terminal-loop','openclaw','main','',3,'platform-eng','claude',6200,580,1900,0.033900,2900,430,'success','',3,12900,84,33.9,0,'',1,'semantic',0,12,'terminal'),

      ('seed-agent-log-aider-001','seed-agent-aider-migration','aider','main','',1,'backend-dev','gpt',7200,810,900,0.040100,3100,450,'success','',4,18300,89,40.1,0,'',0,'',1,42,'migration'),
      ('seed-agent-log-aider-002','seed-agent-aider-migration','aider','main','',2,'backend-dev','gpt',13200,920,0,0.070600,6200,870,'success','',6,53700,74,70.6,0,'',0,'',0,34,'migration'),
      ('seed-agent-log-aider-003','seed-agent-aider-migration','aider','reviewer','seed-agent-aider-migration',3,'backend-dev','gpt',5100,420,1700,0.027600,2500,360,'success','',2,5900,93,27.6,0,'',1,'exact',1,22,'migration'),

      ('seed-agent-log-continue-001','seed-agent-continue-java','continue','main','',1,'jvm-dev','claude',6800,640,2000,0.037200,2800,410,'success','',3,14600,90,37.2,0,'',1,'semantic',1,38,'java'),
      ('seed-agent-log-continue-002','seed-agent-continue-java','continue','main','',2,'jvm-dev','claude',9100,710,2300,0.049100,3600,520,'success','',4,22600,86,49.1,0,'',0,'',0,27,'java'),

      ('seed-agent-log-copilot-001','seed-agent-copilot-test-gen','github_copilot','main','',1,'qa-dev','gpt',3100,350,1100,0.017200,1500,220,'success','',1,2800,98,17.3,0,'',1,'exact',1,33,'tests'),
      ('seed-agent-log-copilot-002','seed-agent-copilot-test-gen','github_copilot','main','',2,'qa-dev','gpt',4500,510,1500,0.025000,2100,290,'success','',2,7200,96,25.0,0,'',1,'semantic',1,25,'tests'),

      ('seed-agent-log-jetbrains-001','seed-agent-jetbrains-kotlin','jetbrains_ai','main','',1,'kotlin-dev','claude',5700,610,1300,0.031500,2400,340,'success','',3,10200,91,31.6,0,'',0,'',0,31,'kotlin'),
      ('seed-agent-log-zed-001','seed-agent-zed-rust','zed_ai','main','',1,'rust-dev','gpt',6100,540,2100,0.033200,2300,330,'success','',4,17400,94,33.2,0,'',1,'semantic',1,29,'rust'),
      ('seed-agent-log-trae-001','seed-agent-trae-mobile','trae','main','',1,'mobile-dev','gpt',7300,690,1200,0.039900,3200,470,'success','',3,11800,88,39.9,0,'',0,'',0,23,'mobile')
  )
SELECT
  rows.id,
  ctx.org_id,
  ctx.router_id,
  ctx.virtual_key_id,
  ctx.team_id,
  rows.user_id,
  CASE rows.model_family WHEN 'claude' THEN ctx.claude_model_id ELSE ctx.gpt_model_id END,
  CASE rows.model_family WHEN 'claude' THEN ctx.claude_model_def ELSE ctx.gpt_model_def END,
  CASE rows.model_family WHEN 'claude' THEN 'anthropic' ELSE 'openai' END,
  rows.input_tokens,
  rows.output_tokens,
  rows.cached_tokens,
  rows.input_tokens + rows.output_tokens,
  rows.cost_usd,
  rows.latency_ms,
  rows.ttft_ms,
  rows.status,
  NULLIF(rows.err, ''),
  'router',
  'seed-target-primary',
  rows.ab_variant,
  json_array(
    json_object('phase', 1, 'kind', 'rate_limit', 'name', 'Rate Limit', 'outcome', 'passed', 'durationMs', 0.4, 'offsetMs', 0),
    json_object('phase', 2, 'kind', 'budget', 'name', 'Budget Check', 'outcome', 'passed', 'durationMs', 0.7, 'offsetMs', 1),
    json_object('phase', 3, 'kind', 'cache', 'name', 'Response Cache', 'outcome', CASE WHEN rows.cache_hit = 1 THEN 'hit_semantic' ELSE 'miss' END, 'detail', CASE WHEN rows.cache_hit = 1 THEN rows.cache_type || ' hit' ELSE 'fingerprint miss' END, 'durationMs', 1.1, 'offsetMs', 2),
    json_object('phase', 4, 'kind', 'transform', 'name', 'Prompt Optimizer', 'outcome', CASE WHEN rows.id IN ('seed-agent-log-codex-002','seed-agent-log-codex-004','seed-agent-log-claude-002','seed-agent-log-claude-003','seed-agent-log-openclaw-001','seed-agent-log-aider-002','seed-agent-log-continue-002') THEN 'applied' ELSE 'skipped' END, 'detail', CASE WHEN rows.id IN ('seed-agent-log-codex-002','seed-agent-log-codex-004','seed-agent-log-claude-002','seed-agent-log-claude-003','seed-agent-log-openclaw-001','seed-agent-log-aider-002','seed-agent-log-continue-002') THEN 'rewrite · ' || (rows.input_tokens * 4) || ' to ' || ((rows.input_tokens * 4) - 3600) || ' chars' ELSE 'rules did not match' END, 'durationMs', 4.2, 'offsetMs', 5),
    json_object('phase', 6, 'kind', 'inference', 'name', 'Model Inference', 'outcome', CASE WHEN rows.status = 'error' THEN 'error' ELSE 'success' END, 'detail', (CASE rows.model_family WHEN 'claude' THEN ctx.claude_model_def ELSE ctx.gpt_model_def END) || ' · ' || rows.input_tokens || '↑ ' || rows.output_tokens || '↓ tok · $' || printf('%.5f', rows.cost_usd), 'durationMs', rows.latency_ms, 'offsetMs', 10),
    json_object('phase', 6, 'kind', 'mcp_tools', 'name', 'MCP Tool Execution', 'outcome', CASE WHEN rows.tools > 0 AND rows.status = 'error' THEN 'error' WHEN rows.tools > 0 THEN 'success' ELSE 'skipped' END, 'detail', rows.tools || ' tool call(s) · response_chars=' || rows.tool_chars, 'durationMs', 19.3, 'offsetMs', 24)
  ),
  rows.session_id,
  rows.client,
  rows.role,
  rows.parent_id,
  rows.turn_index,
  rows.tools,
  rows.tool_chars,
  rows.quality,
  rows.ctx_fill,
  rows.looped,
  NULLIF(rows.loop_reason, ''),
  rows.cache_hit,
  rows.cache_type,
  rows.feedback,
  datetime('now', '-' || rows.minutes_ago || ' minutes')
FROM rows, ctx
WHERE ctx.org_id IS NOT NULL;

INSERT INTO inference_payloads (log_id, router_id, request_fields, response_content, created_at)
SELECT
  id,
  router_id,
  json_object(
    'systemPrompt', 'You are operating as an autonomous coding agent. Keep output concise and preserve user changes.',
    'prompt', CASE agent_session_id
      WHEN 'seed-agent-codex-refactor' THEN 'Refactor the router pipeline observability layer and add session tracking.'
      WHEN 'seed-agent-claude-debug' THEN 'Debug why the frontend agent dashboard cannot inspect tool output.'
      WHEN 'seed-agent-windsurf-ui-polish' THEN 'Polish the analytics UI, fix responsive layout, and verify visual hierarchy.'
      WHEN 'seed-agent-openclaw-terminal-loop' THEN 'Diagnose failing shell commands in the agent terminal workflow.'
      WHEN 'seed-agent-aider-migration' THEN 'Generate a safe migration and update repository wiring.'
      WHEN 'seed-agent-continue-java' THEN 'Refactor the Java service layer and keep tests green.'
      WHEN 'seed-agent-copilot-test-gen' THEN 'Generate missing unit tests for cache and session behavior.'
      WHEN 'seed-agent-jetbrains-kotlin' THEN 'Inspect Kotlin handlers and suggest repository cleanup.'
      WHEN 'seed-agent-zed-rust' THEN 'Optimize a Rust parser loop and check cargo output.'
      WHEN 'seed-agent-trae-mobile' THEN 'Update the mobile route guard and validate navigation edge cases.'
      ELSE 'Refresh documentation and summarize cost-saving changes.'
    END,
    'agentSessionId', agent_session_id,
    'turnIndex', turn_index
  ),
  CASE status
    WHEN 'error' THEN error_message
    ELSE 'Implemented turn ' || turn_index || ' for ' || agent_session_id || '. Captured tokens, cost, pipeline trace, and follow-up notes.'
  END,
  created_at
FROM inference_logs
WHERE id LIKE 'seed-agent-log-%';

INSERT INTO tool_call_archives (
  id, org_id, router_id, log_id, agent_session_id, tool_name, tool_call_id,
  request_preview, request_payload, response_preview, response_payload,
  response_chars, error_message, archived, created_at
)
WITH tool_logs AS (
  SELECT * FROM inference_logs WHERE id LIKE 'seed-agent-log-%' AND tool_call_count > 0
),
tool_idx(n, tool_name) AS (
  VALUES
    (1, 'fs.read'),
    (2, 'rg.search'),
    (3, 'go.test'),
    (4, 'npm.build'),
    (5, 'git.diff'),
    (6, 'terminal.exec')
)
SELECT
  'seed-agent-tool-' || substr(log.id, length('seed-agent-log-') + 1) || '-' || tool_idx.n,
  log.org_id,
  log.router_id,
  log.id,
  log.agent_session_id,
  tool_idx.tool_name,
  'call_' || substr(log.id, length(log.id) - 2) || '_' || tool_idx.n,
  json_object('path', CASE tool_idx.tool_name WHEN 'rg.search' THEN 'internal/modules' ELSE 'pipeline.go' END, 'query', 'agent session tracking'),
  json_object('tool', tool_idx.tool_name, 'args', json_object('path', CASE tool_idx.tool_name WHEN 'rg.search' THEN 'internal/modules' WHEN 'terminal.exec' THEN './scripts/check.sh' ELSE 'pipeline.go' END, 'query', 'agent session tracking', 'limit', 50)),
  CASE
    WHEN log.status = 'error' AND tool_idx.n = log.tool_call_count THEN 'failed after retry: path not found'
    WHEN tool_idx.tool_name = 'go.test' THEN 'ok hyperstrate/server/internal/modules/router/application'
    WHEN tool_idx.tool_name = 'npm.build' THEN 'built in 7.7s with existing chunk warnings'
    WHEN tool_idx.tool_name = 'terminal.exec' THEN 'exit status captured with stdout/stderr preview'
    ELSE 'matched files and returned summarized output for ' || log.agent_session_id
  END,
  json_object(
    'tool', tool_idx.tool_name,
    'summary', CASE
      WHEN log.status = 'error' AND tool_idx.n = log.tool_call_count THEN 'failed after retry: path not found'
      ELSE 'returned a large result; preview kept in the trace and full payload archived here'
    END,
    'lines', json_array('pipeline.go:619 executeMCPTools', 'service.go:757 RouterInferenceLoggedEvent', 'handler.go:64 X-Agent', 'terminal: command trace available')
  ),
  CAST(log.tool_result_chars / log.tool_call_count AS integer),
  CASE WHEN log.status = 'error' AND tool_idx.n = log.tool_call_count THEN COALESCE(log.error_message, 'tool failed') ELSE NULL END,
  CASE WHEN log.tool_result_chars > 12000 THEN 1 ELSE 0 END,
  log.created_at
FROM tool_logs log
JOIN tool_idx ON tool_idx.n <= log.tool_call_count;

INSERT INTO compression_events (
  id, org_id, router_id, log_id, agent_session_id, feature_name,
  before_chars, after_chars, saved_chars, estimated_tokens_saved,
  exact, quality_score, detail, created_at
)
SELECT
  'seed-agent-compress-' || substr(id, length('seed-agent-log-') + 1),
  org_id,
  router_id,
  id,
  agent_session_id,
  CASE WHEN input_tokens > 12000 THEN 'Context Compression' ELSE 'Prompt Optimizer' END,
  input_tokens * 4,
  (input_tokens * 4) - CASE WHEN input_tokens > 12000 THEN 7200 ELSE 3600 END,
  CASE WHEN input_tokens > 12000 THEN 7200 ELSE 3600 END,
  CASE WHEN input_tokens > 12000 THEN 1800 ELSE 900 END,
  1,
  quality_score,
  CASE WHEN input_tokens > 12000 THEN 'context compression' ELSE 'rewrite' END || ' · ' || (input_tokens * 4) || ' to ' || ((input_tokens * 4) - CASE WHEN input_tokens > 12000 THEN 7200 ELSE 3600 END) || ' chars',
  created_at
FROM inference_logs
WHERE id IN (
  'seed-agent-log-codex-002',
  'seed-agent-log-codex-004',
  'seed-agent-log-claude-002',
  'seed-agent-log-claude-003',
  'seed-agent-log-openclaw-001',
  'seed-agent-log-aider-002',
  'seed-agent-log-continue-002'
);

INSERT INTO agent_session_events (
  id, org_id, router_id, virtual_key_id, team_id, user_id, agent_session_id, agent, event_type, detail, created_at
)
SELECT 'seed-agent-event-codex-start', org_id, router_id, virtual_key_id, team_id, user_id, agent_session_id, agent, 'checkpoint_created', 'Baseline before router pipeline refactor', datetime(created_at, '-5 minutes')
FROM inference_logs WHERE id = 'seed-agent-log-codex-001'
UNION ALL
SELECT 'seed-agent-event-codex-compact', org_id, router_id, virtual_key_id, team_id, user_id, agent_session_id, agent, 'compaction_completed', 'Compacted after MCP tool output grew beyond preview threshold', datetime(created_at, '+35 minutes')
FROM inference_logs WHERE id = 'seed-agent-log-codex-004'
UNION ALL
SELECT 'seed-agent-event-codex-end', org_id, router_id, virtual_key_id, team_id, user_id, agent_session_id, agent, 'session_end', 'Final dashboard verification completed', datetime(created_at, '+3 minutes')
FROM inference_logs WHERE id = 'seed-agent-log-codex-008'
UNION ALL
SELECT 'seed-agent-event-claude-compact-start', org_id, router_id, virtual_key_id, team_id, user_id, agent_session_id, agent, 'compaction_started', 'Context exceeded healthy range while reading large tool results', datetime(created_at, '+2 minutes')
FROM inference_logs WHERE id = 'seed-agent-log-claude-002'
UNION ALL
SELECT 'seed-agent-event-claude-checkpoint', org_id, router_id, virtual_key_id, team_id, user_id, agent_session_id, agent, 'checkpoint_created', 'Recovered after loop detection and resumed with narrower context', datetime(created_at, '+2 minutes')
FROM inference_logs WHERE id = 'seed-agent-log-claude-004'
UNION ALL
SELECT 'seed-agent-event-cursor-checkpoint', org_id, router_id, virtual_key_id, team_id, user_id, agent_session_id, agent, 'checkpoint_created', 'Docs draft approved for publication', datetime(created_at, '+2 minutes')
FROM inference_logs WHERE id = 'seed-agent-log-cursor-003'
UNION ALL
SELECT 'seed-agent-event-windsurf-checkpoint', org_id, router_id, virtual_key_id, team_id, user_id, agent_session_id, agent, 'checkpoint_created', 'UI polish accepted after responsive scan', datetime(created_at, '+2 minutes')
FROM inference_logs WHERE id = 'seed-agent-log-windsurf-003'
UNION ALL
SELECT 'seed-agent-event-openclaw-clear', org_id, router_id, virtual_key_id, team_id, user_id, agent_session_id, agent, 'clear', 'Cleared terminal context after repeated missing-binary failure', datetime(created_at, '+2 minutes')
FROM inference_logs WHERE id = 'seed-agent-log-openclaw-002'
UNION ALL
SELECT 'seed-agent-event-aider-migration', org_id, router_id, virtual_key_id, team_id, user_id, agent_session_id, agent, 'checkpoint_created', 'Migration generated and atlas hash refreshed', datetime(created_at, '+2 minutes')
FROM inference_logs WHERE id = 'seed-agent-log-aider-002'
UNION ALL
SELECT 'seed-agent-event-continue-compact', org_id, router_id, virtual_key_id, team_id, user_id, agent_session_id, agent, 'compaction_completed', 'Java service context compacted before final test pass', datetime(created_at, '+2 minutes')
FROM inference_logs WHERE id = 'seed-agent-log-continue-002'
UNION ALL
SELECT 'seed-agent-event-copilot-end', org_id, router_id, virtual_key_id, team_id, user_id, agent_session_id, agent, 'session_end', 'Test generation completed with cache hits', datetime(created_at, '+2 minutes')
FROM inference_logs WHERE id = 'seed-agent-log-copilot-002'
UNION ALL
SELECT 'seed-agent-event-zed-rust-checkpoint', org_id, router_id, virtual_key_id, team_id, user_id, agent_session_id, agent, 'checkpoint_created', 'Rust parser benchmark noted for follow-up', datetime(created_at, '+2 minutes')
FROM inference_logs WHERE id = 'seed-agent-log-zed-001';
