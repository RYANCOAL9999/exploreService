package handler

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"exploreService/internal/model"
	"exploreService/pb"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupTestDB initializes a lightning-fast in-memory SQLite database
// and runs auto-migration for isolated unit testing.
func setupTestDB(t *testing.T) *gorm.DB {
	// Use t.Name() as the DB name so each test
	// gets its own isolated in-memory instance
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=private", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect to in-memory sqlite db: %v", err)
	}

	// Migrate the schema just like we do in production Postgres
	err = db.AutoMigrate(&model.Decision{})
	if err != nil {
		t.Fatalf("failed to migrate test db schema: %v", err)
	}

	// Close the connection after the test completes to release the in-memory DB
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		sqlDB.Close()
	})

	return db
}

// =====================================================================================================
// CASE A: Decision Overwrite Test
// =====================================================================================================
// This test ensures that when an actor changes their mind and submits a new
// decision for the same recipient, the old record is elegantly overwritten (Upsert)
// rather than duplicating rows or throwing a unique constraint error.
func TestPutDecision_Overwrite(t *testing.T) {
	// =================================================================================================
	// 1. Setup isolated test environment
	// =================================================================================================
	db := setupTestDB(t)
	h := NewExploreHandler(db)
	ctx := context.Background()

	actorID := "user_alice"
	recipientID := "user_bob"

	// =================================================================================================
	// 2. First Decision: Alice PASSES Bob (LikedRecipient = false)
	// =================================================================================================
	firstReq := &pb.DecisionRequest{
		ActorUserId:     actorID,
		RecipientUserId: recipientID,
		LikedRecipient:  false,
	}

	_, err := h.PutDecision(ctx, firstReq)

	if err != nil {
		t.Fatalf("First PutDecision failed: %v", err)
	}

	// =================================================================================================
	// 3. Verify database state after first decision
	// =================================================================================================
	var count int64
	db.Model(&model.Decision{}).Where("actor_user_id = ? AND recipient_user_id = ?", actorID, recipientID).Count(&count)
	if count != 1 {
		t.Errorf("Expected exactly 1 decision row in DB, found %d", count)
	}

	var savedDecision model.Decision
	db.Where("actor_user_id = ? AND recipient_user_id = ?", actorID, recipientID).First(&savedDecision)
	if savedDecision.LikedRecipient {
		t.Errorf("Expected LikedRecipient to be false, got true")
	}

	// =================================================================================================
	// 4. Second Decision (Overwrite): Alice changes her mind and LIKES Bob (LikedRecipient = true)
	// =================================================================================================
	secondReq := &pb.DecisionRequest{
		ActorUserId:     actorID,
		RecipientUserId: recipientID,
		LikedRecipient:  true,
	}

	_, err = h.PutDecision(ctx, secondReq)
	if err != nil {
		t.Fatalf("Second PutDecision (overwrite) failed: %v", err)
	}

	// =================================================================================================
	// 5. Critical Verifications for Overwrite Behavior
	// =================================================================================================
	// Total rows for this pair must STILL be exactly 1 (No duplicate records!)
	db.Model(&model.Decision{}).Where("actor_user_id = ? AND recipient_user_id = ?", actorID, recipientID).Count(&count)
	if count != 1 {
		t.Errorf("Overwrite failed: Expected exactly 1 row in DB, but found %d (records duplicated!)", count)
	}

	// The value inside that 1 row must be updated to true
	db.Where("actor_user_id = ? AND recipient_user_id = ?", actorID, recipientID).First(&savedDecision)
	if !savedDecision.LikedRecipient {
		t.Errorf("Update failed: Expected LikedRecipient to be overwritten to true, but remained false")
	}

	// =================================================================================================
	// 6. Verification for MutualLikes status
	// =================================================================================================
	// We set LikedRecipient = true here to pass the handler's outer guard clause,
	// allowing the system to query the DB and confirm that Bob hasn't liked Alice back yet.
	mutualReq := &pb.DecisionRequest{
		ActorUserId:     actorID,
		RecipientUserId: recipientID,
		LikedRecipient:  true,
	}

	mutualResp, err := h.MutualLikes(ctx, mutualReq)
	if err != nil {
		t.Fatalf("MutualLikes check failed: %v", err)
	}

	// Since Bob hasn't liked Alice back yet, MutualLikes must be false
	if mutualResp.GetMutualLikes() {
		t.Errorf("Expected MutualLikes to be false (Bob hasn't liked back yet), got true")
	}

}

