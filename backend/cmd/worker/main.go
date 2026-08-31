package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"my_feed_system/internal/account"
	"my_feed_system/internal/audit"
	"my_feed_system/internal/config"
	"my_feed_system/internal/db"
	"my_feed_system/internal/feed"
	"my_feed_system/internal/logging"
	"my_feed_system/internal/mq"
	"my_feed_system/internal/notification"
	"my_feed_system/internal/observability"
	"my_feed_system/internal/popularity"
	"my_feed_system/internal/recommend"
	"my_feed_system/internal/storage"
	"my_feed_system/internal/video"
	workerpkg "my_feed_system/internal/worker"
)

// fatal 记录致命错误后退出，理由同 API 侧：启动期依赖不可用应交由编排层重启。
func fatal(msg string, err error) {
	slog.Error(msg, slog.String("error", err.Error()))
	os.Exit(1)
}

func main() {
	// 先用环境变量把日志跑起来，保证「配置加载失败」本身也有结构化日志可查。
	logging.Setup(os.Getenv("LOG_LEVEL"), os.Getenv("LOG_FORMAT"), "worker")

	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		fatal("load config failed", err)
	}
	logging.Setup(
		firstNonEmpty(os.Getenv("LOG_LEVEL"), cfg.Log.Level),
		firstNonEmpty(os.Getenv("LOG_FORMAT"), cfg.Log.Format),
		"worker",
	)

	database, err := db.NewMySQL(cfg.Database)
	if err != nil {
		fatal("connect mysql failed", err)
	}

	var redisClient *redis.Client
	redisClient, err = db.NewRedis(cfg.Redis)
	if err != nil {
		slog.Warn("redis unavailable, popularity updates will be skipped", slog.String("error", err.Error()))
	} else {
		defer func() {
			if closeErr := redisClient.Close(); closeErr != nil {
				slog.Warn("close redis failed", slog.String("error", closeErr.Error()))
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

	fanoutCfg := cfg.Feed.Fanout
	fanoutCfg.ApplyDefaults()
	inboxStore := feed.NewInboxStore(redisCmd, fanoutCfg.InboxMaxSize, fanoutCfg.ActiveTTL())
	outboxStore := feed.NewOutboxStore(redisCmd, fanoutCfg.OutboxMaxSize)
	followingCache := feed.NewFollowingCache(redisCmd, 0)

	rabbitConn, err := mq.Dial(cfg.RabbitMQ)
	if err != nil {
		fatal("connect rabbitmq failed", err)
	}
	defer func() {
		if closeErr := rabbitConn.Close(); closeErr != nil {
			slog.Warn("close rabbitmq failed", slog.String("error", closeErr.Error()))
		}
	}()

	if err := mq.DeclareTopology(rabbitConn); err != nil {
		fatal("declare rabbitmq topology failed", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := observability.StartPprof(ctx, "worker", cfg.Pprof.Worker); err != nil {
		fatal("start worker pprof failed", err)
	}
	// Worker 没有业务路由，扩散等指标只能靠这个附属端口暴露出去。
	if err := observability.StartMetricsServer(ctx, "worker", cfg.Metrics.Worker); err != nil {
		fatal("start worker metrics server failed", err)
	}

	publisher := mq.NewPublisher(rabbitConn)
	notifyWriter := notification.NewWriter(database)
	likeWorker := workerpkg.NewLikeWorker(database, publisher, detailCache)
	commentWorker := workerpkg.NewCommentWorker(database, publisher, detailCache)
	commentWorker.SetNotifier(notifyWriter)
	commentWorker.SetInterestRecorder(account.NewService(database, cfg.JWT.Secret))
	socialWorker := workerpkg.NewSocialWorkerWithFanout(database, inboxStore, followingCache)
	socialWorker.SetNotifier(notifyWriter)
	popularityWorker := workerpkg.NewPopularityWorker(database, popularityService, detailCache)
	timelineConsumer := workerpkg.NewTimelineConsumer(timelineStore, latestCache, publisher)
	fanoutWorker := workerpkg.NewFanoutWorker(database, inboxStore, outboxStore, publisher, fanoutCfg)
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

	mediaWorker := workerpkg.NewMediaWorker(database, objectStore)
	cfg.Embedding.ApplyDefaults()
	embedWorker := workerpkg.NewEmbedWorker(database, recommend.NewHTTPEmbedder(cfg.Embedding))
	var auditWorker *workerpkg.AuditWorker
	if cfg.Audit.Enabled {
		auditService := audit.NewService(
			database,
			video.NewAuditStore(database),
			buildModerator(cfg.Audit),
			video.NewApprovalPublisher(database),
			cfg.Audit.ReviewerAccountIDs,
		)
		auditWorker = workerpkg.NewAuditWorker(auditService)
		slog.Info("content audit enabled")
	} else {
		slog.Info("content audit disabled, audit consumer will not start")
	}
	popularityProjectionPoller := popularity.NewProjectionPoller(popularity.NewProjectionRepo(database), popularityService)

	consumerTagPrefix := strings.TrimSpace(cfg.RabbitMQ.ConsumerTag)
	if consumerTagPrefix == "" {
		consumerTagPrefix = "feed-worker"
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 16)

	start := func(queue string, suffix string, handler mq.HandlerFunc, handleTimeout time.Duration) {
		wg.Add(1)
		go func() {
			defer wg.Done()

			tag := fmt.Sprintf("%s-%s", consumerTagPrefix, suffix)
			consumer := mq.NewConsumer(rabbitConn, queue, tag, cfg.RabbitMQ.PrefetchCount, handler)
			if handleTimeout > 0 {
				consumer.SetHandleTimeout(handleTimeout)
			}
			slog.Info("consumer started", slog.String("queue", queue), slog.String("tag", tag))
			if err := consumer.Run(ctx); err != nil && ctx.Err() == nil {
				errCh <- fmt.Errorf("run consumer queue=%s: %w", queue, err)
			}
		}()
	}

	start(mq.QueueLikeWrite, "like", likeWorker.Handle, 0)
	start(mq.QueueCommentWrite, "comment", commentWorker.Handle, 0)
	start(mq.QueueSocialWrite, "social", socialWorker.Handle, 0)
	start(mq.QueuePopularityUpdate, "popularity", popularityWorker.Handle, 0)
	start(mq.QueueTimelineUpdate, "timeline", timelineConsumer.Handle, 0)
	if redisClient != nil {
		start(mq.QueueTimelineFanout, "fanout", fanoutWorker.Handle, 0)
	} else {
		// 收件箱与发件箱都在 Redis 上，没有 Redis 时启动这个消费者只会把消息全打进死信队列。
		// 关注流此时由 API 侧自动退回 MySQL 读扩散，功能不受影响。
		slog.Warn("redis unavailable, following fanout consumer will not start")
	}
	// 转码要跑完整条视频，不能套写扩散那条 10 秒预算。
	start(mq.QueueMediaTranscode, "media", mediaWorker.Handle, 15*time.Minute)
	start(mq.QueueVideoEmbed, "embed", embedWorker.Handle, 0)
	go embedWorker.Backfill(ctx)
	if auditWorker != nil {
		start(mq.QueueAuditModerate, "audit", auditWorker.Handle, 0)
	}

	for _, spec := range mq.QueueSpecs() {
		spec := spec
		wg.Add(1)
		go func() {
			defer wg.Done()

			tag := fmt.Sprintf("%s-dlq-%s", consumerTagPrefix, spec.Queue)
			consumer := mq.NewDeadLetterConsumer(rabbitConn, database, spec.DLQ, tag, cfg.RabbitMQ.PrefetchCount)
			slog.Info("dlq consumer started",
				slog.String("queue", spec.DLQ), slog.String("source_queue", spec.Queue), slog.String("tag", tag))
			if err := consumer.Run(ctx); err != nil && ctx.Err() == nil {
				errCh <- fmt.Errorf("run dlq consumer queue=%s: %w", spec.DLQ, err)
			}
		}()
	}

	if popularityService != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slog.Info("popularity projection poller started")
			popularityProjectionPoller.Run(ctx)
		}()
	}

	select {
	case <-ctx.Done():
		slog.Info("worker shutting down")
	case runErr := <-errCh:
		slog.Error("worker stopped by consumer error", slog.String("error", runErr.Error()))
		stop()
	}

	wg.Wait()
	slog.Info("worker exited")
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

// buildModerator 与 API 侧保持同一套装配逻辑，保证机审规则两端一致。
func buildModerator(cfg config.AuditConfig) audit.Moderator {
	blockWords, err := audit.LoadWordFile(cfg.BlockWordFile)
	if err != nil {
		slog.Warn("load block word file failed, continuing without it",
			slog.String("path", cfg.BlockWordFile), slog.String("error", err.Error()))
	}
	reviewWords, err := audit.LoadWordFile(cfg.ReviewWordFile)
	if err != nil {
		slog.Warn("load review word file failed, continuing without it",
			slog.String("path", cfg.ReviewWordFile), slog.String("error", err.Error()))
	}

	slog.Info("content moderator ready",
		slog.Int("block_words", len(blockWords)),
		slog.Int("review_words", len(reviewWords)),
		slog.String("media_policy", cfg.MediaPolicy))

	return audit.NewKeywordModerator(blockWords, reviewWords, audit.MediaPolicy(cfg.MediaPolicy))
}
