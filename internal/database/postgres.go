package database

import (
	"exploreService/internal/model"
	"log"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// NewPostgresConnection initializes and returns a standalone database connection instance
// instead of assigning it to a global variable, improving testability through dependency injection.
func NewPostgresConnection() *gorm.DB {
	dsn := os.Getenv("DB")

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("❌ Failed to connect to DB: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("❌ Failed to get underlying sql.DB: %v", err)
	}

	// Connection pool optimization for high concurrency scenarios.
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	// Run migration after pool is configured so it benefits from the correct connection settings.
	err = db.AutoMigrate(&model.Decision{})
	if err != nil {
		log.Fatalf("❌ Failed to run database auto-migration: %v", err)
	}

	log.Println("✅ Database connected and connection pool configured successfully...")

	return db
}
