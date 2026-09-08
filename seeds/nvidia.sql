-- NVIDIA NIM catalog seed.
-- integrate.api.nvidia.com serves every hosted model free on the dev
-- tier (nvapi-* key, rate-limited ~40 RPM) — the whole provider is a
-- "free models" lane like OpenRouter's :free set, so no pricing rows.
-- Users bring their own key from build.nvidia.com (free signup).
--
-- Model ids verified against the live GET /v1/models on 2026-07-08;
-- tool calling smoke-tested against z-ai/glm-5.2 (finish_reason ==
-- "tool_calls"). Context windows are conservative documented values —
-- staff tune in oracle-web (which flips Locked=true so re-seeds don't
-- clobber).

INSERT INTO provider_catalog (
  id, slug, name, description, icon, capabilities,
  enabled, sort_order,
  api_key_enabled, api_key_base_url, api_key_env_keys,
  api_key_docs_url, api_key_signup_url, api_key_has_shared_key,
  monthly_enabled, monthly_auth_type,
  auth_type, base_url, env_keys, docs_url, signup_url,
  created_at, updated_at
) VALUES (
  'nvidia',
  'nvidia',
  'NVIDIA',
  'Free NIM inference — GLM, DeepSeek, Kimi, Nemotron and more (rate-limited).',
  'nvidia',
  '["tools","vision","reasoning"]',
  true, 95,
  true,
  'https://integrate.api.nvidia.com/v1',
  '["NVIDIA_API_KEY"]',
  'https://docs.api.nvidia.com',
  'https://build.nvidia.com',
  false,
  false, '',
  'api_key',
  'https://integrate.api.nvidia.com/v1',
  '["NVIDIA_API_KEY"]',
  'https://docs.api.nvidia.com',
  'https://build.nvidia.com',
  NOW(), NOW()
)
-- DO UPDATE (not DO NOTHING): prod had a hand-created 'nvidia' row with
-- the raw API key pasted into api_key_env_keys (served publicly!) plus
-- junk flags (api_key_has_shared_key=true, monthly_auth_type set). The
-- conflict branch heals ONLY config-integrity fields — env-var names,
-- URLs, auth modes. Staff-owned levers (enabled, sort_order, name,
-- description, icon, capabilities) are deliberately NOT in the SET list
-- so a re-run can never override an oracle-web kill-switch or retune.
ON CONFLICT (id) DO UPDATE SET
  api_key_enabled        = EXCLUDED.api_key_enabled,
  api_key_base_url       = EXCLUDED.api_key_base_url,
  api_key_env_keys       = EXCLUDED.api_key_env_keys,
  api_key_docs_url       = EXCLUDED.api_key_docs_url,
  api_key_signup_url     = EXCLUDED.api_key_signup_url,
  api_key_has_shared_key = EXCLUDED.api_key_has_shared_key,
  monthly_enabled        = EXCLUDED.monthly_enabled,
  monthly_auth_type      = EXCLUDED.monthly_auth_type,
  auth_type              = EXCLUDED.auth_type,
  base_url               = EXCLUDED.base_url,
  env_keys               = EXCLUDED.env_keys,
  docs_url               = EXCLUDED.docs_url,
  signup_url             = EXCLUDED.signup_url,
  updated_at             = NOW();

