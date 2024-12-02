package main

import (
	"context"
	"go-service-tracing/jaeger/grpcexample/service1/service1"
	"log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	conn, err := grpc.NewClient("localhost:8082", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	client := service1.NewStorageClient(conn)
	_, err = client.Put(context.Background(), &service1.PutRequest{
		Key:   "test",
		Value: "test2",
	})
	if err != nil {
		panic(err)
	}
	log.Printf("put success")
	resp, err := client.Get(context.Background(), &service1.GetRequest{Key: "test"})
	if err != nil {
		panic(err)
	}
	log.Printf("get success, value: %s", resp.Value)
}
