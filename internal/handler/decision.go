package handler

import (
	"context"
	"errors"
	"log"

	"exploreService/internal/model"
	"exploreService/pb"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PutDecision records the decision of the actor to like or pass the recipient.
func (h *ExploreHandler) PutDecision(ctx context.Context, req *pb.DecisionRequest) (*pb.PutDecisionResponse, error) {
	log.Printf("[gRPC] PutDecision invoked: ActorUserID=%s, RecipientUserID=%s, Liked=%t",
		req.GetActorUserId(), req.GetRecipientUserId(), req.GetLikedRecipient())

	if req.GetActorUserId() == "" || req.GetRecipientUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "actor_user_id and recipient_user_id are required")
	}

	// 1. Upsert (Insert or Update if exists to support overwrite)
	decision := model.Decision{
		ActorUserID:     req.GetActorUserId(),
		RecipientUserID: req.GetRecipientUserId(),
		LikedRecipient:  req.GetLikedRecipient(),
	}

	// Using GORM Clauses to handle Upsert based on the unique constraint combination
	err := h.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "actor_user_id"}, {Name: "recipient_user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"liked_recipient", "updated_at"}),
	}).Create(&decision).Error

	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to save decision: %v", err)
	}

	return &pb.PutDecisionResponse{}, nil
}

func (h *ExploreHandler) MutualLikes(ctx context.Context, req *pb.DecisionRequest) (*pb.MutualLikesResponse, error) {
	log.Printf("[gRPC] PutDecision invoked: ActorUserID=%s, RecipientUserID=%s, Liked=%t",
		req.GetActorUserId(), req.GetRecipientUserId(), req.GetLikedRecipient())

	if req.GetActorUserId() == "" || req.GetRecipientUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "actor_user_id and recipient_user_id are required")
	}

	mutualLikes := false
	if req.GetLikedRecipient() {
		var reverseDecision model.Decision
		err := h.db.WithContext(ctx).
			Where("actor_user_id = ? AND recipient_user_id = ? AND liked_recipient = ?",
				req.GetRecipientUserId(), req.GetActorUserId(), true).
			First(&reverseDecision).Error

		if err == nil {
			mutualLikes = true
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, status.Errorf(codes.Internal, "failed to check mutual likes: %v", err)
		}
	}

	return &pb.MutualLikesResponse{
		MutualLikes: mutualLikes,
	}, nil

}
