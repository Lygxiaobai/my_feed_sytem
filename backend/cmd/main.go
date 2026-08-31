package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	"my_feed_system/internal/logging"
	"my_feed_system/internal/mq"
	"my_feed_system/internal/observability"
	"my_feed_system/internal/outbox"
	"my_feed_system/internal/popularity"
	"my_feed_system/internal/storage"
	"my_feed_system/internal/video"
	"my_feed_system/internal/wallet"
)

const serverShutdownTimeout = 5 * time.Second

// fatal 记录致命错误后退出。
// slog 刻意没有提供 Fatal 级别；启动期依赖不可用时必须让进程立即退出，
// 由编排层重启，因此这里显式 os.Exit(1)。
func fatal(msg string, err error) {
	slog.Error(msg, slog.String("error", err.Error()))
	os.Exit(1)
}

func main() {
	// 先用环境变量把日志跑起来，保证「配置加载失败」本身也有结构化日志可查。
	logging.Setup(os.Getenv("LOG_LEVEL"), os.Getenv("LOG_FORMAT"), "api")

	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		fatal("load config failed", err)
	}
	// 配置就绪后按配置重建；环境变量优先，便于线上临时调整级别而不改配置文件。
	logging.Setup(
		firstNonEmpty(os.Getenv("LOG_LEVEL"), cfg.Log.Level),
		firstNonEmpty(os.Getenv("LOG_FORMAT"), cfg.Log.Format),
		"api",
	)

	database, err := db.NewMySQL(cfg.Database)
	if err != nil {
		fatal("connect mysql failed", err)
	}

	var redisClient *redis.Client
	redisClient, err = db.NewRedis(cfg.Redis)
	if err != nil {
		slog.Warn("redis unavailable, falling back to MySQL-only mode", slog.String("error", err.Error()))
	} else {
		defer func() {
			if closeErr := redisClient.Close(); closeErr != nil {
				slog.Warn("close redis failed", slog.String("error", closeErr.Error()))
			}
		}()
		slog.Info("redis connected", slog.String("host", cfg.Redis.Host), slog.Int("port", cfg.Redis.Port))
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
		fatal("create local detail cache failed", err)
	}
	defer localDetailStore.Close()
	localDetailCache := video.NewLocalDetailCache(localDetailStore)

	//最新视频缓存
	// latest/hot 页只缓存热点结果，容量可以明显小于 detail L1。
	localLatestStore, err := cachex.NewBytesCache(observability.CacheFeedLatest, 16<<20)
	if err != nil {
		fatal("create local latest feed cache failed", err)
	}
	defer localLatestStore.Close()
	localLatestCache := feed.NewLocalLatestPageCache(localLatestStore)

	//热榜视频页缓存
	localHotStore, err := cachex.NewBytesCache(observability.CacheFeedHot, 16<<20)
	if err != nil {
		fatal("create local hot feed cache failed", err)
	}
	defer localHotStore.Close()
	localHotCache := feed.NewLocalHotPageCache(localHotStore)

	var rabbitConn *amqp.Connection
	if conn, err := mq.Dial(cfg.RabbitMQ); err != nil {
		slog.Warn("rabbitmq unavailable, running in degraded mode and outbox will retry later",
			slog.String("error", err.Error()))
	} else {
		rabbitConn = conn
		if err := mq.DeclareTopology(rabbitConn); err != nil {
			slog.Warn("declare rabbitmq topology failed, outbox will retry later",
				slog.String("error", err.Error()))
		}
		defer func() {
			if closeErr := rabbitConn.Close(); closeErr != nil {
				slog.Warn("close rabbitmq failed", slog.String("error", closeErr.Error()))
			}
		}()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := observability.StartPprof(ctx, "api", cfg.Pprof.API); err != nil {
		fatal("start api pprof failed", err)
	}

	publisher := mq.NewResilientPublisher(cfg.RabbitMQ)
	//视频已经发出redis未成功写入
	go outbox.NewPoller(outbox.NewRepo(database), publisher).Run(ctx)
	go wallet.NewExpirePoller(wallet.NewService(database)).Run(ctx)

	objectStore, err := storage.New(storage.Config{
		Endpoint:       cfg.Storage.Endpoint,
		PublicEndpoint: cfg.Storage.PublicEndpoint,
		AccessKey:      cfg.Storage.AccessKey,
		SecretKey:      cfg.Storage.SecretKey,
		UseSSL:         cfg.Storage.UseSSL,
		Region:         cfg.Storage.Region,
		BucketUploads:  cfg.Storage.BucketUploads,
		BucketMedia:    cfg.Storage.BucketMedia,
	})
	if err != nil {
		fatal("connect object storage failed", err)
	}

	router := httpserver.NewRouterWithLocalCaches(
		database,
		redisCmd,
		popularityService,
		publisher,
		localDetailCache,
		localLatestCache,
		localHotCache,
		cfg.JWT.Secret,
		objectStore,
		cfg.Upload.MaxVideoBytes,
		cfg.Audit,
		cfg.Auth,
		cfg.Ops,
		cfg.Stripe,
		cfg.Feed,
		cfg.Embedding,
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
			slog.Info("cache invalidation consumer started",
				slog.String("exchange", mq.ExchangeCacheInvalidated), slog.String("tag", tag))
			handle := func(ctx context.Context, event mq.Envelope) error {
				// 同一个 fanout 通道上按 cache name 分发到各自的本地失效处理器。
				if err := detailInvalidationConsumer.Handle(ctx, event); err != nil {
					return err
				}
				return latestInvalidationConsumer.Handle(ctx, event)
			}
			//消费广播消息
			if err := mq.ConsumeEphemeralFanout(ctx, rabbitConn, mq.ExchangeCacheInvalidated, tag, cfg.RabbitMQ.PrefetchCount, handle); err != nil && ctx.Err() == nil {
				slog.Error("cache invalidation consumer stopped", slog.String("error", err.Error()))
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
		slog.Info("server started", slog.String("addr", addr))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("run server failed: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		slog.Info("server shutting down")
	case err := <-errCh:
		slog.Error("server stopped unexpectedly", slog.String("error", err.Error()))
		stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fatal("shutdown server failed", err)
	}
}

// firstNonEmpty 返回第一个非空字符串，用于实现「环境变量优先于配置文件」。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
