package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"my_feed_system/internal/config"
	"my_feed_system/internal/db"
	httpserver "my_feed_system/internal/http"
	"my_feed_system/internal/mq"
	"my_feed_system/internal/observability"
	"my_feed_system/internal/outbox"
	"my_feed_system/internal/popularity"
)

const serverShutdownTimeout = 5 * time.Second

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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := observability.StartPprof(ctx, "api", cfg.Pprof.API); err != nil {
		log.Fatalf("start api pprof failed: %v", err)
	}

	publisher := mq.NewResilientPublisher(cfg.RabbitMQ)
	go outbox.NewPoller(outbox.NewRepo(database), publisher).Run(ctx)

	router := httpserver.NewRouter(database, redisCmd, popularityService, publisher, cfg.JWT.Secret, cfg.Upload.Dir)
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	server := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("server started at %s", addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("run server failed: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		log.Printf("server shutting down")
	case err := <-errCh:
		log.Printf("%v", err)
		stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("shutdown server failed: %v", err)
	}
}
