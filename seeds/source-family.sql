-- Source-family upstream providers, curated models, and operator
-- routing table. Idempotent (ON CONFLICT DO NOTHING) — safe to re-run.
--
-- API keys are stored encrypted at rest (AES-GCM via
-- CREDITS_UPSTREAM_KEY_SECRET). This seed inserts placeholder
-- ciphertext + enabled=false so the rows exist but the gateway
-- can't proxy requests until an admin pastes the real key via
-- Oracle's provider/upstreams page (which encrypts on save +
-- toggles enabled=true).

-- ─── Upstream providers ──────────────────────────────────────────────────

INSERT INTO construct_upstream_providers (id, label, base_url, auth_header, api_key_encrypted, enabled, created_at, updated_at)
VALUES
  ('together',   'Together AI', 'https://api.together.xyz/v1',          'authorization-bearer', 'PASTE_VIA_ORACLE', false, NOW(), NOW()),
  ('fireworks',  'Fireworks AI', 'https://api.fireworks.ai/inference/v1','authorization-bearer', 'PASTE_VIA_ORACLE', false, NOW(), NOW()),
  ('openrouter', 'OpenRouter',  'https://openrouter.ai/api/v1',          'authorization-bearer', 'PASTE_VIA_ORACLE', false, NOW(), NOW()),
  ('deepseek',   'DeepSeek',    'https://api.deepseek.com/v1',           'authorization-bearer', 'PASTE_VIA_ORACLE', false, NOW(), NOW()),
  ('moonshot',   'Moonshot AI', 'https://api.moonshot.ai/v1',            'authorization-bearer', 'PASTE_VIA_ORACLE', false, NOW(), NOW()),
  ('zai',        'Z.AI',        'https://api.z.ai/api/paas/v4',          'authorization-bearer', 'PASTE_VIA_ORACLE', false, NOW(), NOW()),
  ('qwen',       'Qwen',        'https://dashscope-intl.aliyuncs.com/compatible-mode/v1', 'authorization-bearer', 'PASTE_VIA_ORACLE', false, NOW(), NOW()),
  ('mistral',    'Mistral AI',  'https://api.mistral.ai/v1',             'authorization-bearer', 'PASTE_VIA_ORACLE', false, NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

-- ─── Construct catalog (per Source-family role) ──────────────────────────
--
-- Two tables (renamed 2026-05-19 from the muddled construct_models):
--   - construct_picker_entries: user-facing chat rows. Today just `source`.
--   - construct_routing_targets: internal aliases (Apoc, Trinity, …)
--     referenced by source_family_routes. enabled=false hides them from
--     wherever a target list is shown for picking; routing still uses them.

-- ─── User-facing picker entry ─────────────────────────────────────────────
-- One row: "source". Anything else added here shows up in the desktop
-- picker. Routed via source_family_routes, not pinned to an upstream.

INSERT INTO construct_picker_entries (
  id, label, description, icon, route_via_operator, capabilities, enabled, sort_order, created_at, updated_at
) VALUES
  ('source',
     'Source',
     'Mixture of Agents — auto-routed across the Source family (Tank, Trinity, Apoc, Mouse, Oracle, Neo, Morpheus). 100 free prompts per user.',
     'sparkles',
     'tank',
     '["tools","vision","reasoning"]', true, 1, NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

-- ─── Internal routing targets (referenced by source_family_routes) ────────
-- Each row binds a stable id (Apoc, Trinity, …) to one upstream + model id.

INSERT INTO construct_routing_targets (
  id, upstream_provider_id, upstream_model, label, description, icon,
  credits_per_prompt, max_tool_calls_per_credit, max_output_tokens_per_credit,
  thinking_mode, capabilities, enabled, sort_order, created_at, updated_at
) VALUES
  ('construct-tank',
     'fireworks', 'openai/gpt-oss-120b',
     'Tank (router)',
     'Cheap fast router. Picks which Source operator handles a turn.',
     'route',
     1, 4, 512,
     'off', '["tools"]', false, 10, NOW(), NOW()),

  ('construct-trinity',
     'together', 'meta-llama/Llama-3.3-70B-Instruct-Turbo',
     'Trinity (fast tier)',
     'Text + tool dispatch + narrow factual answers.',
     'zap',
     1, 16, 2048,
     'off', '["tools"]', false, 20, NOW(), NOW()),

  ('construct-apoc',
     'openrouter', 'deepseek/deepseek-v4-pro',
     'Apoc (code)',
     'Code generation, scaffolding spaces, long-context refactors.',
     'code',
     2, 32, 8192,
     'auto', '["tools","reasoning"]', false, 30, NOW(), NOW()),

  ('construct-mouse',
     'openrouter', 'moonshotai/kimi-k2.6',
     'Mouse (design)',
     'Design IR + vision-grounded layout generation.',
     'palette',
     2, 16, 4096,
     'off', '["tools","vision"]', false, 40, NOW(), NOW()),

  ('construct-oracle',
     'openrouter', 'moonshotai/kimi-k2.6',
     'Oracle (staff vision)',
     'Staff-only visual triage and document reading.',
     'eye',
     2, 16, 4096,
     'off', '["tools","vision"]', false, 50, NOW(), NOW()),

  ('construct-neo',
     'openrouter', 'deepseek/deepseek-v4-pro',
     'Neo (mid tier)',
     'Best single-model reasoning when MoA is overkill.',
     'brain',
     2, 16, 4096,
     'auto', '["tools","reasoning"]', false, 60, NOW(), NOW()),

  -- Morpheus MoA proposers + aggregator (internal)
  ('construct-morpheus-prop-1',
     'together', 'Qwen/Qwen3-Coder-480B-A35B-Instruct-FP8',
     'Morpheus proposer (Qwen3-Coder-480B)',
     'MoA proposer leg — code-oriented.',
     'sparkles',
     1, 8, 4096,
     'off', '["tools"]', false, 70, NOW(), NOW()),

  ('construct-morpheus-prop-2',
     'fireworks', 'openai/gpt-oss-120b',
     'Morpheus proposer (gpt-oss-120b)',
     'MoA proposer leg — reasoning, cached.',
     'sparkles',
     1, 8, 4096,
     'auto', '["tools","reasoning"]', false, 71, NOW(), NOW()),

  ('construct-morpheus-prop-3',
     'openrouter', 'moonshotai/kimi-k2.6',
     'Morpheus proposer (Kimi-K2.6)',
     'MoA proposer leg — multimodal capable.',
     'sparkles',
     1, 8, 4096,
     'off', '["tools","vision"]', false, 72, NOW(), NOW()),

  ('construct-morpheus-agg',
     'openrouter', 'deepseek/deepseek-v4-pro',
     'Morpheus aggregator (DeepSeek-V4-Pro)',
     'MoA aggregator — synthesises proposer outputs into final reply.',
     'merge',
     3, 16, 8192,
     'auto', '["reasoning"]', false, 73, NOW(), NOW()),

  -- Mistral candidates (added 2026-05-19 after Le Chat Work-mode capability test).
  -- Bench these via api/inference/cmd/bench before promoting any of them
  -- into the Source family freeze.
  -- Pinned to dated snapshots, not -latest aliases. The aliases resolve
  -- to older generation snapshots with lower rate limits + dated behavior.
  ('construct-mistral-small',
     'mistral', 'mistral-small-2506',
     'Mistral Small (Tank / Trinity candidate)',
     'Cheap fast tier. 5M TPM, 20.83 RPS — highest concurrency on Mistral. $0.10/$0.30 per M.',
     'zap',
     1, 8, 2048,
     'off', '["tools"]', false, 80, NOW(), NOW()),

  ('construct-mistral-medium',
     'mistral', 'mistral-medium-2604',
     'Mistral Medium 3.5 (frontier multimodal, agentic + coding)',
     'Premier tier. Multimodal + agentic + coding. $1.50/$7.50 per M. Dated snapshot 2604 = Mistral Medium 3.5 release.',
     'code',
     2, 32, 8192,
     'auto', '["tools","vision","reasoning"]', false, 81, NOW(), NOW()),

  ('construct-mistral-codestral',
     'mistral', 'codestral-2508',
     'Codestral (code completion)',
     'Code-focused, chat completions + function calling. No agent framework. $0.30/$0.90 per M.',
     'code',
     2, 32, 8192,
     'off', '["tools"]', false, 82, NOW(), NOW()),

  ('construct-mistral-devstral',
     'mistral', 'devstral-2512',
     'Devstral 2 (code agents)',
     'Mistral frontier code-agent model. Designed for tool-using software-engineering loops. $0.40/$2.00 per M.',
     'code',
     2, 32, 8192,
     'off', '["tools"]', false, 83, NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

-- ─── Source-family routing ───────────────────────────────────────────────
--
-- One row per (operator_id, slot, position, model_id). Slots:
--   primary    — first model the operator tries (position always 0)
--   backup     — ordered fallback chain (position 0, 1, 2…)
--   aggregator — MoA aggregator (position always 0)
--   proposer   — MoA proposer set (position 0, 1, 2…)
--
-- ON CONFLICT key is (operator_id, slot, position) so re-running the
-- seed is a no-op once the row exists. Admin edits in Oracle replace
-- the full per-operator set transactionally; the seed only seeds the
-- initial wiring.

INSERT INTO source_family_routes (operator_id, slot, position, model_id, created_at, updated_at) VALUES
  -- Tank — cheap fast router. No backups; if the router is down the
  -- whole Source dispatch is down.
  ('tank',     'primary',    0, 'construct-tank',                NOW(), NOW()),

  -- Trinity — fast text + tool dispatch tier.
  ('trinity',  'primary',    0, 'construct-trinity',             NOW(), NOW()),

  -- Apoc — code generation. Falls through Devstral → Mistral Medium → Trinity on errors.
  ('apoc',     'primary',    0, 'construct-apoc',                NOW(), NOW()),
  ('apoc',     'backup',     0, 'construct-mistral-devstral',    NOW(), NOW()),
  ('apoc',     'backup',     1, 'construct-mistral-medium',      NOW(), NOW()),
  ('apoc',     'backup',     2, 'construct-trinity',             NOW(), NOW()),

  -- Mouse — design IR + vision.
  ('mouse',    'primary',    0, 'construct-mouse',               NOW(), NOW()),

  -- Oracle — staff vision + tool calling.
  ('oracle',   'primary',    0, 'construct-oracle',              NOW(), NOW()),

  -- Neo — mid-tier reasoning. Backup to Apoc for code-shaped prompts.
  ('neo',      'primary',    0, 'construct-neo',                 NOW(), NOW()),
  ('neo',      'backup',     0, 'construct-apoc',                NOW(), NOW()),

  -- Morpheus — Mixture-of-Agents ensemble.
  ('morpheus', 'aggregator', 0, 'construct-morpheus-agg',        NOW(), NOW()),
  ('morpheus', 'proposer',   0, 'construct-morpheus-prop-1',     NOW(), NOW()),
  ('morpheus', 'proposer',   1, 'construct-morpheus-prop-2',     NOW(), NOW()),
  ('morpheus', 'proposer',   2, 'construct-morpheus-prop-3',     NOW(), NOW())
ON CONFLICT (operator_id, slot, position) DO NOTHING;
