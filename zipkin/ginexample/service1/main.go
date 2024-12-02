package main

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/openzipkin/zipkin-go"
	zipkinhttp "github.com/openzipkin/zipkin-go/middleware/http"
	"github.com/openzipkin/zipkin-go/propagation/b3"
	httpreporter "github.com/openzipkin/zipkin-go/reporter/http"
	"github.com/redis/go-redis/v9"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	tracer      *zipkin.Tracer
	redisClient *redis.Client
	httpClient  *zipkinhttp.Client
)

func main() {
	createTracer()
	createRedisClient()
	createHttpClient()
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

		span, ctx := tracer.StartSpanFromContext(c.Request.Context(), "kv.set")
		defer span.Finish()
		span.Tag("key", key)
		span.Tag("value", value)

		params := url.Values{
			"key":   {key},
			"value": {value},
		}
		req, err := http.NewRequest("POST", "http://localhost:8081/kv/put", strings.NewReader(params.Encode()))
		if err != nil {
			c.JSON(500, gin.H{
				"message": err.Error(),
			})
			return
		}

		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req = req.WithContext(ctx)
		resp, err := httpClient.DoWithAppSpan(req, "service2")
		if err != nil {
			c.JSON(500, gin.H{
				"message": err.Error(),
			})
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

		span, ctx := tracer.StartSpanFromContext(c.Request.Context(), "kv.get")
		defer span.Finish()
		span.Tag("key", key)

		params := url.Values{
			"key": {key},
		}

		req, err := http.NewRequest("GET", "http://localhost:8081/kv/get?"+params.Encode(), nil)
		if err != nil {
			c.JSON(500, gin.H{
				"message": err.Error(),
			})
			return
		}

		req = req.WithContext(ctx)

		resp, err := httpClient.DoWithAppSpan(req, "service2")
		if err != nil {
			c.JSON(500, gin.H{
				"message": err.Error(),
			})
			return
		}

		defer resp.Body.Close()
		c.Status(resp.StatusCode)
		io.Copy(c.Writer, resp.Body)
	})
	log.Fatal(r.Run(":8082"))
}

func createHttpClient() {
	var err error
	httpClient, err = zipkinhttp.NewClient(tracer, zipkinhttp.ClientTrace(true))
	if err != nil {
		log.Fatalf("unable to create http client: %+v\n", err)
	}
}

func zipkinMiddleware() func(c *gin.Context) {
	return func(c *gin.Context) {
		// get parent context using b3
		spanContext := tracer.Extract(b3.ExtractHTTP(c.Request))
		// start a new span
		request := fmt.Sprintf("%s %s", c.Request.Method, c.Request.URL.Path)
		span, ctx := tracer.StartSpanFromContext(c.Request.Context(), request, zipkin.Parent(spanContext))
		defer span.Finish()
		// set the new context
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
	endpoint, err := zipkin.NewEndpoint("service1", "localhost:8082")
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
