package handlers

// Admin CRUD for the Source-family routing table. Oracle's
// provider/source-family page is the only caller. Rows are
// one-per-(operator, slot, position) so the UI can add/remove/reorder
// individual model assignments without rebuilding CSV strings.
//
// Slots:
//   primary    → first model the operator tries (position always 0)
//   backup     → ordered fallback chain (position 0,1,2…)
//   aggregator → MoA aggregator (position always 0)
//   proposer   → MoA proposer set (position 0,1,2…)

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"construct/provider/internal/database"
	"construct/provider/internal/models"

	"gorm.io/gorm"
)

// operatorView is the per-operator response shape returned to Oracle.
// It groups the underlying SourceFamilyRoute rows into the four slots
// the UI cares about so the frontend doesn't have to bucket them.
type operatorView struct {
	OperatorID string   `json:"operator_id"`
	Kind       string   `json:"kind"` // "single" | "moa" — derived from which slots are populated
	Primary    *routeRef `json:"primary,omitempty"`
	Backups    []routeRef `json:"backups"`
	Aggregator *routeRef `json:"aggregator,omitempty"`
	Proposers  []routeRef `json:"proposers"`
}

type routeRef struct {
	ID       uint   `json:"id"`
	ModelID  string `json:"model_id"`
	Position int    `json:"position"`
}

func groupRoutes(rows []models.SourceFamilyRoute) []operatorView {
	byOp := map[string]*operatorView{}
	order := []string{}
	for _, r := range rows {
		v, ok := byOp[r.OperatorID]
		if !ok {
			v = &operatorView{OperatorID: r.OperatorID, Backups: []routeRef{}, Proposers: []routeRef{}}
			byOp[r.OperatorID] = v
			order = append(order, r.OperatorID)
		}
		ref := routeRef{ID: r.ID, ModelID: r.ModelID, Position: r.Position}
		switch r.Slot {
		case "primary":
			v.Primary = &ref
		case "backup":
			v.Backups = append(v.Backups, ref)
		case "aggregator":
			v.Aggregator = &ref
		case "proposer":
			v.Proposers = append(v.Proposers, ref)
		}
	}
	out := make([]operatorView, 0, len(order))
	for _, id := range order {
		v := byOp[id]
		sort.Slice(v.Backups, func(i, j int) bool { return v.Backups[i].Position < v.Backups[j].Position })
		sort.Slice(v.Proposers, func(i, j int) bool { return v.Proposers[i].Position < v.Proposers[j].Position })
		if v.Aggregator != nil || len(v.Proposers) > 0 {
			v.Kind = "moa"
		} else {
			v.Kind = "single"
		}
		out = append(out, *v)
	}
	return out
}

// GET /api/admin/source-family — list grouped by operator.
func AdminListSourceFamily(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) {
		return
	}
	var rows []models.SourceFamilyRoute
	database.DB.Order("operator_id ASC, slot ASC, position ASC").Find(&rows)
	ops := groupRoutes(rows)
	WriteJSON(w, 200, map[string]any{"operators": ops, "total": len(ops)})
}

// GET /api/admin/source-family/{id} — one operator's routes.
func AdminGetSourceFamilyOperator(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	var rows []models.SourceFamilyRoute
	database.DB.Where("operator_id = ?", id).Order("slot ASC, position ASC").Find(&rows)
	if len(rows) == 0 {
		WriteJSON(w, 404, map[string]any{"error": "operator not found"})
		return
	}
	ops := groupRoutes(rows)
	WriteJSON(w, 200, ops[0])
}

// upsertBody is the PUT shape — full replacement of one operator's
// routes. Caller sends the desired final state; handler diffs by
// deleting all existing rows and inserting the new set under a tx.
type upsertBody struct {
	Primary    string   `json:"primary"`    // model_id or ""
	Backups    []string `json:"backups"`    // ordered model_ids
	Aggregator string   `json:"aggregator"` // model_id or ""
	Proposers  []string `json:"proposers"`  // ordered model_ids
}

