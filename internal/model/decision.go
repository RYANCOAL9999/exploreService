package model

import "time"

// Decision represents the database schema for user likes/passes.
type Decision struct {
	ID              uint      `gorm:"primaryKey"`
	ActorUserID     string    `gorm:"type:varchar(64);not null;index:idx_actor_recipient,priority:1;index:idx_actor_liked"`
	RecipientUserID string    `gorm:"type:varchar(64);not null;index:idx_actor_recipient,priority:2;index:idx_recipient_liked_created,priority:1"`
	LikedRecipient  bool      `gorm:"not null;index:idx_actor_liked;index:idx_recipient_liked_created,priority:2"`
	CreatedAt       time.Time `gorm:"index:idx_recipient_liked_created,priority:3"`
	UpdatedAt       time.Time
}
