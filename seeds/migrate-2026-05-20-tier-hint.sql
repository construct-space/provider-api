-- Phase 1 of the L/M/S tier system. AutoMigrate adds the columns;
-- this script seeds sensible defaults so the desktop has something
-- to pre-populate when a user first opens the LLM Providers panel.
--
-- Safe to run multiple times — every UPDATE is idempotent.

BEGIN;

-- Construct picker entry — Source's tier overrides.
-- Medium stays "tank" (current default). Large fans out via Morpheus
-- (MoA, multi-model synthesis). Small uses Trinity (Llama-3.3-70B
-- Turbo on Together — proven, cheap, fast tool calling).
UPDATE construct_picker_entries
SET route_via_operator_large = 'morpheus',
    route_via_operator_small = 'trinity'
WHERE id = 'source'
  AND (route_via_operator_large IS NULL OR route_via_operator_large = '')
  AND (route_via_operator_small IS NULL OR route_via_operator_small = '');

-- Provider catalog tier hints. These are SEED suggestions — the
-- desktop will override per-user. Run them once; admin edits via
-- Oracle stick. If you're re-seeding intentionally and want to reset
-- a user's choice, that's an auth.json reset on the client side,
-- not a server change.

-- Anthropic
UPDATE provider_catalog_models SET tier_hint = 'large'  WHERE provider_id = 'anthropic' AND model_id LIKE '%opus%';
UPDATE provider_catalog_models SET tier_hint = 'medium' WHERE provider_id = 'anthropic' AND model_id LIKE '%sonnet%';
UPDATE provider_catalog_models SET tier_hint = 'small'  WHERE provider_id = 'anthropic' AND model_id LIKE '%haiku%';

-- OpenAI — large = thinking models (o-series), small = mini variants
UPDATE provider_catalog_models SET tier_hint = 'large'  WHERE provider_id = 'openai' AND (model_id LIKE 'o3%' OR model_id LIKE 'o4%' OR model_id LIKE '%-thinking%');
UPDATE provider_catalog_models SET tier_hint = 'small'  WHERE provider_id = 'openai' AND model_id LIKE '%-mini%';
UPDATE provider_catalog_models SET tier_hint = 'medium' WHERE provider_id = 'openai' AND tier_hint IS NULL AND model_id LIKE 'gpt-%';

-- Google
UPDATE provider_catalog_models SET tier_hint = 'large'  WHERE provider_id = 'google' AND model_id LIKE '%-pro%';
UPDATE provider_catalog_models SET tier_hint = 'small'  WHERE provider_id = 'google' AND model_id LIKE '%-flash-lite%';
UPDATE provider_catalog_models SET tier_hint = 'medium' WHERE provider_id = 'google' AND model_id LIKE '%-flash%' AND tier_hint IS NULL;

-- Mistral (BYOK catalog if it exists; provider-api's mistral upstream
-- is a separate concept covered by Construct routing).
UPDATE provider_catalog_models SET tier_hint = 'large'  WHERE provider_id = 'mistral' AND model_id LIKE '%large%';
UPDATE provider_catalog_models SET tier_hint = 'medium' WHERE provider_id = 'mistral' AND model_id LIKE '%medium%';
UPDATE provider_catalog_models SET tier_hint = 'small'  WHERE provider_id = 'mistral' AND model_id LIKE '%small%';

-- Bump the catalog version so connected clients refetch. Table is
-- provider_catalog_version (the Go model is named CatalogVersion but
-- its TableName() returns provider_catalog_version — see provider.go).
UPDATE provider_catalog_version
SET version    = (extract(epoch from NOW())::bigint)::text,
    updated_at = NOW()
WHERE id = 1;

COMMIT;