// =====================================================================================================
// CASE B: Mutual Like Detection Test
// =====================================================================================================
// This test verifies the match-making engine. When User B likes User A,
// and subsequently User A likes User B, the system must instantly detect
// this overlap and return MutualLikes = true in the gRPC response.
func TestPutDecision_MutualLikeDetection(t *testing.T) {
	// =================================================================================================
	// 1. Setup isolated test environment
	// =================================================================================================
	db := setupTestDB(t)
	h := NewExploreHandler(db)
	ctx := context.Background()
	userA := "user_alpha"
	userB := "user_beta"

	// =================================================================================================
	// 2. Step 1: User B LIKES User A first
	// =================================================================================================
	// We seed this directly into the DB to represent an existing historical state
	historicalLike := model.Decision{
		ActorUserID:     userB,
		RecipientUserID: userA,
		LikedRecipient:  true,
	}
	if err := db.Create(&historicalLike).Error; err != nil {
		t.Fatalf("failed to seed historical like from B to A: %v", err)
	}

	// =================================================================================================
	// 3. Step 2: User A now LIKES User B via gRPC call
	// =================================================================================================
	reqA := &pb.DecisionRequest{
		ActorUserId:     userA,
		RecipientUserId: userB,
		LikedRecipient:  true,
	}

	_, err := h.PutDecision(ctx, reqA)
	if err != nil {
		t.Fatalf("PutDecision from A to B failed: %v", err)
	}

	// =================================================================================================
	// 4. Critical Verification for Match-Making
	// =================================================================================================
	// Both users have liked each other; MutualLikes must return true.
	mutualResp, err := h.MutualLikes(ctx, reqA)
	if err != nil {
		t.Fatalf("PutDecision overwrite to Pass failed: %v", err)
	}

	// Since both users have liked each other, MutualLikes must return true
	if !mutualResp.GetMutualLikes() {
		t.Errorf("Logic Error: MutualLikes triggered even though Actor chose to PASS")
	}

	// =================================================================================================
	// 5. Edge Case Verification: Overwrite to PASS
	// =================================================================================================
	// If User A changes their mind and passes on User B, MutualLikes must immediately return false.
	passReq := &pb.DecisionRequest{
		ActorUserId:     userA,
		RecipientUserId: userB,
		LikedRecipient:  false, // User A is now passing on User B
	}

	_, err = h.PutDecision(ctx, passReq)
	if err != nil {
		t.Fatalf("PutDecision overwrite to Pass failed: %v", err)
	}

	// Since LikedRecipient = false, this call will hit the handler's early guard clause,
	// bypassing the DB lookup and cleanly returning false.
	passMutualResp, err := h.MutualLikes(ctx, passReq)
	if err != nil {
		t.Fatalf("MutualLikes check after Pass failed: %v", err)
	}

	if passMutualResp.GetMutualLikes() {
		t.Errorf("Logic Error: MutualLikes triggered even though Actor chose to PASS")
	}

}

