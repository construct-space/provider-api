-- Catalog refresh: update capability flags + model_id + enabled/default so
-- brain (and the rest of the desktop) can rely on the catalog as source of
-- truth.
--
-- Why this exists: operator never consulted capabilities — it had its own
-- hardcoded knowledge — so the rows drifted (empty model_id, missing
-- "caching" / "structured" flags, all rows disabled). Brain consumes these
-- fields directly, so they need to be honest.
--
-- Scope of edits:
--   - capabilities       — set per provider's real support matrix
--   - model_id           — populate with the upstream id used in API calls
--   - enabled            — flip to true for models we actually serve
--   - default            — exactly one per provider, the recommended pick
--
-- Untouched on update:
--   - pricing (input_cost_per_1m, etc.)  — staff manages via oracle-web
--   - locked flag
--   - sort_order
--
-- Re-runnable: UPSERTs throughout; safe to apply more than once.
--
-- Capability strings (brain consumes these):
--   tools       — provider supports tool/function calling
--   structured  — provider enforces strict JSON schema on tool inputs
--                 and/or supports response_format json_schema
--   vision      — accepts image inputs
--   caching     — supports prompt caching (cache_control or equivalent)
--   reasoning   — exposes thinking/reasoning blocks
--
-- IMPORTANT: xiaomi/MiMo is intentionally NOT tagged "structured" — its
-- response_format:json_schema is silently ignored (verified 2026-04).

BEGIN;

-- ───────────────────────────────────────────────────────────────────────
-- Providers — INSERT if missing, only touch capabilities + enabled on
-- update. Everything else (URLs, env keys) stays as-is so staff edits in
-- oracle-web aren't clobbered.
-- ───────────────────────────────────────────────────────────────────────

INSERT INTO provider_catalog (
  id, slug, name, description, icon, capabilities,
  enabled, sort_order,
  api_key_enabled, api_key_base_url, api_key_env_keys,
  api_key_docs_url, api_key_signup_url,
  monthly_enabled, monthly_auth_type,
  auth_type, base_url, env_keys, docs_url, signup_url,
  created_at, updated_at
) VALUES
  ('anthropic', 'anthropic', 'Anthropic',
   'Claude models — Opus, Sonnet, Haiku.', 'anthropic',
   '["tools","vision","structured","caching","reasoning"]',
   true, 10,
   true, 'https://api.anthropic.com', '["ANTHROPIC_API_KEY"]',
   'https://docs.anthropic.com/en/api', 'https://console.anthropic.com',
   true, 'oauth',
   'api_key', 'https://api.anthropic.com', '["ANTHROPIC_API_KEY"]',
   'https://docs.anthropic.com/en/api', 'https://console.anthropic.com',
   NOW(), NOW()),

  ('openai', 'openai', 'OpenAI',
   'GPT and o-series models.', 'openai',
   '["tools","vision","structured","caching","reasoning"]',
   true, 20,
   true, 'https://api.openai.com/v1', '["OPENAI_API_KEY"]',
   'https://platform.openai.com/docs/api-reference', 'https://platform.openai.com',
   true, 'oauth',
   'api_key', 'https://api.openai.com/v1', '["OPENAI_API_KEY"]',
   'https://platform.openai.com/docs/api-reference', 'https://platform.openai.com',
   NOW(), NOW()),

  ('google', 'google', 'Google Gemini',
   'Gemini models via Google AI Studio.', 'google',
   '["tools","vision","structured","caching","reasoning"]',
   true, 30,
   true, 'https://generativelanguage.googleapis.com/v1beta', '["GEMINI_API_KEY"]',
   'https://ai.google.dev/docs', 'https://aistudio.google.com',
   false, '',
   'api_key', 'https://generativelanguage.googleapis.com/v1beta', '["GEMINI_API_KEY"]',
   'https://ai.google.dev/docs', 'https://aistudio.google.com',
   NOW(), NOW()),

  ('xiaomi', 'xiaomi', 'Xiaomi MiMo',
   'MiMo models from Xiaomi (multi-region).', 'xiaomi',
   '["tools","reasoning"]',
   true, 40,
   true, 'https://api.mimo.ai/v1', '["MIMO_API_KEY","XIAOMI_API_KEY"]',
   'https://mimo.ai/docs', 'https://mimo.ai',
   false, '',
   'api_key', 'https://api.mimo.ai/v1', '["MIMO_API_KEY","XIAOMI_API_KEY"]',
   'https://mimo.ai/docs', 'https://mimo.ai',
   NOW(), NOW()),

  ('deepseek', 'deepseek', 'DeepSeek',
   'DeepSeek Chat and Reasoner.', 'deepseek',
   '["tools","structured","reasoning"]',
   true, 50,
   true, 'https://api.deepseek.com/v1', '["DEEPSEEK_API_KEY"]',
   'https://api-docs.deepseek.com', 'https://platform.deepseek.com',
   false, '',
   'api_key', 'https://api.deepseek.com/v1', '["DEEPSEEK_API_KEY"]',
   'https://api-docs.deepseek.com', 'https://platform.deepseek.com',
   NOW(), NOW()),

  ('openrouter', 'openrouter', 'OpenRouter',
   'Multi-provider aggregator (OpenAI-compatible).', 'openrouter',
   '["tools","vision","structured"]',
   true, 60,
   true, 'https://openrouter.ai/api/v1', '["OPENROUTER_API_KEY"]',
   'https://openrouter.ai/docs', 'https://openrouter.ai',
   false, '',
   'api_key', 'https://openrouter.ai/api/v1', '["OPENROUTER_API_KEY"]',
   'https://openrouter.ai/docs', 'https://openrouter.ai',
   NOW(), NOW())

