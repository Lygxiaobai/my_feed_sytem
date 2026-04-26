package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/redis/go-redis/v9"

	"my_feed_system/internal/config"
	"my_feed_system/internal/db"
	"my_feed_system/internal/feed"
	"my_feed_system/internal/mq"
	"my_feed_system/internal/observability"
	"my_feed_system/internal/popularity"
	"my_feed_system/internal/video"
	workerpkg "my_feed_system/internal/worker"
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
		log.Printf("connect redis failed, popularity updates will be skipped: %v", err)
	} else {
		defer func() {
			if closeErr := redisClient.Close(); closeErr != nil {
				log.Printf("close redis failed: %v", closeErr)
			}
		}()
	}

	var popularityService *popularity.Service
	if redisClient != nil {
		popularityService = popularity.NewService(redisClient)
	}

	var redisCmd redis.Cmdable
	if redisClient != nil {
		redisCmd = redisClient
	}

	detailCache := video.NewDetailCache(redisCmd)
	latestCache := feed.NewLatestCache(redisCmd)
	timelineStore := feed.NewGlobalTimelineStore(redisCmd)

	rabbitConn, err := mq.Dial(cfg.RabbitMQ)
	if err != nil {
		log.Fatalf("connect rabbitmq failed: %v", err)
	}
	defer func() {
		if closeErr := rabbitConn.Close(); closeErr != nil {
			log.Printf("close rabbitmq failed: %v", closeErr)
		}
	}()

	if err := mq.DeclareTopology(rabbitConn); err != nil {
		log.Fatalf("declare rabbitmq topology failed: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := observability.StartPprof(ctx, "worker", cfg.Pprof.Worker); err != nil {
		log.Fatalf("start worker pprof failed: %v", err)
	}

	publisher := mq.NewPublisher(rabbitConn)
	likeWorker := workerpkg.NewLikeWorker(database, publisher, detailCache)
	commentWorker := workerpkg.NewCommentWorker(database, publisher, detailCache)
	socialWorker := workerpkg.NewSocialWorker(database)
	popularityWorker := workerpkg.NewPopularityWorker(database, popularityService, detailCache)
	timelineConsumer := workerpkg.NewTimelineConsumer(timelineStore, latestCache, publisher)
	popularityProjectionPoller := popularity.NewProjectionPoller(popularity.NewProjectionRepo(database), popularityService)

	consumerTagPrefix := strings.TrimSpace(cfg.RabbitMQ.ConsumerTag)
	if consumerTagPrefix == "" {
		consumerTagPrefix = "feed-worker"
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 5)

	start := func(queue string, suffix string, handler mq.HandlerFunc) {
		wg.Add(1)
		go func() {
			defer wg.Done()

			tag := fmt.Sprintf("%s-%s", consumerTagPrefix, suffix)
			consumer := mq.NewConsumer(rabbitConn, queue, tag, cfg.RabbitMQ.PrefetchCount, handler)
			log.Printf("consumer started: queue=%s tag=%s", queue, tag)
			if err := consumer.Run(ctx); err != nil && ctx.Err() == nil {
				errCh <- fmt.Errorf("run consumer queue=%s: %w", queue, err)
			}
		}()
	}

	start(mq.QueueLikeWrite, "like", likeWorker.Handle)
	start(mq.QueueCommentWrite, "comment", commentWorker.Handle)
	start(mq.QueueSocialWrite, "social", socialWorker.Handle)
	start(mq.QueuePopularityUpdate, "popularity", popularityWorker.Handle)
	start(mq.QueueTimelineUpdate, "timeline", timelineConsumer.Handle)

	if popularityService != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Printf("popularity projection poller started")
			popularityProjectionPoller.Run(ctx)
		}()
	}

	select {
	case <-ctx.Done():
		log.Printf("worker shutting down")
	case runErr := <-errCh:
		log.Printf("worker stopped by consumer error: %v", runErr)
		stop()
	}

	wg.Wait()
	log.Printf("worker exited")
}
