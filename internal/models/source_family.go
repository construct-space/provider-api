package models

import "time"

// SourceFamilyRoute — one row per (operator, slot, position, model)
// mapping. Replaces the older SourceFamilyOperator table that crammed
// primary + CSV-of-backups into a single row.
//
// Each row points one operator at one ConstructModel under a slot:
//
//   slot=primary      → the first model the operator tries
//   slot=backup       → ordered fallback chain (position 0, 1, 2...)
//   slot=aggregator   → MoA aggregator (one row, position 0)
//   slot=proposer     → MoA proposer set (position 0, 1, 2...)
//
// Composite unique on (operator_id, slot, position) so admin UI can
// safely add/remove/reorder rows without index conflicts.
type SourceFamilyRoute struct {
	ID         uint   `json:"id" gorm:"primarykey;autoIncrement"`
	OperatorID string `json:"operator_id" gorm:"size:32;uniqueIndex:idx_route_position;index;not null"`
	Slot       string `json:"slot" gorm:"size:16;uniqueIndex:idx_route_position;not null"`
	Position   int    `json:"position" gorm:"uniqueIndex:idx_route_position;not null;default:0"`
	ModelID    string `json:"model_id" gorm:"size:64;not null"`

	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy string    `json:"updated_by" gorm:"size:120"`
	CreatedAt time.Time `json:"created_at"`
}

func (SourceFamilyRoute) TableName() string { return "source_family_routes" }
