-- Tinker upstream + Source-medium / Source-vision picker entries.
-- Idempotent (ON CONFLICT DO NOTHING) — safe to re-run on every deploy.
--
-- Pattern mirrors source-family.sql: rows are inserted disabled, with
-- placeholder ciphertext for the API key. An Oracle admin pastes the
-- real key via the provider/upstreams page, which encrypts on save and
-- flips enabled=true. The proxy doesn't actually validate the bearer
-- value, but the schema requires *some* ciphertext.
--
-- IMPORTANT: base_url must be reachable from the api/provider service.
-- Production: https://llm.lisaos.dev/v1 (Caprover app 'llm' with
-- TLS-terminated public domain). Local dev: override via the Oracle UI
-- to http://host.docker.internal:11435/v1 (Docker → host) or
-- http://localhost:11435/v1 (bare-metal).

-- ─── Upstream provider ───────────────────────────────────────────────────

INSERT INTO construct_upstream_providers (
  id, label, base_url, auth_header, api_key_encrypted, enabled, created_at, updated_at
) VALUES
  ('tinker', 'Construct LLM (Source fine-tunes)', 'https://llm.lisaos.dev/v1',
   'authorization-bearer', 'PASTE_VIA_ORACLE', false, NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

-- ─── Routing targets ─────────────────────────────────────────────────────
-- Two targets, one per fine-tune. upstream_model matches the served name
-- in tinker_proxy.py (CONFIG["served_name"] = "source-medium" today; add
-- a second proxy on a sibling port for source-vision).

INSERT INTO construct_routing_targets (
  id, upstream_provider_id, upstream_model, label, description, icon,
  credits_per_prompt, max_tool_calls_per_credit, max_output_tokens_per_credit,
  thinking_mode, capabilities, enabled, sort_order, created_at, updated_at
) VALUES
  ('tinker-source-medium',
     'tinker', 'source-medium',
     'Source-medium (v6.2)',
     'Construct on-device assistant — fine-tuned Qwen3.6-35B-A3B. Concise, action-oriented, manifest-aware.',
     'sparkles',
     1, 16, 2048,
     'off', '["tools","reasoning"]', false, 90, NOW(), NOW()),

  ('tinker-source-vision',
     'tinker', 'source-vision',
     'Source-vision (v0.1)',
     'Construct on-device assistant with image input — fine-tuned Qwen3-VL-30B-A3B-Instruct. Same character as Source-medium, can consume screenshots/Figma exports.',
     'eye',
     1, 16, 2048,
     'off', '["tools","vision","reasoning"]', false, 91, NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

-- ─── User-facing picker entries ──────────────────────────────────────────
-- Two rows: source-medium and source-vision appear as separate options
-- in the desktop picker. Each routes directly to its own operator (no
-- MoA, no router) so the picker entry is essentially a thin wrapper
-- around the routing target.

INSERT INTO construct_picker_entries (
  id, label, description, icon, route_via_operator, capabilities, enabled, sort_order, created_at, updated_at
) VALUES
  ('source-medium',
     'Source-medium',
     'On-device assistant. Concise, action-oriented, knows the Construct manifest and SDK. Fine-tuned from Qwen3.6-35B-A3B.',
     'sparkles',
     'source-medium',
     '["tools","reasoning"]', true, 5, NOW(), NOW()),

  ('source-vision',
     'Source-vision',
     'On-device assistant with image input. Same character as Source-medium, plus understands screenshots, Figma exports, and other visual inputs.',
     'eye',
     'source-vision',
     '["tools","vision","reasoning"]', true, 6, NOW(), NOW())
ON CONFLICT (id) DO NOTHING;

-- ─── Source-family routes ────────────────────────────────────────────────
-- Each operator has just a primary slot — no fallback chain. If Tinker
-- is unreachable we fail fast rather than silently degrading to Trinity;
-- the user explicitly picked Source-medium and shouldn't get a different
-- model's answer without knowing.

INSERT INTO source_family_routes (operator_id, slot, position, model_id, created_at, updated_at) VALUES
  ('source-medium', 'primary', 0, 'tinker-source-medium', NOW(), NOW()),
  ('source-vision', 'primary', 0, 'tinker-source-vision', NOW(), NOW())
ON CONFLICT (operator_id, slot, position) DO NOTHING;
