package main

import (
	"context"
	"fmt"
	"log"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"tiket/internal/config"
	"tiket/internal/handler"
	"tiket/internal/queue"
	"tiket/internal/router"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatal("redis connection failed: ", err)
	}

	svc := queue.New(rdb, queue.Config{
		MaxActive:         cfg.MaxActive,
		Lease:             cfg.Lease,
		AverageSession:    cfg.AverageSession,
		PromotionInterval: cfg.PromotionInterval,
	})
	svc.StartPromoter(ctx)
	defer svc.StopPromoter()

	r := gin.Default()
	h := handler.New(svc)
	router.Setup(r, h)

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("queue api listening on %s", addr)
	log.Fatal(r.Run(addr))
}