// =====================================================================================================
// CASE C: Heavy User Cursor Pagination Test
// =====================================================================================================
// This test challenges the scale requirement by simulating a user with multiple
// likes. It verifies that ListLikedYou returns results in reverse chronological
// order (newest first), properly generates an encoded NextPaginationToken, and
// allows seamless paging without using performance-killing SQL OFFSETS.
func TestListLikedYou_CursorPagination(t *testing.T) {
	// =================================================================================================
	// 1. Setup isolated test environment
	// =================================================================================================
	db := setupTestDB(t)
	h := NewExploreHandler(db)
	ctx := context.Background()
	targetUser := "user_celebrity"

	// =================================================================================================
	// 2. Seed 5 historical likes with distinct, ascending timestamps
	// =================================================================================================
	// Likers: user_1 (oldest) -> user_5 (newest)
	baseTime := time.Now().Truncate(time.Second)
	for i := 1; i <= 5; i++ {
		like := model.Decision{
			ActorUserID:     "user_" + strconv.Itoa(i),
			RecipientUserID: targetUser,
			LikedRecipient:  true,
			CreatedAt:       baseTime.Add(time.Duration(i) * time.Hour), // Each like is 1 hour newer
		}
		if err := db.Create(&like).Error; err != nil {
			t.Fatalf("failed to seed pagination data: %v", err)
		}
	}

	// Paginate with a page size of 2 to test multiple pages
	pswithTwo := uint32(2)

	// =================================================================================================
	// 3. PAGE 1: Fetch first 2 records (Should be user_5 and user_4)
	// =================================================================================================
	page1Req := &pb.ListLikedYouRequest{
		RecipientUserId: targetUser,
		PageSize:        &pswithTwo,
	}

	page1Resp, err := h.ListLikedYou(ctx, page1Req)
	if err != nil {
		t.Fatalf("Page 1 query failed: %v", err)
	}

	if len(page1Resp.GetLikers()) != 2 {
		t.Fatalf("Expected 2 items on Page 1, got %d", len(page1Resp.GetLikers()))
	}

	// Must be newest first: user_5 then user_4
	if page1Resp.GetLikers()[0].GetActorId() != "user_5" || page1Resp.GetLikers()[1].GetActorId() != "user_4" {
		t.Errorf("Page 1 sort order incorrect. Expected [user_5, user_4], got [%s, %s]",
			page1Resp.GetLikers()[0].GetActorId(), page1Resp.GetLikers()[1].GetActorId())
	}

	token1 := page1Resp.GetNextPaginationToken()
	if token1 == "" {
		t.Fatalf("Expected a valid NextPaginationToken on Page 1, got empty")
	}

	// =================================================================================================
	// 4. PAGE 2: Fetch next 2 records using token1 (Should be user_3 and user_2)
	// =================================================================================================
	page2Req := &pb.ListLikedYouRequest{
		RecipientUserId: targetUser,
		PageSize:        &pswithTwo,
		PaginationToken: &token1,
	}

	page2Resp, err := h.ListLikedYou(ctx, page2Req)
	if err != nil {
		t.Fatalf("Page 2 query failed: %v", err)
	}

	if len(page2Resp.GetLikers()) != 2 {
		t.Fatalf("Expected 2 items on Page 2, got %d", len(page2Resp.GetLikers()))
	}

	if page2Resp.GetLikers()[0].GetActorId() != "user_3" || page2Resp.GetLikers()[1].GetActorId() != "user_2" {
		t.Errorf("Page 2 sort order incorrect. Expected [user_3, user_2], got [%s, %s]",
			page2Resp.GetLikers()[0].GetActorId(), page2Resp.GetLikers()[1].GetActorId())
	}

	token2 := page2Resp.GetNextPaginationToken()

	if token2 == "" {
		t.Fatalf("Expected a valid NextPaginationToken on Page 2, got empty")
	}

	// =================================================================================================
	// 5. PAGE 3: Fetch final record using token2 (Should be user_1)
	// =================================================================================================
	page3Req := &pb.ListLikedYouRequest{
		RecipientUserId: targetUser,
		PageSize:        &pswithTwo,
		PaginationToken: &token2,
	}

	page3Resp, err := h.ListLikedYou(ctx, page3Req)
	if err != nil {
		t.Fatalf("Page 3 query failed: %v", err)
	}

	if len(page3Resp.GetLikers()) != 1 {
		t.Fatalf("Expected 1 final item on Page 3, got %d", len(page3Resp.GetLikers()))
	}

	if page3Resp.GetLikers()[0].GetActorId() != "user_1" {
		t.Errorf("Page 3 item incorrect. Expected user_1, got %s", page3Resp.GetLikers()[0].GetActorId())
	}

	// Essential Check: Since there are no more records, NextPaginationToken MUST be nil!
	if page3Resp.GetNextPaginationToken() != "" {
		t.Errorf("Expected NextPaginationToken to be nil on the final page, but got a token")
	}
}