// PUT /api/admin/source-family/{id} — replace all routes for operator.
func AdminUpsertSourceFamilyOperator(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		WriteJSON(w, 400, map[string]any{"error": "operator_id required"})
		return
	}
	var body upsertBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteJSON(w, 400, map[string]any{"error": "invalid json"})
		return
	}

	// Collect referenced model ids and validate they exist.
	refs := []string{}
	if body.Primary != "" {
		refs = append(refs, body.Primary)
	}
	for _, m := range body.Backups {
		if m = strings.TrimSpace(m); m != "" {
			refs = append(refs, m)
		}
	}
	if body.Aggregator != "" {
		refs = append(refs, body.Aggregator)
	}
	for _, m := range body.Proposers {
		if m = strings.TrimSpace(m); m != "" {
			refs = append(refs, m)
		}
	}
	if len(refs) > 0 {
		var found []string
		database.DB.Model(&models.ConstructRoutingTarget{}).Where("id IN ?", refs).Pluck("id", &found)
		seen := map[string]bool{}
		for _, f := range found {
			seen[f] = true
		}
		missing := []string{}
		for _, ref := range refs {
			if !seen[ref] {
				missing = append(missing, ref)
			}
		}
		if len(missing) > 0 {
			WriteJSON(w, 400, map[string]any{"error": "unknown routing_target ids: " + strings.Join(missing, ", ")})
			return
		}
	}

	now := time.Now()
	by := actor(r)
	rows := []models.SourceFamilyRoute{}
	if body.Primary != "" {
		rows = append(rows, models.SourceFamilyRoute{
			OperatorID: id, Slot: "primary", Position: 0, ModelID: body.Primary,
			UpdatedAt: now, UpdatedBy: by, CreatedAt: now,
		})
	}
	for i, m := range body.Backups {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		rows = append(rows, models.SourceFamilyRoute{
			OperatorID: id, Slot: "backup", Position: i, ModelID: m,
			UpdatedAt: now, UpdatedBy: by, CreatedAt: now,
		})
	}
	if body.Aggregator != "" {
		rows = append(rows, models.SourceFamilyRoute{
			OperatorID: id, Slot: "aggregator", Position: 0, ModelID: body.Aggregator,
			UpdatedAt: now, UpdatedBy: by, CreatedAt: now,
		})
	}
	for i, m := range body.Proposers {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		rows = append(rows, models.SourceFamilyRoute{
			OperatorID: id, Slot: "proposer", Position: i, ModelID: m,
			UpdatedAt: now, UpdatedBy: by, CreatedAt: now,
		})
	}

	err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("operator_id = ?", id).Delete(&models.SourceFamilyRoute{}).Error; err != nil {
			return err
		}
		if len(rows) > 0 {
			if err := tx.Create(&rows).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		WriteJSON(w, 500, map[string]any{"error": "save failed: " + err.Error()})
		return
	}
	bumpCatalogVersion()

	var fresh []models.SourceFamilyRoute
	database.DB.Where("operator_id = ?", id).Order("slot ASC, position ASC").Find(&fresh)
	ops := groupRoutes(fresh)
	if len(ops) == 0 {
		WriteJSON(w, 200, operatorView{OperatorID: id, Kind: "single", Backups: []routeRef{}, Proposers: []routeRef{}})
		return
	}
	WriteJSON(w, 200, ops[0])
}

// DELETE /api/admin/source-family/{id} — wipe all routes for operator.
func AdminDeleteSourceFamilyOperator(w http.ResponseWriter, r *http.Request) {
	if !requireInternalSecret(w, r) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if err := database.DB.Where("operator_id = ?", id).Delete(&models.SourceFamilyRoute{}).Error; err != nil {
		WriteJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	bumpCatalogVersion()
	WriteJSON(w, 204, nil)
}

// actor pulls the gateway-injected user header (X-Auth-User-ID) for
// the updated_by audit column. Empty when missing — the X-Internal-
// Secret check already gated the request, so we don't fail.
func actor(r *http.Request) string {
	return r.Header.Get("X-Auth-User-ID")
}
