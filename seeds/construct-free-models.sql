-- Construct free models: Code (GLM 5.2) + DeepSeek V4 Pro on the Construct
-- picker, served from platform credits (Fireworks primary, Together
-- backup — both upstreams already hold keys via oracle-web).
--
-- Chain: picker entry --route_via_operator--> source_family_routes
-- (primary → backups) --model_id--> construct_routing_targets
-- (upstream + upstream model + credit cost) --> upstream provider.
-- The chat dispatcher reads operator ids straight from the DB, so new
-- entries need zero code changes.
--
-- Model ids verified against both accounts' live /v1/models 2026-07-08:
--   fireworks: accounts/fireworks/models/glm-5p2, .../deepseek-v4-pro
--   together:  zai-org/GLM-5.2, deepseek-ai/DeepSeek-V4-Pro
--
-- Credit costs start at source-medium parity (1/prompt, default caps);
-- staff tune in oracle-web.

BEGIN;

-- Rename cleanup: an earlier revision of this seed created the GLM lane
-- as picker id 'glm-5.2' / operator 'glm-5.2' (renamed to 'code' on
-- 2026-07-08). Drop the old-named rows so a DB seeded pre-rename doesn't
-- end up with two GLM entries; the inserts below recreate the 'code'
-- rows. No-op on fresh or already-renamed databases.
DELETE FROM construct_picker_entries WHERE id = 'glm-5.2';
DELETE FROM source_family_routes WHERE operator_id = 'glm-5.2';

-- ── Routing targets ────────────────────────────────────────────────────
INSERT INTO construct_routing_targets (
  id, upstream_provider_id, upstream_model, label, description, icon,
  credits_per_prompt, max_tool_calls_per_credit, max_output_tokens_per_credit,
  thinking_mode, capabilities, enabled, sort_order, created_at, updated_at
) VALUES
  ('fireworks-glm-5p2',         'fireworks', 'accounts/fireworks/models/glm-5p2',
   'GLM 5.2 (Fireworks)',       '', 'zai',
   1, 20, 4096, '', '["tools","reasoning"]', true, 100, NOW(), NOW()),

  ('together-glm-5p2',          'together',  'zai-org/GLM-5.2',
   'GLM 5.2 (Together)',        '', 'zai',
   1, 20, 4096, '', '["tools","reasoning"]', true, 110, NOW(), NOW()),

  ('fireworks-deepseek-v4-pro', 'fireworks', 'accounts/fireworks/models/deepseek-v4-pro',
   'DeepSeek V4 Pro (Fireworks)', '', 'deepseek',
   1, 20, 4096, '', '["tools","reasoning"]', true, 120, NOW(), NOW()),

  ('together-deepseek-v4-pro',  'together',  'deepseek-ai/DeepSeek-V4-Pro',
   'DeepSeek V4 Pro (Together)', '', 'deepseek',
   1, 20, 4096, '', '["tools","reasoning"]', true, 130, NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

-- ── Operator chains: Fireworks primary, Together backup ────────────────
-- Upsert on the (operator, slot, position) unique index so a stale row
-- from a partial earlier apply gets re-pointed at the right target
-- instead of silently kept.
INSERT INTO source_family_routes (operator_id, slot, position, model_id, created_at, updated_at)
VALUES
  ('code',            'primary', 0, 'fireworks-glm-5p2',         NOW(), NOW()),
  ('code',            'backup',  0, 'together-glm-5p2',          NOW(), NOW()),
  ('deepseek-v4-pro', 'primary', 0, 'fireworks-deepseek-v4-pro', NOW(), NOW()),
  ('deepseek-v4-pro', 'backup',  0, 'together-deepseek-v4-pro',  NOW(), NOW())
ON CONFLICT (operator_id, slot, "position") DO UPDATE SET
  model_id   = EXCLUDED.model_id,
  updated_at = NOW();

-- ── Picker entries (what users see under the Construct provider) ───────
INSERT INTO construct_picker_entries (
  id, label, description, icon, capabilities,
  enabled, sort_order, route_via_operator,
  route_via_operator_large, route_via_operator_small,
  created_at, updated_at
) VALUES
  ('code', 'Code',
   'Coding-tuned lane — tools + reasoning, on Construct credits.',
   'zai', '["tools","reasoning"]',
   true, 50, 'code', '', '', NOW(), NOW()),

  ('deepseek-v4-pro', 'DeepSeek V4 Pro',
   'DeepSeek flagship — tools + reasoning, on Construct credits.',
   'deepseek', '["tools","reasoning"]',
   true, 60, 'deepseek-v4-pro', '', '', NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

-- Bump catalog version so version-keyed clients refetch (oracle-web's
-- picker CRUD bumps on every save for the same reason). md5(random())
-- — the providers DB has no pgcrypto.
UPDATE provider_catalog_version
   SET version    = substr(md5(random()::text), 1, 16),
       updated_at = NOW()
 WHERE id = 1;

COMMIT;

-- Verification (after COMMIT):
--   SELECT id, label, route_via_operator FROM construct_picker_entries ORDER BY sort_order;
--   SELECT operator_id, slot, position, model_id FROM source_family_routes ORDER BY operator_id, slot, position;