// =====================================================================================================
// CASE D: Isolated Unit Tests for MutualLikes API
// =====================================================================================================
// This test suite micro-tests the MutualLikes handler in isolation by directly
// seeding varied state matrices into the database, verifying the early guard
// clause, and checking the robustness of the read-only relation lookup.
func TestMutualLikes_IsolatedScenarios(t *testing.T) {
	// =================================================================================================
	// 1. Setup isolated test environment
	// =================================================================================================
	db := setupTestDB(t)
	h := NewExploreHandler(db)
	ctx := context.Background()
	userA := "user_alpha"
	userB := "user_beta"

	// =================================================================================================
	// 2. Scenario 1: Clean Slate (Zero Records in Database)
	// =================================================================================================
	// Ensures that if two users have never seen each other, it returns false safely.
	cleanReq := &pb.DecisionRequest{
		ActorUserId:     userA,
		RecipientUserId: userB,
		LikedRecipient:  true,
	}
	resp1, err := h.MutualLikes(ctx, cleanReq)
	if err != nil {
		t.Fatalf("Scenario 1 failed: %v", err)
	}
	if resp1.GetMutualLikes() {
		t.Errorf("Scenario 1 Error: Expected false on a clean database, got true")
	}

	// =================================================================================================
	// 3. Scenario 2: Early Guard Clause Verification (The "Pass" Optimization)
	// =================================================================================================
	// Even if User B historically LIKED User A, if User A invokes this with LikedRecipient = false,
	// the code MUST trigger the early if-guard, short-circuit, and return false without hitting the DB.
	historicalLike := model.Decision{
		ActorUserID:     userB,
		RecipientUserID: userA,
		LikedRecipient:  true,
	}
	if err := db.Create(&historicalLike).Error; err != nil {
		t.Fatalf("Failed to seed historical like for Scenario 2: %v", err)
	}

	passReq := &pb.DecisionRequest{
		ActorUserId:     userA,
		RecipientUserId: userB,
		LikedRecipient:  false, // This triggers your 'if req.GetLikedRecipient()' check
	}
	resp2, err := h.MutualLikes(ctx, passReq)
	if err != nil {
		t.Fatalf("Scenario 2 failed: %v", err)
	}
	if resp2.GetMutualLikes() {
		t.Errorf("Scenario 2 Error: Guard clause failed. MutualLikes returned true even though Actor passed")
	}

	// Clean database state for the next deterministic scenario
	db.Exec("DELETE FROM decisions")

	// =================================================================================================
	// 4. Scenario 3: Asymmetric Dislike State (Double-PASS)
	// =================================================================================================
	// If both users explicitly chose to PASS on each other in the database,
	// MutualLikes must return false even if the request payload carries LikedRecipient = true.
	dislikeRecords := []model.Decision{
		{ActorUserID: userA, RecipientUserID: userB, LikedRecipient: false},
		{ActorUserID: userB, RecipientUserID: userA, LikedRecipient: false},
	}
	if err := db.Create(&dislikeRecords).Error; err != nil {
		t.Fatalf("Failed to seed double-pass records for Scenario 3: %v", err)
	}

	attackReq := &pb.DecisionRequest{
		ActorUserId:     userA,
		RecipientUserId: userB,
		LikedRecipient:  true, // Pretending to like, forcing the code to query the DB
	}
	resp3, err := h.MutualLikes(ctx, attackReq)
	if err != nil {
		t.Fatalf("Scenario 3 failed: %v", err)
	}
	if resp3.GetMutualLikes() {
		t.Errorf("Scenario 3 Error: Relation engine bypassed filters; returned true for historical Double-PASS")
	}
}

