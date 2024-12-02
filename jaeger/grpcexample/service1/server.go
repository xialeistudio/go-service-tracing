package main

import (
	"context"
	"fmt"
	"log"
	"net"

	"go-service-tracing/jaeger/grpcexample/service1/service1"
	"go-service-tracing/jaeger/grpcexample/service2/service2"

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
	svc2Client service2.StorageClient
)

type server struct {
	service1.UnimplementedStorageServer
}

func (s *server) Put(ctx context.Context, req *service1.PutRequest) (*service1.PutResponse, error) {
	tracer := otel.Tracer("service1")
	ctx, span := tracer.Start(ctx, "service1.Put")
	defer span.End()

	_, err := svc2Client.Put(ctx, &service2.PutRequest{
		Key:   req.Key,
		Value: req.Value,
	})
	if err != nil {
		return nil, fmt.Errorf("service2 put error: %v", err)
	}

	return &service1.PutResponse{}, nil
}

func (s *server) Get(ctx context.Context, req *service1.GetRequest) (*service1.GetResponse, error) {
	tracer := otel.Tracer("service1")
	ctx, span := tracer.Start(ctx, "service1.Get")
	defer span.End()

	resp, err := svc2Client.Get(ctx, &service2.GetRequest{
		Key: req.Key,
	})
	if err != nil {
		return nil, fmt.Errorf("service2 get error: %v", err)
	}

	return &service1.GetResponse{
		Value: resp.Value,
	}, nil
}

func main() {
	shutdown := initTracer()
	defer shutdown()

	createService2Client()

	lis, err := net.Listen("tcp", ":8082")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer(
		grpc.UnaryInterceptor(otelgrpc.UnaryServerInterceptor()),
		grpc.StreamInterceptor(otelgrpc.StreamServerInterceptor()),
	)
	service1.RegisterStorageServer(s, &server{})

	log.Printf("server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func createService2Client() {
	conn, err := grpc.Dial("localhost:8081",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(otelgrpc.UnaryClientInterceptor()),
		grpc.WithStreamInterceptor(otelgrpc.StreamClientInterceptor()),
	)
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	svc2Client = service2.NewStorageClient(conn)
}

func initTracer() func() {
	ctx := context.Background()
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("service1"),
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