ON CONFLICT (id) DO UPDATE SET
  capabilities = EXCLUDED.capabilities,
  enabled      = EXCLUDED.enabled,
  updated_at   = NOW();

-- ───────────────────────────────────────────────────────────────────────
-- Models — UPSERT with the fields brain needs. On update, only refresh
-- capabilities + model_id + enabled + default; leave pricing/locked/sort
-- alone.
-- ───────────────────────────────────────────────────────────────────────

-- ── Anthropic ──────────────────────────────────────────────────────────
INSERT INTO provider_catalog_models (
  id, provider_id, model_id, name,
  capabilities, context_window, max_output_tokens,
  available_on_api_key, available_on_monthly,
  enabled, "default", sort_order,
  created_at, updated_at
) VALUES
  ('anthropic:claude-opus-4-7',     'anthropic', 'claude-opus-4-7',          'Claude Opus 4.7',
   '["tools","vision","structured","caching","reasoning"]',
   200000, 32000, true, true, true, false, 10, NOW(), NOW()),

  ('anthropic:claude-sonnet-4-6',   'anthropic', 'claude-sonnet-4-6',        'Claude Sonnet 4.6',
   '["tools","vision","structured","caching","reasoning"]',
   1000000, 64000, true, true, true, true,  20, NOW(), NOW()),

  ('anthropic:claude-sonnet-4-5',   'anthropic', 'claude-sonnet-4-5',        'Claude Sonnet 4.5',
   '["tools","vision","structured","caching","reasoning"]',
   1000000, 64000, true, true, true, false, 30, NOW(), NOW()),

  ('anthropic:claude-haiku-4-5',    'anthropic', 'claude-haiku-4-5-20251001','Claude Haiku 4.5',
   '["tools","vision","structured","caching","reasoning"]',
   200000, 64000, true, true, true, false, 40, NOW(), NOW())

ON CONFLICT (id) DO UPDATE SET
  model_id     = EXCLUDED.model_id,
  capabilities = EXCLUDED.capabilities,
  enabled      = EXCLUDED.enabled,
  "default"    = EXCLUDED."default",
  updated_at   = NOW();

-- ── OpenAI ─────────────────────────────────────────────────────────────
-- Conservative seed; staff add more in oracle-web. o-series gets
-- "reasoning"; non-reasoning chat models don't.
INSERT INTO provider_catalog_models (
  id, provider_id, model_id, name,
  capabilities, context_window, max_output_tokens,
  available_on_api_key, available_on_monthly,
  enabled, "default", sort_order,
  created_at, updated_at
) VALUES
  ('openai:gpt-4o',       'openai', 'gpt-4o',       'GPT-4o',
   '["tools","vision","structured","caching"]',
   128000, 16384, true, false, true, true, 10, NOW(), NOW()),

  ('openai:gpt-4o-mini',  'openai', 'gpt-4o-mini',  'GPT-4o mini',
   '["tools","vision","structured","caching"]',
   128000, 16384, true, false, true, false, 20, NOW(), NOW()),

  ('openai:o3-mini',      'openai', 'o3-mini',      'o3-mini',
   '["tools","structured","reasoning"]',
   200000, 100000, true, false, true, false, 30, NOW(), NOW()),

  ('openai:o1',           'openai', 'o1',           'o1',
   '["structured","reasoning"]',
   200000, 100000, true, false, true, false, 40, NOW(), NOW())

ON CONFLICT (id) DO UPDATE SET
  model_id     = EXCLUDED.model_id,
  capabilities = EXCLUDED.capabilities,
  enabled      = EXCLUDED.enabled,
  "default"    = EXCLUDED."default",
  updated_at   = NOW();