// =====================================================================================================
// CASE E: New Liked You Exclusion Test
// =====================================================================================================
// This test verifies that ListNewLikedYou only displays records of users who
// liked the recipient, while strictly excluding individuals whom the recipient
// has already liked back (Mutual Matches). This prevents redundant data on
// incoming match feeds.
func TestListNewLikedYou_ExclusionFilter(t *testing.T) {
	// =================================================================================================
	// 1. Setup isolated test environment
	// =================================================================================================
	db := setupTestDB(t)
	h := NewExploreHandler(db)
	ctx := context.Background()
	targetUser := "user_recipient"

	// =================================================================================================
	// 2. Scene 1: user_new_liker LIKES targetUser (Target user has NOT reacted back yet)
	// =================================================================================================
	newLiker := model.Decision{
		ActorUserID:     "user_new_liker",
		RecipientUserID: targetUser,
		LikedRecipient:  true,
		CreatedAt:       time.Now().Add(-2 * time.Hour),
	}
	if err := db.Create(&newLiker).Error; err != nil {
		t.Fatalf("failed to seed new liker: %v", err)
	}

	// =================================================================================================
	// 3. Scene 2: user_mutual_match LIKES targetUser, AND targetUser LIKES them back (Mutual Like)
	// =================================================================================================
	mutualLiker := model.Decision{
		ActorUserID:     "user_mutual_match",
		RecipientUserID: targetUser,
		LikedRecipient:  true,
		CreatedAt:       time.Now().Add(-1 * time.Hour),
	}
	if err := db.Create(&mutualLiker).Error; err != nil {
		t.Fatalf("failed to seed mutual liker: %v", err)
	}

	// The counter-decision (Target user liking user_mutual_match back)
	targetLikeBack := model.Decision{
		ActorUserID:     targetUser,
		RecipientUserID: "user_mutual_match",
		LikedRecipient:  true,
		CreatedAt:       time.Now(),
	}
	if err := db.Create(&targetLikeBack).Error; err != nil {
		t.Fatalf("failed to seed target user counter-like: %v", err)
	}

	// =================================================================================================
	// 4. EXECUTE QUERY & VERIFY
	// =================================================================================================
	// Set a large page size to ensure we get all potential likers in one response
	pswith10 := uint32(10)

	req := &pb.ListLikedYouRequest{
		RecipientUserId: targetUser,
		PageSize:        &pswith10,
	}

	resp, err := h.ListNewLikedYou(ctx, req)
	if err != nil {
		t.Fatalf("ListNewLikedYou call failed: %v", err)
	}

	// =================================================================================================
	// 5. Validation 1: The length of results must be EXACTLY 1
	// =================================================================================================
	if len(resp.GetLikers()) != 1 {
		t.Fatalf("Filter Error: Expected exactly 1 new liker, but found %d in response", len(resp.GetLikers()))
	}

	// =================================================================================================
	// 6. Validation 2: The remaining user MUST be "user_new_liker"
	// =================================================================================================
	remainingActor := resp.GetLikers()[0].GetActorId()
	if remainingActor != "user_new_liker" {
		t.Errorf("Filter Mismatch: Expected 'user_new_liker' to be present, but found '%s' instead. Mutual match was not filtered out!", remainingActor)
	}
}

// =====================================================================================================
// CASE F: Accurate Counting & Noise Isolation Test
// =====================================================================================================
// This test ensures that CountLikedYou returns a 100% precise calculation.
// It injects various database noises (such as pass decisions and likes directed
// to other users) and verifies that the counter filters them out completely,
// leveraging the optimized database indexes.
func TestCountLikedYou_NoiseIsolation(t *testing.T) {
	// =================================================================================================
	// 1. Setup isolated test environment
	// =================================================================================================
	db := setupTestDB(t)
	h := NewExploreHandler(db)
	ctx := context.Background()
	targetUser := "user_popular"

	// =================================================================================================
	// 2. Inject Valid Data: 2 distinct users LIKED targetUser
	// =================================================================================================
	validLike1 := model.Decision{
		ActorUserID:     "user_fan_a",
		RecipientUserID: targetUser,
		LikedRecipient:  true,
	}
	validLike2 := model.Decision{
		ActorUserID:     "user_fan_b",
		RecipientUserID: targetUser,
		LikedRecipient:  true,
	}
	_ = db.Create(&validLike1)
	_ = db.Create(&validLike2)

	// =================================================================================================
	// 3. Inject Noise Type 1: A user PASSED targetUser (LikedRecipient = false)
	// =================================================================================================
	passNoise := model.Decision{
		ActorUserID:     "user_hater",
		RecipientUserID: targetUser,
		LikedRecipient:  false, // This is a PASS, should NOT be counted!
	}
	_ = db.Create(&passNoise)

	// =================================================================================================
	// 4. Inject Noise Type 2: A user LIKED someone else entirely
	// =================================================================================================
	wrongTargetNoise := model.Decision{
		ActorUserID:     "user_fan_c",
		RecipientUserID: "user_someone_else", // Different recipient, should NOT be counted!
		LikedRecipient:  true,
	}
	_ = db.Create(&wrongTargetNoise)

	// =================================================================================================
	// 5. EXECUTE COUNTER & VERIFY
	// =================================================================================================
	req := &pb.CountLikedYouRequest{
		RecipientUserId: targetUser,
	}

	resp, err := h.CountLikedYou(ctx, req)
	if err != nil {
		t.Fatalf("CountLikedYou call failed: %v", err)
	}

	// Critical Assertion: The total count MUST be exactly 2, ignoring all noise records.
	expectedCount := uint64(2)
	if resp.GetCount() != expectedCount {
		t.Errorf("Counting Error: Expected exactly %d likes for '%s', but got %d instead. Database noise leaked into the count!",
			expectedCount, targetUser, resp.GetCount())
	}
}

