package main

import (
	"context"
	"fmt"
	"github.com/openzipkin/zipkin-go"
	zikpingrpc "github.com/openzipkin/zipkin-go/middleware/grpc"
	httpreporter "github.com/openzipkin/zipkin-go/reporter/http"
	"github.com/redis/go-redis/v9"
	"go-service-tracing/zipkin/grpcexample/service1/service1"
	"go-service-tracing/zipkin/grpcexample/service2/service2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"log"
	"net"
	"time"
)

var (
	tracer      *zipkin.Tracer
	redisClient *redis.Client
	svc2Client  service2.StorageClient
)

type server struct {
	service1.UnimplementedStorageServer
}

func (s server) Put(ctx context.Context, req *service1.PutRequest) (*service1.PutResponse, error) {
	_, err := svc2Client.Put(ctx, &service2.PutRequest{
		Key:   req.Key,
		Value: req.Value,
	})
	if err != nil {
		return nil, fmt.Errorf("service2 put error: %+v", err)
	}
	return &service1.PutResponse{}, nil
}

func (s server) Get(ctx context.Context, req *service1.GetRequest) (*service1.GetResponse, error) {
	resp, err := svc2Client.Get(ctx, &service2.GetRequest{Key: req.Key})
	if err != nil {
		return nil, fmt.Errorf("service2 get error: %+v", err)
	}
	return &service1.GetResponse{Value: resp.Value}, nil
}

func main() {
	createTracer()
	createRedisClient()
	createService2Client()

	listener, err := net.Listen("tcp", ":8081")
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	sh := zikpingrpc.NewServerHandler(tracer)
	s := grpc.NewServer(grpc.StatsHandler(sh))
	service1.RegisterStorageServer(s, &server{})

	log.Printf("server listening at %v", listener.Addr())
	if err := s.Serve(listener); err != nil {
		panic(err)
	}
}

func createService2Client() {
	sh := zikpingrpc.NewClientHandler(tracer)
	conn, err := grpc.NewClient("localhost:8082",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(sh),
	)
	if err != nil {
		panic(err)
	}
	svc2Client = service2.NewStorageClient(conn)
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

func createTracer() {
	reporter := httpreporter.NewReporter("http://localhost:9411/api/v2/spans", httpreporter.Timeout(time.Second*5))
	// 初始化endpoint
	endpoint, err := zipkin.NewEndpoint("service1", "localhost:8081")
	if err != nil {
		log.Fatalf("unable to create local endpoint: %+v\n", err)
	}
	sampler, err := zipkin.NewCountingSampler(1)
	if err != nil {
		log.Fatalf("unable to create sampler: %+v\n", err)
	}
	// 初始化tracer
	tracer, err = zipkin.NewTracer(reporter,
		zipkin.WithLocalEndpoint(endpoint),
		zipkin.WithSampler(sampler),
	)
	if err != nil {
		log.Fatalf("unable to create tracer: %+v\n", err)
	}
}
