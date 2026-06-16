package model

import "time"

// Decision represents the database schema for user matchmaking choices (Likes and Passes).
// It combines a composite unique index for data protection and target composite indexes for query optimization.
type Decision struct {
	ID uint `gorm:"primaryKey"`

	// ActorUserID acts as the first part of the unique constraint to support automated overwrites (Upsert).
	ActorUserID string `gorm:"type:varchar(64);not null;uniqueIndex:idx_actor_recipient,priority:1;index:idx_actor_liked"`

	// RecipientUserID acts as the second part of the unique index and leads the query optimizer for lookups.
	RecipientUserID string `gorm:"type:varchar(64);not null;uniqueIndex:idx_actor_recipient,priority:2;index:idx_recipient_liked_created,priority:1"`

	// LikedRecipient flags if the action is a Like (true) or Pass (false).
	LikedRecipient bool `gorm:"not null;index:idx_actor_liked;index:idx_recipient_liked_created,priority:2"`

	// CreatedAt is indexed hierarchically to support instant, O(1) constant-time cursor pagination.
	CreatedAt time.Time `gorm:"index:idx_recipient_liked_created,priority:3"`
	UpdatedAt time.Time
}