-- ── Google Gemini ──────────────────────────────────────────────────────
INSERT INTO provider_catalog_models (
  id, provider_id, model_id, name,
  capabilities, context_window, max_output_tokens,
  available_on_api_key, available_on_monthly,
  enabled, "default", sort_order,
  created_at, updated_at
) VALUES
  ('google:gemini-2.5-pro',   'google', 'gemini-2.5-pro',   'Gemini 2.5 Pro',
   '["tools","vision","structured","caching","reasoning"]',
   2000000, 65536, true, false, true, true,  10, NOW(), NOW()),

  ('google:gemini-2.5-flash', 'google', 'gemini-2.5-flash', 'Gemini 2.5 Flash',
   '["tools","vision","structured","caching"]',
   1000000, 65536, true, false, true, false, 20, NOW(), NOW())

ON CONFLICT (id) DO UPDATE SET
  model_id     = EXCLUDED.model_id,
  capabilities = EXCLUDED.capabilities,
  enabled      = EXCLUDED.enabled,
  "default"    = EXCLUDED."default",
  updated_at   = NOW();

-- ── Xiaomi MiMo — structured deliberately omitted ──────────────────────
INSERT INTO provider_catalog_models (
  id, provider_id, model_id, name,
  capabilities, context_window, max_output_tokens,
  available_on_api_key, available_on_monthly,
  enabled, "default", sort_order,
  created_at, updated_at
) VALUES
  ('xiaomi:mimo-7b-rl',     'xiaomi', 'mimo-7b-rl',     'MiMo 7B RL',
   '["tools","reasoning"]',
   32768, 8192, true, false, true, true,  10, NOW(), NOW()),

  ('xiaomi:mimo-vl-7b-rl',  'xiaomi', 'mimo-vl-7b-rl',  'MiMo VL 7B RL',
   '["tools","vision","reasoning"]',
   32768, 8192, true, false, true, false, 20, NOW(), NOW())

ON CONFLICT (id) DO UPDATE SET
  model_id     = EXCLUDED.model_id,
  capabilities = EXCLUDED.capabilities,
  enabled      = EXCLUDED.enabled,
  "default"    = EXCLUDED."default",
  updated_at   = NOW();

-- ── DeepSeek ───────────────────────────────────────────────────────────
INSERT INTO provider_catalog_models (
  id, provider_id, model_id, name,
  capabilities, context_window, max_output_tokens,
  available_on_api_key, available_on_monthly,
  enabled, "default", sort_order,
  created_at, updated_at
) VALUES
  ('deepseek:deepseek-chat',     'deepseek', 'deepseek-chat',     'DeepSeek Chat',
   '["tools","structured"]',
   64000, 8192, true, false, true, true,  10, NOW(), NOW()),

  ('deepseek:deepseek-reasoner', 'deepseek', 'deepseek-reasoner', 'DeepSeek Reasoner',
   '["tools","structured","reasoning"]',
   64000, 8192, true, false, true, false, 20, NOW(), NOW())

ON CONFLICT (id) DO UPDATE SET
  model_id     = EXCLUDED.model_id,
  capabilities = EXCLUDED.capabilities,
  enabled      = EXCLUDED.enabled,
  "default"    = EXCLUDED."default",
  updated_at   = NOW();

-- ── Alibaba — populate model_id (was empty) without touching caps ──────
-- Alibaba seed already set capabilities; just fix model_id and ensure
-- enabled/default. Re-using the model_id == catalog-slug convention.
UPDATE provider_catalog_models SET
  model_id   = REPLACE(id, 'alibaba:', ''),
  enabled    = true,
  updated_at = NOW()
WHERE provider_id = 'alibaba' AND (model_id IS NULL OR model_id = '');

UPDATE provider_catalog_models SET
  "default"  = true,
  updated_at = NOW()
WHERE id = 'alibaba:qwen3-max';

-- ───────────────────────────────────────────────────────────────────────
-- Bump catalog version so brain + every other client invalidates cache.
-- ───────────────────────────────────────────────────────────────────────
-- md5(random()) not gen_random_bytes: the providers DB has no pgcrypto,
-- and gen_random_bytes aborts the whole seed there.
UPDATE provider_catalog_version
   SET version    = substr(md5(random()::text), 1, 16),
       updated_at = NOW()
 WHERE id = 1;

COMMIT;

-- Verification queries (run after COMMIT, not part of the migration):
--
--   SELECT slug, capabilities, enabled FROM provider_catalog ORDER BY sort_order;
--
--   SELECT provider_id, id, model_id, capabilities, enabled, "default"
--     FROM provider_catalog_models
--    ORDER BY provider_id, sort_order;
--
--   SELECT version FROM provider_catalog_version WHERE id = 1;
