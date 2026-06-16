package handler

import (
	"exploreService/pb"

	"gorm.io/gorm"
)

// ExploreHandler handles incoming gRPC requests for the ExploreService.
type ExploreHandler struct {
	pb.UnimplementedExploreServiceServer
	db *gorm.DB
}

// NewExploreHandler creates a new instance of ExploreHandler with injected DB.
func NewExploreHandler(db *gorm.DB) *ExploreHandler {
	return &ExploreHandler{
		db: db,
	}
}
