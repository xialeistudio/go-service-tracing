package main

import (
	"context"
	"fmt"
	"github.com/openzipkin/zipkin-go"
	zikpingrpc "github.com/openzipkin/zipkin-go/middleware/grpc"
	httpreporter "github.com/openzipkin/zipkin-go/reporter/http"
	"github.com/redis/go-redis/v9"
	"go-service-tracing/zipkin/grpcexample/service2/service2"
	"google.golang.org/grpc"
	"log"
	"net"
	"time"
)

var (
	tracer      *zipkin.Tracer
	redisClient *redis.Client
)

type server struct {
	service2.UnimplementedStorageServer
}

func (s server) Put(ctx context.Context, req *service2.PutRequest) (*service2.PutResponse, error) {
	span, ctx := tracer.StartSpanFromContext(ctx, "redis.set")
	defer span.Finish()

	span.Tag("redis.key", req.Key)
	span.Tag("redis.value", req.Value)

	if err := redisClient.Set(ctx, req.Key, req.Value, time.Minute).Err(); err != nil {
		return nil, fmt.Errorf("redis set error: %+v", err)
	}

	return &service2.PutResponse{}, nil
}

func (s server) Get(ctx context.Context, req *service2.GetRequest) (*service2.GetResponse, error) {
	span, ctx := tracer.StartSpanFromContext(ctx, "redis.get")
	defer span.Finish()

	span.Tag("redis.key", req.Key)

	value, err := redisClient.Get(ctx, req.Key).Result()
	if err != nil {
		return nil, fmt.Errorf("redis get error: %+v", err)
	}

	return &service2.GetResponse{Value: value}, nil
}

func main() {
	createTracer()
	createRedisClient()

	listener, err := net.Listen("tcp", ":8082")
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	sh := zikpingrpc.NewServerHandler(tracer)
	s := grpc.NewServer(grpc.StatsHandler(sh))
	service2.RegisterStorageServer(s, &server{})

	log.Printf("server listening at %v", listener.Addr())
	if err := s.Serve(listener); err != nil {
		panic(err)
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

func createTracer() {
	reporter := httpreporter.NewReporter("http://localhost:9411/api/v2/spans", httpreporter.Timeout(time.Second*5))
	// 初始化endpoint
	endpoint, err := zipkin.NewEndpoint("service2", "localhost:8082")
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
