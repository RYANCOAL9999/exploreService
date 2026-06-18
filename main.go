package main

import (
	"exploreService/internal/database"
	"exploreService/internal/handler"
	"exploreService/pb"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

const (
	serviceName    = "exploreService"
	serviceVersion = "0.1.0"
	defaultPort    = "50051"
)

func main() {

	log.Printf("Starting %s v%s...", serviceName, serviceVersion)

	// Initialize the PostgreSQL connection pool.
	db := database.NewPostgresConnection()

	// Ensure the database connection is closed when the application exits.
	defer database.ClosePostgresConnection(db)

	// Determine the port from environment variables or fallback to default.
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}
	address := fmt.Sprintf(":%s", port)

	lis, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", address, err)
	}

	// Initialize the gRPC server.
	grpcServer := grpc.NewServer()

	// Instantiated gRPC Official Health Check Service
	healthServer := health.NewServer()

	// ➕ Register the health check service to your gRPC server.
	healthpb.RegisterHealthServer(grpcServer, healthServer)

	// Instantiate the handler using the new constructor
	serverHandler := handler.NewExploreHandler(db)

	// Register the handler to the gRPC server.
	pb.RegisterExploreServiceServer(grpcServer, serverHandler)

	// ➕ Set initial health status
	// "" represents the global state, while "exploreService" represents the state of a specific service.
	// When the program reaches this point, it means the database connection has been successfully established and the Handler has been initialized, allowing it to safely receive traffic.
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("exploreService", healthpb.HealthCheckResponse_SERVING)

	go func() {
		log.Printf("🚀 gRPC Server is listening on %s", address)
		if err := grpcServer.Serve(lis); err != nil {
			log.Printf("gRPC server stopped serving: %v", err)
		}
	}()

	// Graceful shutdown logic...
	shutdownChannel := make(chan os.Signal, 1)
	signal.Notify(shutdownChannel, syscall.SIGINT, syscall.SIGTERM)

	sig := <-shutdownChannel
	log.Printf("Received shutdown signal: %v. Initiating graceful shutdown...", sig)

	// Tell K8S: I'm about to shut down, stop sending me data.
	log.Println("Changing health status to NOT_SERVING...")
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus("exploreService", healthpb.HealthCheckResponse_NOT_SERVING)

	shutdownTimeout := 5 * time.Second
	done := make(chan struct{})

	go func() {
		grpcServer.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		log.Println("gRPC server stopped gracefully.")
	case <-time.After(shutdownTimeout):
		log.Println("Graceful shutdown timed out. Forcing hard stop...")
		grpcServer.Stop()
	}

	log.Println("Service execution finished.")

}
