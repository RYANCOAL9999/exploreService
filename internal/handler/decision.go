package handler

import (
	"context"
	"errors"
	"log"
	"time"

	"exploreService/internal/model"
	"exploreService/pb"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

//*******************************Disable it*****************************************************************************************************************************//
// PutDecision records the decision of the actor to like or pass the recipient.
// func (h *ExploreHandler) PutDecision(ctx context.Context, req *pb.DecisionRequest) (*pb.PutDecisionResponse, error) {
// 	log.Printf("[gRPC] PutDecision invoked: ActorUserID=%s, RecipientUserID=%s, Liked=%t",
// 		req.GetActorUserId(), req.GetRecipientUserId(), req.GetLikedRecipient())

// 	if req.GetActorUserId() == "" || req.GetRecipientUserId() == "" {
// 		return nil, status.Error(codes.InvalidArgument, "actor_user_id and recipient_user_id are required")
// 	}

// 	// 1. Upsert (Insert or Update if exists to support overwrite)
// 	decision := model.Decision{
// 		ActorUserID:     req.GetActorUserId(),
// 		RecipientUserID: req.GetRecipientUserId(),
// 		LikedRecipient:  req.GetLikedRecipient(),
// 	}

// 	// Using GORM Clauses to handle Upsert based on the unique constraint combination
// 	err := h.db.WithContext(ctx).Clauses(clause.OnConflict{
// 		Columns:   []clause.Column{{Name: "actor_user_id"}, {Name: "recipient_user_id"}},
// 		DoUpdates: clause.AssignmentColumns([]string{"liked_recipient", "updated_at"}),
// 	}).Create(&decision).Error

// 	if err != nil {
// 		return nil, status.Errorf(codes.Internal, "failed to save decision: %v", err)
// 	}

// 	return &pb.PutDecisionResponse{}, nil
// }
//**********************************************************************************************************************************************************************//

// Active concurrent requests are capped at 50 to protect the 6GB available RAM
// from OOM and prevent DB connection pool starvation.
var maxConcurrentRequests = make(chan struct{}, 50)

// Hard timeout limit for any single database operations to mitigate row-level deadlock risks.
const dbHardTimeout = 50 * time.Millisecond

func (h *ExploreHandler) PutDecision(ctx context.Context, req *pb.DecisionRequest) (*pb.PutDecisionResponse, error) {
	// ==============================================================================
	// Architectural Decision 1: Node-Level In-Memory Rate Limiting (Semaphore)
	// ==============================================================================
	// When traffic spikes from upstream load balancers, any incoming request
	// beyond the 50th active worker will be immediately deflected to save resources.
	select {
	case maxConcurrentRequests <- struct{}{}:
		// Successfully acquired execution slot, schedule token release
		defer func() { <-maxConcurrentRequests }()
	default:
		// Capacity reached. Fail-Fast immediately to protect pod memory from ballooning.
		// Returns codes.ResourceExhausted to prompt upstream API collectors to retry later.
		log.Printf("[Throttling Triggered] Concurrency exceeded 50, rejecting request for Actor: %s", req.GetActorUserId())
		return nil, status.Error(codes.ResourceExhausted, "server concurrency limit reached, load shedding active")
	}

	// ==============================================================================
	// Basic Input Validation
	// ==============================================================================
	if req.GetActorUserId() == "" || req.GetRecipientUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "actor_user_id and recipient_user_id are required")
	}

	// ==============================================================================
	// Architectural Decision 2: DB Context Lifecycle Isolation
	// ==============================================================================
	// If 15M records encounter a celebrity profile hot-spot, row-locks can block indefinitely.
	// We encapsulate the execution within a strict 50ms deadline context.

	// dbCtx, cancel := context.WithTimeout(ctx, dbHardTimeout)
	// defer cancel()

	// ------------------------------------------------------------------------------
	// CRITICAL ARCHITECTURAL FIX: Dynamic Timeout Evaluation
	// ------------------------------------------------------------------------------
	// If the upstream context already defines a strict deadline (e.g., during testing or
	// API gateway orchestration), we respect the existing context chain.
	// Otherwise, we enforce our defensive 50ms hard boundary fallback.
	var dbCtx context.Context
	var cancel context.CancelFunc

	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		dbCtx = ctx
		cancel = func() {} // No-op, managed by upstream lifecycle
	} else {
		dbCtx, cancel = context.WithTimeout(ctx, dbHardTimeout)
	}
	defer cancel()

	decision := model.Decision{
		ActorUserID:     req.GetActorUserId(),
		RecipientUserID: req.GetRecipientUserId(),
		LikedRecipient:  req.GetLikedRecipient(),
	}

	// ==============================================================================
	// Core Execution: Atomic UPSERT with Context Enforcement
	// ==============================================================================
	err := h.db.WithContext(dbCtx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "actor_user_id"}, {Name: "recipient_user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"liked_recipient", "updated_at"}),
	}).Create(&decision).Error

	// ==============================================================================
	// Architectural Decision 3: Precision Error Extraction & Fast Release
	// ==============================================================================
	if err != nil {
		// Scenario A: DB query exceeded the 50ms hard limit. Break execution to release worker thread.
		if errors.Is(dbCtx.Err(), context.DeadlineExceeded) {
			log.Printf("[DB Timeout] Deadlock or slow I/O aborted query after 50ms for Actor: %s", req.GetActorUserId())
			return nil, status.Error(codes.DeadlineExceeded, "database row lock wait timeout exceeded")
		}

		// Scenario B: Other deterministic internal database failures.
		return nil, status.Errorf(codes.Internal, "failed to update/insert decision record: %v", err)
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
