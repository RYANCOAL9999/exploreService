# Explore Service

A high-performance gRPC service built in Go to manage user matchmaking decisions (Likes and Passes). The service is fully containerized, performance-optimized for users with high volume data, and includes a comprehensive test suite.

---

## Technical Architecture & Design Decisions

- **Layered Architecture**: The project follows a strict separation of concerns. gRPC endpoints are handled in `internal/handler/`, database access and logic reside in `internal/database/`, and database schemas are defined in `internal/model/`.
- **Dependency Injection**: The GORM database connection instance is initialized in `main.go` and explicitly passed into the gRPC handler. No global database variables are used, making the code highly testable.
- **KISS Principle**: No over-engineered external caching (e.g., Redis) or message queues are introduced. High-throughput scaling is achieved through optimized SQL design and proper indexing on a single PostgreSQL instance.

---

## Scalability Considerations

The service is designed to handle users with hundreds of thousands of decisions without degradation:

- **Pagination queries** use a compound cursor `(created_at, id)` rather than `LIMIT/OFFSET`, so query cost remains constant regardless of how deep into the result set a user is paginating.
- **CountLikedYou** leverages the composite index directly, avoiding full table scans even for users with large decision histories.
- **Known bottleneck**: `ListNewLikedYou` performs a LEFT JOIN to exclude mutual matches. For recipients with an extremely high volume of likes (e.g. 500k+), this join may become expensive. At that scale, a pre-computed matches table or a caching layer (e.g. Redis sets) would be the natural next step — but this is intentionally out of scope for this exercise per the KISS principle.

---

## Project Setup & Running

### 1. Docker Environment (Recommended)

The entire environment (Go gRPC Service + PostgreSQL) can be spun up with one command. The Go container automatically waits for PostgreSQL to pass its health check before starting, then executes the auto-migration to prepare the database schema.

```bash
docker-compose up --build
```

The service will start listening on port 50051.

### 2. Local Environment

If you prefer running the binary natively on your host machine:

1. Copy the environment variable template:

```bash
cp .env.example .env
set -a
source .env
set +a
```

2. Adjust the connection variables inside .env to match your local database settings.

3. Start the application:

```bash
go run main.go
```

## Running Tests

The test suite validates complex business logic and edge cases. Tests execute within milliseconds using isolated in-memory SQLite databases, each uniquely named per test via `t.Name()` to prevent state leakage between parallel test runs.

To run all unit tests with verbose output:

```bash
go test ./internal/handler/... -v
```

## Core Test Cases Covered:

Decision Overwriting: Ensures that a second decision from the same actor to the same recipient successfully overwrites the existing state instead of throwing constraint errors or creating duplicate records.

Mutual Like Detection: Confirms that when two users both like each other, MutualLikes evaluates to true inside the gRPC response instantly.

Cursor Pagination Boundary: Verifies that multi-page token parsing returns records sequentially by time (newest first) and safely yields a nil token on the final page.

Mutual Match Exclusion Filter: Validates that ListNewLikedYou accurately outputs only unprocessed likes while excluding users who have already achieved a mutual match.

Count Isolation: Checks that CountLikedYou returns precise calculations even when mixed with unrelated database rows, such as pass decisions or likes for different recipients.

## Manual API Verification

Once the container or local application is running on port 50051, you can interact with the gRPC service using grpcurl or Postman:

### 1. Submit a Decision (Like)

```bash
grpcurl -plaintext -d '{"actor_user_id": "alice", "recipient_user_id": "bob", "liked_recipient": true}' localhost:50051 pb.ExploreService/PutDecision
```

### 2. List Users Who Liked a Recipient

```bash
grpcurl -plaintext -d '{"recipient_user_id": "bob", "page_size": 2}' localhost:50051 pb.ExploreService/ListLikedYou
```
