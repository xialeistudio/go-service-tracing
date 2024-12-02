package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	redisClient *redis.Client
	httpClient  *http.Client
)

func main() {
	shutdown := initTracer()
	defer shutdown()

	createRedisClient()
	createHttpClient()

	r := gin.Default()
	r.Use(otelgin.Middleware("service1"))

	r.POST("/kv/put", func(c *gin.Context) {
		key := c.PostForm("key")
		value := c.PostForm("value")
		if key == "" || value == "" {
			c.JSON(400, gin.H{
				"message": "key or value is empty",
			})
			return
		}
		tracer := otel.Tracer("service1")

		ctx, span := tracer.Start(c.Request.Context(), "kv.set")
		defer span.End()

		params := url.Values{
			"key":   {key},
			"value": {value},
		}
		req, err := http.NewRequestWithContext(ctx, "POST", "http://localhost:8081/kv/put",
			strings.NewReader(params.Encode()))
		if err != nil {
			c.JSON(500, gin.H{"message": err.Error()})
			return
		}

		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := httpClient.Do(req)
		if err != nil {
			c.JSON(500, gin.H{"message": err.Error()})
			return
		}

		defer resp.Body.Close()
		c.Status(resp.StatusCode)
		io.Copy(c.Writer, resp.Body)
	})

	r.GET("/kv/get", func(c *gin.Context) {
		key := c.Query("key")
		if key == "" {
			c.JSON(400, gin.H{
				"message": "key is empty",
			})
			return
		}

		tracer := otel.Tracer("service1")
		ctx, span := tracer.Start(c.Request.Context(), "kv.get")
		defer span.End()

		params := url.Values{
			"key": {key},
		}

		req, err := http.NewRequestWithContext(ctx, "GET",
			"http://localhost:8081/kv/get?"+params.Encode(), nil)
		if err != nil {
			c.JSON(500, gin.H{"message": err.Error()})
			return
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			c.JSON(500, gin.H{"message": err.Error()})
			return
		}

		defer resp.Body.Close()
		c.Status(resp.StatusCode)
		io.Copy(c.Writer, resp.Body)
	})
	log.Printf("service1 running on port 8082")
	log.Fatal(r.Run(":8082"))
}

func createHttpClient() {
	httpClient = &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
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
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("service1"),
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