// =====================================================================================================
// CASE G: ListNewLikedYou Underfilled Page Bug Test
// =====================================================================================================
// This test verifies the edge case where memory-filtering
// causes empty pages despite more valid data existing
// in the database due to hardcoded fetch limits.
func TestListNewLikedYou_UnderfilledPage_Bug(t *testing.T) {
	// =================================================================================================
	// 1. Setup isolated test environment
	// =================================================================================================
	db := setupTestDB(t)
	h := NewExploreHandler(db)
	ctx := context.Background()
	recipientID := "target_user"
	now := time.Now()

	// =================================================================================================
	// 2: Concurrency Mechanics Setup
	// =================================================================================================
	// Target PageSize is 2, creating a hardcoded fetchSize of 4 (PageSize * 2).
	// We inject 5 total outbound records to trigger memory-filtering depletion.

	// A. Generate 4 Mutual Matches (Users who liked me, and I liked them back)
	// Chronologically sorted from T+4 down to T+1 (Newest to oldest)
	for i := 1; i <= 4; i++ {
		actorID := fmt.Sprintf("Mutual_Liker_%d", i)

		// Inbound decision record: They liked me
		err := db.Create(&model.Decision{
			ActorUserID:     actorID,
			RecipientUserID: recipientID,
			LikedRecipient:  true,
			CreatedAt:       now.Add(time.Duration(i) * time.Minute),
		}).Error
		assert.NoError(t, err)

		// Outbound decision record: I liked them back (Simulating mutual match state)
		err = db.Create(&model.Decision{
			ActorUserID:     recipientID,
			RecipientUserID: actorID,
			LikedRecipient:  true,
			CreatedAt:       now.Add(time.Duration(i) * time.Minute),
		}).Error
		assert.NoError(t, err)
	}

	// B. Generate the 5th oldest record: The only valid target
	// (They liked me, but I have NOT liked them back). Created at T+0.
	validActorID := "Valid_Liker_A"
	err := db.Create(&model.Decision{
		ActorUserID:     validActorID,
		RecipientUserID: recipientID,
		LikedRecipient:  true,
		CreatedAt:       now,
	}).Error
	assert.NoError(t, err)

	// Define page size as a local variable to safely acquire its pointer reference.
	pageSizeVal := uint32(2)

	// =================================================================================================
	// 3. Execution & Structural Evaluation
	// =================================================================================================
	req := &pb.ListLikedYouRequest{
		RecipientUserId: recipientID,
		PageSize:        &pageSizeVal, // fetchSize inside code will evaluate to 2 * 2 = 4
	}

	resp, err := h.ListNewLikedYou(ctx, req)
	assert.NoError(t, err)

	// =================================================================================================
	// Step 4: Engineering Analysis & Assertions
	// =================================================================================================
	// CRITICAL SYSTEM ANALYSIS:
	// Due to the hard LIMIT(4) clause enforced during Step 1's query execution,
	// the database scanner only loads the 4 newest mutual-match records.
	// They are fully stripped out during Step 3's high-speed hash map filter.
	//
	// The valid 5th record ("Valid_Liker_A") is left untouched in the database,
	// causing an empty page result while returning a NextPaginationToken.
	t.Logf("[Test Audit] Response Likers Array Length: %d", len(resp.GetLikers()))

	token := resp.GetNextPaginationToken()
	if token != "" {
		t.Logf("[Test Audit] Next Pagination Token Generated: %s", token)
	}

	if len(resp.GetLikers()) == 0 {
		t.Errorf("FAIL: The response list is empty. Truncation bug reproduced!")
		return
	}
	// This assertion highlights the pagination truncation issue on your original code layout.
	assert.NotEmpty(t, resp.GetLikers(), "The response list must not return empty when older eligible records are present in the datastore.")
	assert.Equal(t, validActorID, resp.GetLikers()[0].ActorId, "The processor failed to fetch and append the valid candidate record.")

}

