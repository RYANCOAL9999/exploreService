package model

import "time"

// Decision represents the database schema for user likes/passes.
// type Decision struct {
// 	ID              uint      `gorm:"primaryKey"`
// 	ActorUserID     string    `gorm:"type:varchar(64);not null;index:idx_actor_recipient,priority:1;index:idx_actor_liked"`
// 	RecipientUserID string    `gorm:"type:varchar(64);not null;index:idx_actor_recipient,priority:2;index:idx_recipient_liked_created,priority:1"`
// 	LikedRecipient  bool      `gorm:"not null;index:idx_actor_liked;index:idx_recipient_liked_created,priority:2"`
// 	CreatedAt       time.Time `gorm:"index:idx_recipient_liked_created,priority:3"`
// 	UpdatedAt       time.Time
// }

type Decision struct {
    ID              uint      `gorm:"primaryKey"`
    // Use the same uniqueIndex name (idx_actor_recipient) so GORM 
    // correctly enforces the composite unique constraint during AutoMigrate in SQLite.
    ActorUserID     string    `gorm:"type:varchar(255);not null;uniqueIndex:idx_actor_recipient"`
    RecipientUserID string    `gorm:"type:varchar(255);not null;uniqueIndex:idx_actor_recipient"`
    LikedRecipient  bool      `gorm:"not null"`
    CreatedAt       time.Time
    UpdatedAt       time.Time
}
