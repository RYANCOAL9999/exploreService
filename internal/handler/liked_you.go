package handler

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"exploreService/internal/model"
	"exploreService/pb"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// cursor holds both timestamp and ID to uniquely identify a pagination position.
// Using only created_at is insufficient because multiple records can share the same
// timestamp (e.g. bulk inserts, low-resolution clocks), which would cause records
// to be silently skipped. The ID acts as a tiebreaker.
type cursor struct {
	Time time.Time
	ID   uint
}

// encodeCursor serialises a cursor into a base64 token safe for gRPC transport.
func encodeCursor(c cursor) string {
	raw := fmt.Sprintf("%d:%d", c.Time.UnixNano(), c.ID)
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

// decodeCursor deserialises a base64 token back into a cursor.
func decodeCursor(token string) (cursor, error) {
	b, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return cursor{}, err
	}

	parts := strings.SplitN(string(b), ":", 2)
	if len(parts) != 2 {
		return cursor{}, fmt.Errorf("malformed cursor")
	}

	nano, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return cursor{}, fmt.Errorf("invalid timestamp in cursor")
	}

	id, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return cursor{}, fmt.Errorf("invalid id in cursor")
	}

	return cursor{
		Time: time.Unix(0, nano),
		ID:   uint(id),
	}, nil
}

// safePageSize clamps the requested page size to a sensible range.
func safePageSize(requested uint32) int {
	if requested <= 0 {
		return 20
	}
	if requested > 100 {
		return 100
	}
	return int(requested)
}

// ListLikedYou lists all users who liked the recipient, newest first, with cursor pagination.
func (h *ExploreHandler) ListLikedYou(ctx context.Context, req *pb.ListLikedYouRequest) (*pb.ListLikedYouResponse, error) {
	log.Printf("[gRPC] ListLikedYou invoked: RecipientUserID=%s", req.GetRecipientUserId())

	if req.GetRecipientUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "recipient_user_id is required")
	}

	pageSize := safePageSize(req.GetPageSize())

	query := h.db.WithContext(ctx).
		Where("recipient_user_id = ? AND liked_recipient = ?", req.GetRecipientUserId(), true).
		Order("created_at DESC, id DESC").
		Limit(pageSize)

	// Apply cursor filter when paginating beyond page 1.
	// The compound condition (created_at, id) < (cursorTime, cursorID) ensures
	// no records are skipped even when multiple rows share the same timestamp.
	if req.GetPaginationToken() != "" {
		c, err := decodeCursor(req.GetPaginationToken())
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid pagination token: %v", err)
		}
		query = query.Where(
			"(created_at, id) < (?, ?)",
			c.Time, c.ID,
		)
	}

	var decisions []model.Decision
	if err := query.Find(&decisions).Error; err != nil {
		return nil, status.Errorf(codes.Internal, "failed to query likes: %v", err)
	}

	likers := make([]*pb.ListLikedYouResponse_Liker, len(decisions))
	for i, d := range decisions {
		likers[i] = &pb.ListLikedYouResponse_Liker{
			ActorId:       d.ActorUserID,
			UnixTimestamp: uint64(d.CreatedAt.Unix()),
		}
	}

	// Only emit a next token if there are potentially more records remaining.
	var nextToken *string
	if len(decisions) == pageSize {
		last := decisions[len(decisions)-1]
		t := encodeCursor(cursor{Time: last.CreatedAt, ID: last.ID})
		nextToken = &t
	}

	return &pb.ListLikedYouResponse{
		Likers:              likers,
		NextPaginationToken: nextToken,
	}, nil
}

