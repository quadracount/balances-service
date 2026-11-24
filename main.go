// balances-service: saldos, plan para saldar cuentas y pagos de QuadraCount.
package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	pb "github.com/quadracount/balances-service/gen/balancespb"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mongoClient, err := mongo.Connect(options.Client().ApplyURI(env("MONGO_URL", "mongodb://localhost:27017")))
	if err != nil {
		log.Fatalf("MongoDB: %v", err)
	}
	defer func() { _ = mongoClient.Disconnect(context.Background()) }()
	store := NewStore(mongoClient.Database(env("MONGO_DB", "balances_db")))

	go ConsumeEvents(ctx, env("RABBITMQ_URL", "amqp://localhost:5672"), store)

	addr := ":" + env("GRPC_PORT", "50052")
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("escuchando en %s: %v", addr, err)
	}
	grpcServer := grpc.NewServer()
	pb.RegisterBalancesServiceServer(grpcServer, &server{store: store})

	// grpc.health.v1, para los probes gRPC de Kubernetes
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(grpcServer, healthServer)

	go func() {
		<-ctx.Done()
		log.Print("deteniendo")
		grpcServer.GracefulStop()
	}()

	log.Printf("balances-service gRPC en %s", addr)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("gRPC: %v", err)
	}
}