-- Curated chat models only — the live /v1/models list also carries
-- embeddings, safety guards, rerankers and parsers that would 400 in a
-- chat picker. All free on the dev tier.
INSERT INTO provider_catalog_models (
  id, provider_id, model_id, name,
  capabilities, context_window, max_output_tokens,
  available_on_api_key, available_on_monthly,
  enabled, "default", sort_order,
  created_at, updated_at
) VALUES
  ('nvidia:z-ai/glm-5.2',                            'nvidia', 'z-ai/glm-5.2',                            'GLM 5.2',                 '["tools","reasoning"]',           200000, 16384, true, false, true, true,  10, NOW(), NOW()),
  ('nvidia:deepseek-ai/deepseek-v4-pro',             'nvidia', 'deepseek-ai/deepseek-v4-pro',             'DeepSeek V4 Pro',         '["tools","reasoning"]',           131072, 16384, true, false, true, false, 20, NOW(), NOW()),
  ('nvidia:deepseek-ai/deepseek-v4-flash',           'nvidia', 'deepseek-ai/deepseek-v4-flash',           'DeepSeek V4 Flash',       '["tools"]',                       131072, 16384, true, false, true, false, 30, NOW(), NOW()),
  ('nvidia:moonshotai/kimi-k2.6',                    'nvidia', 'moonshotai/kimi-k2.6',                    'Kimi K2.6',               '["tools"]',                       262144, 16384, true, false, true, false, 40, NOW(), NOW()),
  ('nvidia:qwen/qwen3.5-397b-a17b',                  'nvidia', 'qwen/qwen3.5-397b-a17b',                  'Qwen3.5 397B',            '["tools","reasoning"]',           262144, 16384, true, false, true, false, 50, NOW(), NOW()),
  ('nvidia:qwen/qwen3.5-122b-a10b',                  'nvidia', 'qwen/qwen3.5-122b-a10b',                  'Qwen3.5 122B',            '["tools","reasoning"]',           262144, 16384, true, false, true, false, 60, NOW(), NOW()),
  ('nvidia:openai/gpt-oss-120b',                     'nvidia', 'openai/gpt-oss-120b',                     'GPT-OSS 120B',            '["tools","reasoning"]',           131072, 16384, true, false, true, false, 70, NOW(), NOW()),
  ('nvidia:openai/gpt-oss-20b',                      'nvidia', 'openai/gpt-oss-20b',                      'GPT-OSS 20B',             '["tools","reasoning"]',           131072, 16384, true, false, true, false, 80, NOW(), NOW()),
  ('nvidia:meta/llama-4-maverick-17b-128e-instruct', 'nvidia', 'meta/llama-4-maverick-17b-128e-instruct', 'Llama 4 Maverick',        '["tools","vision"]',             1000000, 16384, true, false, true, false, 90, NOW(), NOW()),
  ('nvidia:mistralai/mistral-large-3-675b-instruct-2512', 'nvidia', 'mistralai/mistral-large-3-675b-instruct-2512', 'Mistral Large 3', '["tools"]',                    262144, 16384, true, false, true, false, 100, NOW(), NOW()),
  ('nvidia:nvidia/nemotron-3-super-120b-a12b',       'nvidia', 'nvidia/nemotron-3-super-120b-a12b',       'Nemotron 3 Super 120B',   '["tools","reasoning"]',           131072, 16384, true, false, true, false, 110, NOW(), NOW()),
  ('nvidia:nvidia/llama-3.3-nemotron-super-49b-v1.5','nvidia', 'nvidia/llama-3.3-nemotron-super-49b-v1.5','Nemotron Super 49B v1.5', '["tools","reasoning"]',           131072, 16384, true, false, true, false, 120, NOW(), NOW()),
  ('nvidia:minimaxai/minimax-m3',                    'nvidia', 'minimaxai/minimax-m3',                    'MiniMax M3',              '["tools","reasoning"]',           200000, 16384, true, false, true, false, 130, NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

-- Tier hints (L/M/S picker pre-population) — same post-insert UPDATE
-- style as migrate-2026-05-20-tier-hint.sql. `locked IS NOT TRUE`
-- honours the admin-save-locks-row convention (sync_model.go skips
-- locked rows) so a re-run never reverts staff-tuned hints.
UPDATE provider_catalog_models SET tier_hint = 'large'  WHERE provider_id = 'nvidia' AND locked IS NOT TRUE AND model_id IN ('z-ai/glm-5.2', 'deepseek-ai/deepseek-v4-pro', 'qwen/qwen3.5-397b-a17b', 'mistralai/mistral-large-3-675b-instruct-2512');
UPDATE provider_catalog_models SET tier_hint = 'medium' WHERE provider_id = 'nvidia' AND locked IS NOT TRUE AND model_id IN ('moonshotai/kimi-k2.6', 'qwen/qwen3.5-122b-a10b', 'openai/gpt-oss-120b', 'meta/llama-4-maverick-17b-128e-instruct', 'nvidia/nemotron-3-super-120b-a12b', 'minimaxai/minimax-m3');
UPDATE provider_catalog_models SET tier_hint = 'small'  WHERE provider_id = 'nvidia' AND locked IS NOT TRUE AND model_id IN ('deepseek-ai/deepseek-v4-flash', 'openai/gpt-oss-20b', 'nvidia/llama-3.3-nemotron-super-49b-v1.5');

-- Bump catalog version so clients invalidate cache. md5(random()) —
-- not gen_random_bytes — because the providers DB has no pgcrypto.
UPDATE provider_catalog_version
   SET version = substr(md5(random()::text), 1, 16),
       updated_at = NOW()
 WHERE id = 1;
