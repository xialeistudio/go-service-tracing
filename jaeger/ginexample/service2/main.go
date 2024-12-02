package main

import (
	"context"
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
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

func main() {
	shutdown := initTracer()
	defer shutdown()

	createRedisClient()

	r := gin.Default()
	r.Use(otelgin.Middleware("service2"))

	r.POST("/kv/put", func(c *gin.Context) {
		key := c.PostForm("key")
		value := c.PostForm("value")
		if key == "" || value == "" {
			c.JSON(400, gin.H{
				"message": "key or value is empty",
			})
			return
		}
		tracer := otel.Tracer("service2")

		ctx, span := tracer.Start(c.Request.Context(), "redis.set")
		defer span.End()

		if err := redisClient.Set(ctx, key, value, time.Minute).Err(); err != nil {
			c.JSON(500, gin.H{"message": err.Error()})
			return
		}

		c.JSON(200, gin.H{"message": "success"})
	})

	r.GET("/kv/get", func(c *gin.Context) {
		key := c.Query("key")
		if key == "" {
			c.JSON(400, gin.H{
				"message": "key is empty",
			})
			return
		}
		tracer := otel.Tracer("service2")

		ctx, span := tracer.Start(c.Request.Context(), "redis.get")
		defer span.End()

		value, err := redisClient.Get(ctx, key).Result()
		if err != nil {
			c.JSON(500, gin.H{"message": err.Error()})
			return
		}

		c.JSON(200, gin.H{"value": value})
	})
	log.Printf("service2 running on port 8081")
	log.Fatal(r.Run(":8081"))
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
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("service2"),
			semconv.ServiceVersion("1.0.0"),
		),
	)
	if err != nil {
		panic(err)
	}

	conn, err := grpc.Dial("127.0.0.1:4317",
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
		ctx := context.Background()
		if err := tracerProvider.Shutdown(ctx); err != nil {
			panic(err)
		}
	}
}