// =====================================================================================================
// CASE H: Put Decision Concurrency Deflection Success Test
// =====================================================================================================
// This test executes a heavy concurrent stress test to verify that
// the node-level semaphore strictly caps active database operations at 50,
// deflecting overflow traffic gracefully to stabilize the 6GB RAM profile.
func TestPutDecision_ConcurrencyDeflection_Success(t *testing.T) {
	// =================================================================================================
	// 1. Setup isolated test environment
	// =================================================================================================
	db := setupTestDB(t)
	h := NewExploreHandler(db)

	// =================================================================================================
	// 2: Concurrency Mechanics Setup
	// =================================================================================================
	// We deploy 100 simultaneous requests. Since maxConcurrentRequests is hard-coded
	// to a channel size of 50, exactly 50 should pass, and 50 should be blocked/rejected.
	totalConcurrentRequests := 100
	var wg sync.WaitGroup
	wg.Add(totalConcurrentRequests)

	// Thread-safe counters to track architectural output states
	var structuralSuccessCount int32
	var loadSheddingDeflectedCount int32
	var otherErrorCount int32

	var counterMutex sync.Mutex

	// =================================================================================================
	// 3. Executing the Concurrent Flood
	// =================================================================================================
	for i := 0; i < totalConcurrentRequests; i++ {
		go func(index int) {
			defer wg.Done()

			// Formulate completely distinct decision records to avoid unique constraint violations
			req := &pb.DecisionRequest{
				ActorUserId:     fmt.Sprintf("actor_%d", index),
				RecipientUserId: fmt.Sprintf("recipient_%d", index),
				LikedRecipient:  true,
			}

			// Execute gRPC call directly against the rate-limiting handler logic
			_, err := h.PutDecision(context.Background(), req)

			// Thread-safe state collection phase
			counterMutex.Lock()
			defer counterMutex.Unlock()

			if err == nil {
				structuralSuccessCount++
			} else {
				st, ok := status.FromError(err)
				if ok && st.Code() == codes.ResourceExhausted {
					loadSheddingDeflectedCount++
				} else {
					t.Logf("[Unexpected Failure] Received unexpected error code: %v - %s", st.Code(), st.Message())
					otherErrorCount++
				}
			}
		}(i)
	}

	// Wait for all 100 concurrent Goroutines to exit execution lifecycles
	wg.Wait()

	// ==============================================================================
	// 4: Architectural Assertions
	// ==============================================================================
	t.Logf("[Stress Test Audit] Executed Requests: %d", totalConcurrentRequests)
	t.Logf("[Stress Test Audit] Successfully Processed (Entered DB Slot): %d", structuralSuccessCount)
	t.Logf("[Stress Test Audit] Shedded/Deflected via Semaphore Limit: %d", loadSheddingDeflectedCount)

	// CRITICAL ENGINEERING INVARIANT VERIFICATIONS:
	// 1. Zero unknown internal exceptions must surface during massive load ingestion.
	assert.Equal(t, int32(0), otherErrorCount, "The service must not emit unhandled backend failures during high concurrency.")

	// 2. The sum of accepted entries and rejected items must exactly equal the injected load.
	assert.Equal(t, int32(totalConcurrentRequests), structuralSuccessCount+loadSheddingDeflectedCount,
		"Total processed and shed requests must match total concurrent input.")

	// 3. Since the internal channel buffer is exactly 50, the success metrics should strictly equal 50.
	assert.Equal(t, int32(50), structuralSuccessCount,
		"The in-memory throttling wall failed to lock processing boundaries exactly at 50 concurrent requests.")

	// 4. Correspondingly, exactly 50 requests must be rejected to guarantee 6GB RAM pod stability.
	assert.Equal(t, int32(50), loadSheddingDeflectedCount,
		"The shedding architecture failed to deflect the exact delta of over-capacity requests.")
}
