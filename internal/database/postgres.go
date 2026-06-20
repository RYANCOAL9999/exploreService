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

	defer func() {
		log.Println("Closing database connection pool...")
		if err := sqlDB.Close(); err != nil {
			log.Printf("Error closing database pool: %v", err)
		}
	}()
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

// ClosePostgresConnection safely closes the database connection pool.
// It checks if the connection instance is nil before attempting to close it, preventing potential panics.
func ClosePostgresConnection(db *gorm.DB) {

	// Check if the database connection instance is nil before proceeding to close it.
	if db == nil {
		log.Println("⚠️ Database connection instance is nil, skipping close.")
		return
	}

	// Retrieve the underlying sql.DB to close the connection pool.
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("❌ Failed to get underlying sql.DB: %v", err)
		return
	}

	// Close the database connection pool and log the result.
	log.Println("🔄 Closing database connection pool...")
	if err := sqlDB.Close(); err != nil {
		log.Printf("❌ Error closing database pool: %v", err)
		return
	}

	log.Println("✅ Database connection pool closed successfully.")

}
