package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"

	"my_feed_system/internal/cachex"
	"my_feed_system/internal/config"
	"my_feed_system/internal/db"
	"my_feed_system/internal/feed"
	httpserver "my_feed_system/internal/http"
	"my_feed_system/internal/mq"
	"my_feed_system/internal/observability"
	"my_feed_system/internal/outbox"
	"my_feed_system/internal/popularity"
	"my_feed_system/internal/video"
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

	//视频详细缓存
	localDetailStore, err := cachex.NewBytesCache(observability.CacheVideoDetail, 32<<20)
	if err != nil {
		log.Fatalf("create local detail cache failed: %v", err)
	}
	defer localDetailStore.Close()
	localDetailCache := video.NewLocalDetailCache(localDetailStore)

	//最新视频缓存
	// latest/hot 页只缓存热点结果，容量可以明显小于 detail L1。
	localLatestStore, err := cachex.NewBytesCache(observability.CacheFeedLatest, 16<<20)
	if err != nil {
		log.Fatalf("create local latest feed cache failed: %v", err)
	}
	defer localLatestStore.Close()
	localLatestCache := feed.NewLocalLatestPageCache(localLatestStore)

	//热榜视频页缓存
	localHotStore, err := cachex.NewBytesCache(observability.CacheFeedHot, 16<<20)
	if err != nil {
		log.Fatalf("create local hot feed cache failed: %v", err)
	}
	defer localHotStore.Close()
	localHotCache := feed.NewLocalHotPageCache(localHotStore)

	var rabbitConn *amqp.Connection
	if conn, err := mq.Dial(cfg.RabbitMQ); err != nil {
		log.Printf("connect rabbitmq failed, API will continue in degraded mode and outbox will retry later: %v", err)
	} else {
		rabbitConn = conn
		if err := mq.DeclareTopology(rabbitConn); err != nil {
			log.Printf("declare rabbitmq topology failed, outbox will retry later: %v", err)
		}
		defer func() {
			if closeErr := rabbitConn.Close(); closeErr != nil {
				log.Printf("close rabbitmq failed: %v", closeErr)
			}
		}()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := observability.StartPprof(ctx, "api", cfg.Pprof.API); err != nil {
		log.Fatalf("start api pprof failed: %v", err)
	}

	publisher := mq.NewResilientPublisher(cfg.RabbitMQ)
	//视频已经发出redis未成功写入
	go outbox.NewPoller(outbox.NewRepo(database), publisher).Run(ctx)

	router := httpserver.NewRouterWithLocalCaches(
		database,
		redisCmd,
		popularityService,
		publisher,
		localDetailCache,
		localLatestCache,
		localHotCache,
		cfg.JWT.Secret,
		cfg.Upload.Dir,
	)
	if rabbitConn != nil {
		//L1缓存失效的处理
		detailInvalidationConsumer := video.NewDetailInvalidationConsumer(localDetailCache)
		latestInvalidationConsumer := feed.NewLatestInvalidationConsumer(localLatestCache)
		consumerTagPrefix := strings.TrimSpace(cfg.RabbitMQ.ConsumerTag)
		if consumerTagPrefix == "" {
			consumerTagPrefix = "feed-api"
		}
		go func() {
			tag := fmt.Sprintf("%s-cache-invalidator", consumerTagPrefix)
			log.Printf("cache invalidation consumer started: exchange=%s tag=%s", mq.ExchangeCacheInvalidated, tag)
			handle := func(ctx context.Context, event mq.Envelope) error {
				// 同一个 fanout 通道上按 cache name 分发到各自的本地失效处理器。
				if err := detailInvalidationConsumer.Handle(ctx, event); err != nil {
					return err
				}
				return latestInvalidationConsumer.Handle(ctx, event)
			}
			//消费广播消息
			if err := mq.ConsumeEphemeralFanout(ctx, rabbitConn, mq.ExchangeCacheInvalidated, tag, cfg.RabbitMQ.PrefetchCount, handle); err != nil && ctx.Err() == nil {
				log.Printf("cache invalidation consumer stopped: %v", err)
			}
		}()
	}

	//服务的启动和退出
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
