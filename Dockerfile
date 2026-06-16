# Stage 1: Build the Go binary
FROM golang:1.22-alpine AS builder

# Install git and build tools if required
RUN apk add --no-cache git

# Set working directory inside the container
WORKDIR /app

# Copy go mod files first to leverage Docker cache layers
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire source tree
COPY . .

# Build the highly optimized Go binary for alpine execution
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o explore-service main.go

# Stage 2: Create the final minimal production image
FROM alpine:3.19

WORKDIR /app

# Copy the pre-compiled binary from the builder stage
COPY --from=builder /app/explore-service .

# 💡 Added: Default environment variables for defense.
# If they run "docker run" without any parameters, these values will be used.
ENV PORT=50051
ENV DB="host=postgres user=postgres password=mysecretpassword dbname=explore_db port=5432 sslmode=disable TimeZone=UTC" 

# Expose the gRPC port
EXPOSE 50051

# Run the service
CMD ["./explore-service"]