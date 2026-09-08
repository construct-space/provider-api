// Package database opens the `credits` Postgres connection used by
// every provider-api request. AutoMigrate is intentionally empty in
// Phase 1 — Phase 2 (catalog migration) adds the first models.
package database

import (
	"fmt"
	"log"
	"time"

	"construct/provider/internal/config"
	"construct/provider/internal/models"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func Init(cfg *config.Config) {
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPass, cfg.DBName)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		log.Fatalf("connect credits: %v", err)
	}
	DB = db
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(50)
		sqlDB.SetMaxIdleConns(10)
		sqlDB.SetConnMaxLifetime(30 * time.Minute)
	}

	// AutoMigrate target list grows per phase. See
	// construct-app/docs/plans/2026-05-11-construct-provider-credits.md
	// for the schema and rollout.
	if err := db.AutoMigrate(
		&models.ProviderCatalog{},
		&models.ProviderCatalogModel{},
		&models.CatalogVersion{},
		&models.ConstructUpstreamProvider{},
		&models.ConstructPickerEntry{},
		&models.ConstructRoutingTarget{},
		&models.ConstructConfig{},
		&models.CreditsBalance{},
		&models.CreditsLedger{},
		&models.SourceFamilyRoute{},
	); err != nil {
		log.Fatalf("automigrate: %v", err)
	}

	log.Printf("connected to postgres: %s@%s:%s", cfg.DBName, cfg.DBHost, cfg.DBPort)
}