// ListNewLikedYou lists users who liked the recipient but whom the recipient has not yet liked back.
func (h *ExploreHandler) ListNewLikedYou(ctx context.Context, req *pb.ListLikedYouRequest) (*pb.ListLikedYouResponse, error) {
	log.Printf("[gRPC] ListNewLikedYou invoked: RecipientUserID=%s", req.GetRecipientUserId())

	// Validation was previously missing here — added to match ListLikedYou behaviour.
	if req.GetRecipientUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "recipient_user_id is required")
	}

	pageSize := safePageSize(req.GetPageSize())

	//*******************************Disable it*****************************************************************************************************************************//
	// LEFT JOIN against the same table to find cases where the recipient has NOT liked back.
	// If reverse.id IS NULL, the recipient has not yet responded with a like.
	// query := h.db.WithContext(ctx).
	// 	Model(&model.Decision{}).
	// 	Select("decisions.*").
	// 	Joins(`LEFT JOIN decisions AS reverse
	// 		ON reverse.actor_user_id = decisions.recipient_user_id
	// 		AND reverse.recipient_user_id = decisions.actor_user_id
	// 		AND reverse.liked_recipient = ?`, true).
	// 	Where("decisions.recipient_user_id = ? AND decisions.liked_recipient = ?", req.GetRecipientUserId(), true).
	// 	Where("reverse.id IS NULL").
	// 	Order("decisions.created_at DESC, decisions.id DESC").
	// 	Limit(pageSize)

	// if req.GetPaginationToken() != "" {
	// 	c, err := decodeCursor(req.GetPaginationToken())
	// 	if err != nil {
	// 		return nil, status.Errorf(codes.InvalidArgument, "invalid pagination token: %v", err)
	// 	}
	// 	query = query.Where(
	// 		"(decisions.created_at, decisions.id) < (?, ?)",
	// 		c.Time, c.ID,
	// 	)
	// }
	// var decisions []model.Decision
	// if err := query.Find(&decisions).Error; err != nil {
	// 	return nil, status.Errorf(codes.Internal, "failed to query new likes: %v", err)
	// }
	//**********************************************************************************************************************************************************************//

	//*******************************New Starting***************************************************************************************************************************//
	// Over-fetch to mitigate data reduction caused by in-memory filtering.
	fetchSize := pageSize * 2

	// ==============================================================================================================================
	// Step 1: Lookup Table 1 — Fetch candidate decisions (Single table scan, NO JOIN)
	// ==============================================================================================================================
	query := h.db.WithContext(ctx).
		Model(&model.Decision{}).
		Where("recipient_user_id = ? AND liked_recipient = ?", req.GetRecipientUserId(), true).
		Order("created_at DESC, id DESC").
		Limit(fetchSize)

	if req.GetPaginationToken() != "" {
		c, err := decodeCursor(req.GetPaginationToken())
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid pagination token: %v", err)
		}
		query = query.Where("(created_at, id) < (?, ?)", c.Time, c.ID)
	}

	var candidateDecisions []model.Decision
	if err := query.Find(&candidateDecisions).Error; err != nil {
		return nil, status.Errorf(codes.Internal, "failed to query candidate decisions: %v", err)
	}

	if len(candidateDecisions) == 0 {
		return &pb.ListLikedYouResponse{}, nil
	}

	// Extract candidate ActorUserIDs for the reverse lookup.
	actorIDs := make([]string, len(candidateDecisions))
	for i, d := range candidateDecisions {
		actorIDs[i] = d.ActorUserID
	}

	// ==============================================================================================================================
	// Step 2: Lookup Table 2 — Reverse check which candidates I have already liked (Batch IN query)
	// ==============================================================================================================================
	var myReverseDecisions []model.Decision
	err := h.db.WithContext(ctx).
		Model(&model.Decision{}).
		Select("recipient_user_id"). // Only recipient_user_id is required to build the lookup map.
		Where("actor_user_id = ? AND recipient_user_id IN (?) AND liked_recipient = ?", req.GetRecipientUserId(), actorIDs, true).
		Find(&myReverseDecisions).Error
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to query reverse decisions: %v", err)
	}

	// ==============================================================================================================================
	// Step 3: In-Memory Hash Filtering
	// ==============================================================================================================================
	// Map matched entries for O(1) high-speed lookups.
	likedBackMap := make(map[string]bool, len(myReverseDecisions))
	for _, rd := range myReverseDecisions {
		likedBackMap[rd.RecipientUserID] = true
	}

	// Filter out matches, capped strictly by the requested pageSize.
	var decisions []model.Decision
	loopBrokeEarly := false // True if we stopped before fully consuming this batch.
	for i, d := range candidateDecisions {
		if !likedBackMap[d.ActorUserID] { // If I have not liked them back.
			decisions = append(decisions, d)
			if len(decisions) == pageSize {
				if i < len(candidateDecisions)-1 {
					loopBrokeEarly = true
				}
				break
			}
		}
	}

	// ==============================================================================================================================
	// Step 4: Serialize Response and Generate Pagination Token
	// ==============================================================================================================================
	likers := make([]*pb.ListLikedYouResponse_Liker, len(decisions))
	for i, d := range decisions {
		likers[i] = &pb.ListLikedYouResponse_Liker{
			ActorId:       d.ActorUserID,
			UnixTimestamp: uint64(d.CreatedAt.Unix()),
		}
	}

	var nextToken *string
	//*******************************Disable it*****************************************************************************************************************************//
	// if len(decisions) == pageSize {
	// 	last := decisions[len(decisions)-1]
	// 	t := encodeCursor(cursor{Time: last.CreatedAt, ID: last.ID})
	// 	nextToken = &t
	// }
	//**********************************************************************************************************************************************************************//

	// ==============================================================================================================================
	// Step 5: Generate the next cursor token if:
	// 1. The final filtered results reach the requested page size limit.
	// 2. The database raw records match the fetchSize, indicating more data might exist for filtering.
	// ==============================================================================================================================
	if len(candidateDecisions) == fetchSize || loopBrokeEarly {
		if len(decisions) > 0 {
			// Safe even when loopBrokeEarly: any unprocessed tail in candidateDecisions sorts
			// strictly after (older than) the last returned decision, so it'll be re-fetched
			// and re-checked on the next page — nothing gets skipped.
			last := decisions[len(decisions)-1]
			t := encodeCursor(cursor{Time: last.CreatedAt, ID: last.ID})
			nextToken = &t
		} else {
			// decisions empty implies loopBrokeEarly is false (can't break early with zero
			// entries when pageSize > 0), so this branch only triggers via condition
			last := candidateDecisions[len(candidateDecisions)-1]
			t := encodeCursor(cursor{Time: last.CreatedAt, ID: last.ID})
			nextToken = &t
		}
	}

	return &pb.ListLikedYouResponse{
		Likers:              likers,
		NextPaginationToken: nextToken,
	}, nil
}

// CountLikedYou counts the number of users who liked the recipient.
func (h *ExploreHandler) CountLikedYou(ctx context.Context, req *pb.CountLikedYouRequest) (*pb.CountLikedYouResponse, error) {
	log.Printf("[gRPC] CountLikedYou invoked: RecipientUserID=%s", req.GetRecipientUserId())

	if req.GetRecipientUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "recipient_user_id is required")
	}

	var count int64
	err := h.db.WithContext(ctx).
		Model(&model.Decision{}).
		Where("recipient_user_id = ? AND liked_recipient = ?", req.GetRecipientUserId(), true).
		Count(&count).Error

	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to count likes: %v", err)
	}

	return &pb.CountLikedYouResponse{
		Count: uint64(count),
	}, nil
}
