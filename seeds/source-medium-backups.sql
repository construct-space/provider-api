-- Source-medium backup routes (2026-07-09).
--
-- The tinker upstream's fine-tune checkpoint
-- (source-medium-v6-2-medium) no longer exists on Tinker's cloud, so
-- the source-medium primary fails every call ("SamplingClient is
-- poisoned"). Until the checkpoint is re-published, give the lane real
-- backups so the Construct default keeps answering: Fireworks
-- gpt-oss-120b first (bigger credit budget), Together Qwen3.5-122B
-- second (same base family as the Construct fine-tunes). The tinker
-- primary stays in place — when the checkpoint revives, it takes over
-- again with no further change.
--
-- Model ids verified against both accounts' live /v1/models 2026-07-08.
-- Re-runnable; same upsert conventions as construct-free-models.sql.

BEGIN;

INSERT INTO construct_routing_targets (
  id, upstream_provider_id, upstream_model, label, description, icon,
  credits_per_prompt, max_tool_calls_per_credit, max_output_tokens_per_credit,
  thinking_mode, capabilities, enabled, sort_order, created_at, updated_at
) VALUES
  ('fireworks-gpt-oss-120b', 'fireworks', 'accounts/fireworks/models/gpt-oss-120b',
   'GPT-OSS 120B (Fireworks)', '', 'openai',
   1, 20, 4096, '', '["tools","reasoning"]', true, 140, NOW(), NOW()),

  ('together-qwen3.5-122b',  'together',  'Qwen/Qwen3.5-122B-A10B-FP8',
   'Qwen3.5 122B (Together)', '', 'togetherai',
   1, 20, 4096, '', '["tools","reasoning"]', true, 150, NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

INSERT INTO source_family_routes (operator_id, slot, position, model_id, created_at, updated_at)
VALUES
  ('source-medium', 'backup', 0, 'fireworks-gpt-oss-120b', NOW(), NOW()),
  ('source-medium', 'backup', 1, 'together-qwen3.5-122b',  NOW(), NOW())
ON CONFLICT (operator_id, slot, "position") DO UPDATE SET
  model_id   = EXCLUDED.model_id,
  updated_at = NOW();

-- Bump catalog version so version-keyed clients refetch.
UPDATE provider_catalog_version
   SET version    = substr(md5(random()::text), 1, 16),
       updated_at = NOW()
 WHERE id = 1;

COMMIT;
