-- One-off migration: split the muddled `construct_models` table into
--   - construct_picker_entries (user-facing chat rows)
--   - construct_routing_targets (internal aliases referenced by source-family)
--
-- The new provider-api binary AutoMigrates the two new tables on
-- startup; this script copies existing rows into them, idempotently.
-- After eyeballing the new Oracle pages, drop the legacy table.
--
-- Safe to run multiple times — all INSERTs use ON CONFLICT DO NOTHING.

BEGIN;

-- 1. Copy routed rows into construct_picker_entries.
INSERT INTO construct_picker_entries (
  id, label, description, icon, capabilities, enabled, sort_order, created_at, updated_at
)
SELECT
  id, label, description, icon, capabilities, enabled, sort_order, created_at, updated_at
FROM construct_models
WHERE kind = 'routed'
ON CONFLICT (id) DO NOTHING;

-- 2. Copy passthrough rows into construct_routing_targets.
INSERT INTO construct_routing_targets (
  id, upstream_provider_id, upstream_model, label, description, icon,
  credits_per_prompt, max_tool_calls_per_credit, max_output_tokens_per_credit,
  thinking_mode, capabilities, enabled, sort_order, created_at, updated_at
)
SELECT
  id, upstream_provider_id, upstream_model, label, description, icon,
  credits_per_prompt, max_tool_calls_per_credit, max_output_tokens_per_credit,
  thinking_mode, capabilities, enabled, sort_order, created_at, updated_at
FROM construct_models
WHERE kind = 'passthrough'
ON CONFLICT (id) DO NOTHING;

-- 3. Sanity: every source_family_routes.model_id must resolve to a
-- routing target.
DO $$
DECLARE
  missing_count int;
BEGIN
  SELECT COUNT(*) INTO missing_count
  FROM source_family_routes r
  LEFT JOIN construct_routing_targets t ON t.id = r.model_id
  WHERE t.id IS NULL;

  IF missing_count > 0 THEN
    RAISE EXCEPTION 'source_family_routes references % missing routing targets', missing_count;
  END IF;
END $$;

COMMIT;

-- 4. After the new Oracle pages look right, drop the legacy table.
-- Uncomment when ready — keep this commented for the first deploy so
-- you can verify before destroying data.
--
-- DROP TABLE construct_models;
