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

	// Instantiate the handler using the new constructor
	serverHandler := handler.NewExploreHandler(db)

	// Register the handler to the gRPC server.
	pb.RegisterExploreServiceServer(grpcServer, serverHandler)

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
