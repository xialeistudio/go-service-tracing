package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"go-service-tracing/jaeger/grpcexample/service2/service2"

	"github.com/redis/go-redis/v9"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	redisClient *redis.Client
)

type server struct {
	service2.UnimplementedStorageServer
}

func (s *server) Put(ctx context.Context, req *service2.PutRequest) (*service2.PutResponse, error) {
	tracer := otel.Tracer("service2")
	ctx, span := tracer.Start(ctx, "redis.set")
	defer span.End()

	err := redisClient.Set(ctx, req.Key, req.Value, time.Minute).Err()
	if err != nil {
		return nil, fmt.Errorf("redis set error: %v", err)
	}

	return &service2.PutResponse{}, nil
}

func (s *server) Get(ctx context.Context, req *service2.GetRequest) (*service2.GetResponse, error) {
	tracer := otel.Tracer("service2")
	ctx, span := tracer.Start(ctx, "redis.get")
	defer span.End()

	value, err := redisClient.Get(ctx, req.Key).Result()
	if err != nil {
		return nil, fmt.Errorf("redis get error: %v", err)
	}

	return &service2.GetResponse{
		Value: value,
	}, nil
}

func main() {
	shutdown := initTracer()
	defer shutdown()

	createRedisClient()

	lis, err := net.Listen("tcp", ":8081")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer(
		grpc.UnaryInterceptor(otelgrpc.UnaryServerInterceptor()),
		grpc.StreamInterceptor(otelgrpc.StreamServerInterceptor()),
	)
	service2.RegisterStorageServer(s, &server{})

	log.Printf("server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func createRedisClient() {
	redisClient = redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	err := redisClient.Ping(ctx).Err()
	if err != nil {
		log.Fatalf("redis ping error: %+v\n", err)
	}
}

func initTracer() func() {
	ctx := context.Background()
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("service2"),
			semconv.ServiceVersion("1.0.0"),
		),
	)
	if err != nil {
		panic(err)
	}

	conn, err := grpc.Dial("localhost:4317",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		panic(err)
	}

	traceExporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		panic(err)
	}

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tracerProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return func() {
		if err := tracerProvider.Shutdown(context.Background()); err != nil {
			panic(err)
		}
	}
}
