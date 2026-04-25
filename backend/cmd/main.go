package main

import (
	"context"
	"fmt"
	"log"

	"github.com/redis/go-redis/v9"

	"my_feed_system/internal/config"
	"my_feed_system/internal/db"
	httpserver "my_feed_system/internal/http"
	"my_feed_system/internal/mq"
	"my_feed_system/internal/outbox"
	"my_feed_system/internal/popularity"
)

func main() {
	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}

	database, err := db.NewMySQL(cfg.Database)
	if err != nil {
		log.Fatalf("connect mysql failed: %v", err)
	}

	var redisClient *redis.Client
	redisClient, err = db.NewRedis(cfg.Redis)
	if err != nil {
		log.Printf("connect redis failed, fallback to MySQL-only mode: %v", err)
	} else {
		defer func() {
			if closeErr := redisClient.Close(); closeErr != nil {
				log.Printf("close redis failed: %v", closeErr)
			}
		}()
		log.Printf("redis connected: %s:%d", cfg.Redis.Host, cfg.Redis.Port)
	}

	var popularityService *popularity.Service
	if redisClient != nil {
		popularityService = popularity.NewService(redisClient)
	}

	var redisCmd redis.Cmdable
	if redisClient != nil {
		redisCmd = redisClient
	}

	if rabbitConn, err := mq.Dial(cfg.RabbitMQ); err != nil {
		log.Printf("connect rabbitmq failed, API will continue in degraded mode and outbox will retry later: %v", err)
	} else {
		if err := mq.DeclareTopology(rabbitConn); err != nil {
			log.Printf("declare rabbitmq topology failed, outbox will retry later: %v", err)
		}
		if closeErr := rabbitConn.Close(); closeErr != nil {
			log.Printf("close rabbitmq failed: %v", closeErr)
		}
	}

	publisher := mq.NewResilientPublisher(cfg.RabbitMQ)
	// API 进程内启动 Outbox Poller；即使启动时 MQ 不可用，后续恢复后也能继续补投。
	go outbox.NewPoller(outbox.NewRepo(database), publisher).Run(context.Background())

	router := httpserver.NewRouter(database, redisCmd, popularityService, publisher, cfg.JWT.Secret, cfg.Upload.Dir)
	addr := fmt.Sprintf(":%d", cfg.Server.Port)

	log.Printf("server started at %s", addr)
	if err := router.Run(addr); err != nil {
		log.Fatalf("run server failed: %v", err)
	}
}
