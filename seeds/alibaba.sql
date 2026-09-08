-- Alibaba Model Studio catalog seed.
-- Apply when the AI Catalyst Program grant is active and the upstream
-- DASHSCOPE key has been pasted into provider-api via oracle-web (the
-- admin save will mark these rows Locked=true so subsequent models.dev
-- syncs don't clobber the hand-tuned pricing).
--
-- Region: International (dashscope-intl). For mainland China switch
-- BaseURL to https://dashscope.aliyuncs.com/compatible-mode/v1.

INSERT INTO provider_catalog (
  id, slug, name, description, icon, capabilities,
  enabled, sort_order,
  api_key_enabled, api_key_base_url, api_key_env_keys,
  api_key_docs_url, api_key_signup_url, api_key_has_shared_key,
  monthly_enabled,
  auth_type, base_url, env_keys, docs_url, signup_url,
  created_at, updated_at
) VALUES (
  'alibaba',
  'alibaba',
  'Alibaba Model Studio',
  'Qwen models via DashScope OpenAI-compatible endpoint.',
  'alibaba',
  '["tools","vision","structured"]',
  true, 90,
  true,
  'https://dashscope-intl.aliyuncs.com/compatible-mode/v1',
  '["DASHSCOPE_API_KEY"]',
  'https://help.aliyun.com/zh/model-studio/developer-reference/use-qwen-by-calling-api',
  'https://www.alibabacloud.com/product/modelstudio',
  false,
  false,
  'api_key',
  'https://dashscope-intl.aliyuncs.com/compatible-mode/v1',
  '["DASHSCOPE_API_KEY"]',
  'https://help.aliyun.com/zh/model-studio/developer-reference/use-qwen-by-calling-api',
  'https://www.alibabacloud.com/product/modelstudio',
  NOW(), NOW()
)
ON CONFLICT (id) DO NOTHING;

-- Models. Pricing left at 0 — staff edit in oracle-web (which flips
-- Locked=true) once the grant terms confirm pass-through rates.
INSERT INTO provider_catalog_models (
  id, provider_id, model_id, name,
  capabilities, context_window, max_output_tokens,
  available_on_api_key, available_on_monthly,
  enabled, "default", sort_order,
  created_at, updated_at
) VALUES
  ('alibaba:qwen3-max',         'alibaba', 'qwen3-max',         'Qwen3 Max',         '["tools","structured"]',          262144, 8192, true, false, true, true,  10, NOW(), NOW()),
  ('alibaba:qwen3-coder-plus',  'alibaba', 'qwen3-coder-plus',  'Qwen3 Coder Plus',  '["tools","structured"]',         1048576, 8192, true, false, true, false, 20, NOW(), NOW()),
  ('alibaba:qwen3-coder-flash', 'alibaba', 'qwen3-coder-flash', 'Qwen3 Coder Flash', '["tools","structured"]',         1048576, 8192, true, false, true, false, 30, NOW(), NOW()),
  ('alibaba:qwen-max',          'alibaba', 'qwen-max',          'Qwen Max',          '["tools","structured"]',           32768, 8192, true, false, true, false, 40, NOW(), NOW()),
  ('alibaba:qwen-plus',         'alibaba', 'qwen-plus',         'Qwen Plus',         '["tools","structured"]',          131072, 8192, true, false, true, false, 50, NOW(), NOW()),
  ('alibaba:qwen-turbo',        'alibaba', 'qwen-turbo',        'Qwen Turbo',        '["tools","structured"]',         1000000, 8192, true, false, true, false, 60, NOW(), NOW()),
  ('alibaba:qwen-vl-max',       'alibaba', 'qwen-vl-max',       'Qwen VL Max',       '["tools","vision","structured"]', 32768, 2048, true, false, true, false, 70, NOW(), NOW()),
  ('alibaba:qwen-vl-plus',      'alibaba', 'qwen-vl-plus',      'Qwen VL Plus',      '["tools","vision","structured"]', 32768, 2048, true, false, true, false, 80, NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

-- Bump catalog version so clients invalidate cache.
-- md5(random()) not gen_random_bytes: the providers DB has no pgcrypto,
-- and gen_random_bytes aborts the whole seed there.
UPDATE provider_catalog_version
   SET version = substr(md5(random()::text), 1, 16),
       updated_at = NOW()
 WHERE id = 1;
