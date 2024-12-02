package main

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/openzipkin/zipkin-go"
	"github.com/openzipkin/zipkin-go/propagation/b3"
	httpreporter "github.com/openzipkin/zipkin-go/reporter/http"
	"github.com/redis/go-redis/v9"
	"log"
	"strconv"
	"time"
)

var (
	tracer      *zipkin.Tracer
	redisClient *redis.Client
)

func main() {
	createTracer()
	createRedisClient()
	r := gin.Default()
	r.Use(zipkinMiddleware())
	r.POST("/kv/put", func(c *gin.Context) {
		key := c.PostForm("key")
		value := c.PostForm("value")
		if key == "" || value == "" {
			c.JSON(400, gin.H{
				"message": "key or value is empty",
			})
			return
		}

		span, ctx := tracer.StartSpanFromContext(c.Request.Context(), "redis.set")
		defer span.Finish()
		span.Tag("redis.key", key)
		span.Tag("redis.value", value)

		if err := redisClient.Set(ctx, key, value, time.Minute).Err(); err != nil {
			c.JSON(500, gin.H{
				"message": err.Error(),
			})
			return
		}

		c.JSON(200, gin.H{
			"message": "success",
		})
	})
	r.GET("/kv/get", func(c *gin.Context) {
		key := c.Query("key")
		if key == "" {
			c.JSON(400, gin.H{
				"message": "key is empty",
			})
			return
		}
		span, ctx := tracer.StartSpanFromContext(c.Request.Context(), "redis.get")
		defer span.Finish()
		span.Tag("redis.key", key)

		value, err := redisClient.Get(ctx, key).Result()
		if err != nil {
			c.JSON(500, gin.H{
				"message": err.Error(),
			})
			return
		}
		c.JSON(200, gin.H{
			"value": value,
		})
	})
	log.Fatal(r.Run(":8081"))
}

func zipkinMiddleware() func(c *gin.Context) {
	return func(c *gin.Context) {
		// 使用b3从请求头获取父级span(如果有的话)
		spanContext := tracer.Extract(b3.ExtractHTTP(c.Request))
		// 启动本次请求的根span
		request := fmt.Sprintf("%s %s", c.Request.Method, c.Request.URL.Path)
		span, ctx := tracer.StartSpanFromContext(c.Request.Context(), request, zipkin.Parent(spanContext))
		defer span.Finish()
		c.Request = c.Request.WithContext(ctx)

		span.Tag("http.method", c.Request.Method)
		span.Tag("http.url", c.Request.URL.Path)
		c.Next()
		span.Tag("http.status_code", strconv.Itoa(c.Writer.Status()))
		span.Tag("http.response_size", strconv.Itoa(c.Writer.Size()))
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
	endpoint, err := zipkin.NewEndpoint("service2", "localhost:8081")
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
