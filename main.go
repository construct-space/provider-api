package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"construct/provider/internal/chatlog"
	"construct/provider/internal/config"
	"construct/provider/internal/database"
	"construct/provider/internal/handlers"
	"construct/provider/internal/middleware"
)

func main() {
	cfg := config.Load()
	database.Init(cfg)

	// R2 chat-event logger. No-op when R2_BUCKET env var is unset (local dev).
	logger, err := chatlog.New(context.Background(), chatlog.FromEnv())
	if err != nil {
		log.Fatalf("chatlog init: %v", err)
	}
	if logger != nil {
		defer logger.Stop()
	}
	handlers.SetChatLogger(logger)

	mux := http.NewServeMux()

	// Health (public)
	mux.HandleFunc("GET /health", handlers.Health)
	mux.HandleFunc("GET /api/health", handlers.Health)

	// Public catalog — moved here from source-api on 2026-05-11. The
	// gateway repoints /api/providers/* to this service so operator's
	// modelspec.load keeps hitting the same URL.
	mux.HandleFunc("GET /api/providers", handlers.ListProviders)
	mux.HandleFunc("GET /api/providers/catalog/version", handlers.GetCatalogVersion)

	// Internal admin CRUD — X-Internal-Secret-gated. Oracle is the only
	// caller; it proxies these under its own /api/* routes for the
	// staff UI.
	mux.HandleFunc("GET /api/admin/providers", handlers.AdminListProviders)
	mux.HandleFunc("POST /api/admin/providers", handlers.AdminCreateProvider)
	mux.HandleFunc("GET /api/admin/providers/{id}", handlers.AdminGetProvider)
	mux.HandleFunc("PUT /api/admin/providers/{id}", handlers.AdminUpdateProvider)
	mux.HandleFunc("DELETE /api/admin/providers/{id}", handlers.AdminDeleteProvider)

	mux.HandleFunc("GET /api/admin/providers/{id}/models", handlers.AdminListProviderModels)
	mux.HandleFunc("POST /api/admin/providers/{id}/models", handlers.AdminCreateProviderModel)
	mux.HandleFunc("PUT /api/admin/providers/{id}/models/{modelId}", handlers.AdminUpdateProviderModel)
	mux.HandleFunc("DELETE /api/admin/providers/{id}/models/{modelId}", handlers.AdminDeleteProviderModel)
	mux.HandleFunc("POST /api/admin/providers/{id}/models/{modelId}/sync", handlers.AdminSyncProviderModel)
	mux.HandleFunc("POST /api/admin/providers/{id}/models/{modelId}/unlock", handlers.AdminUnlockProviderModel)

	// OpenAI-compatible inference surface. Standard paths so any OpenAI
	// SDK works with base_url=https://.../api/inference/v1 and the
	// caller's session token / API key — no Construct-specific code
	// required on the client side.
	mux.HandleFunc("GET  /api/inference/v1/models",           handlers.ListInferenceModels)
	mux.HandleFunc("POST /api/inference/v1/chat/completions", handlers.ChatConstruct)

	// Legacy aliases — kept until the desktop ships a release that
	// targets /api/inference/v1. Drop these in a follow-up.
	mux.HandleFunc("GET  /api/construct/models",            handlers.ListConstructModels)
	mux.HandleFunc("POST /api/construct/chat/completions",  handlers.ChatConstruct)
	mux.HandleFunc("POST /api/construct/chat",              handlers.ChatConstruct)

	// Construct runtime — session-authed (gateway injects X-Auth-User-ID).
	mux.HandleFunc("GET  /api/construct/usage", handlers.UsageConstruct)

	// Construct admin — X-Internal-Secret-gated. Oracle is the only caller.
	mux.HandleFunc("GET /api/admin/construct/upstreams", handlers.AdminListConstructUpstreams)
	mux.HandleFunc("POST /api/admin/construct/upstreams", handlers.AdminCreateConstructUpstream)
	mux.HandleFunc("PUT /api/admin/construct/upstreams/{id}", handlers.AdminUpdateConstructUpstream)
	mux.HandleFunc("DELETE /api/admin/construct/upstreams/{id}", handlers.AdminDeleteConstructUpstream)

	// Picker entries — user-facing chat rows shown in the desktop picker.
	mux.HandleFunc("GET /api/admin/construct/picker-entries",         handlers.AdminListPickerEntries)
	mux.HandleFunc("POST /api/admin/construct/picker-entries",        handlers.AdminCreatePickerEntry)
	mux.HandleFunc("PUT /api/admin/construct/picker-entries/{id}",    handlers.AdminUpdatePickerEntry)
	mux.HandleFunc("DELETE /api/admin/construct/picker-entries/{id}", handlers.AdminDeletePickerEntry)

	// Routing targets — internal aliases referenced by source-family routes.
	mux.HandleFunc("GET /api/admin/construct/routing-targets",         handlers.AdminListRoutingTargets)
	mux.HandleFunc("POST /api/admin/construct/routing-targets",        handlers.AdminCreateRoutingTarget)
	mux.HandleFunc("PUT /api/admin/construct/routing-targets/{id}",    handlers.AdminUpdateRoutingTarget)
	mux.HandleFunc("DELETE /api/admin/construct/routing-targets/{id}", handlers.AdminDeleteRoutingTarget)

	mux.HandleFunc("GET /api/admin/construct/config", handlers.AdminGetConstructConfig)
	mux.HandleFunc("PUT /api/admin/construct/config", handlers.AdminUpdateConstructConfig)

	mux.HandleFunc("GET /api/admin/construct/users/{id}", handlers.AdminGetConstructUser)
	mux.HandleFunc("POST /api/admin/construct/users/{id}/grant", handlers.AdminGrantConstructCredits)
	mux.HandleFunc("POST /api/admin/construct/users/{id}/block", handlers.AdminBlockConstructUser)
	mux.HandleFunc("POST /api/admin/construct/users/{id}/unblock", handlers.AdminUnblockConstructUser)

	// Source-family routing table — Oracle's provider/source-family page.
	mux.HandleFunc("GET /api/admin/source-family", handlers.AdminListSourceFamily)
	mux.HandleFunc("GET /api/admin/source-family/{id}", handlers.AdminGetSourceFamilyOperator)
	mux.HandleFunc("PUT /api/admin/source-family/{id}", handlers.AdminUpsertSourceFamilyOperator)
	mux.HandleFunc("DELETE /api/admin/source-family/{id}", handlers.AdminDeleteSourceFamilyOperator)

	handler := middleware.Logger(middleware.CORS(cfg)(mux))

	log.Printf("provider-api listening on :%s", cfg.Port)
	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      300 * time.Second, // streaming chat responses can be long-lived
		IdleTimeout:       120 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
