package http

import (
	"log/slog"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"my_feed_system/internal/account"
	"my_feed_system/internal/admin"
	"my_feed_system/internal/analytics"
	"my_feed_system/internal/audit"
	"my_feed_system/internal/comment"
	"my_feed_system/internal/config"
	"my_feed_system/internal/danmaku"
	"my_feed_system/internal/dm"
	"my_feed_system/internal/feed"
	"my_feed_system/internal/history"
	"my_feed_system/internal/invoice"
	"my_feed_system/internal/like"
	"my_feed_system/internal/media"
	"my_feed_system/internal/middleware/accesslog"
	jwtmiddleware "my_feed_system/internal/middleware/jwt"
	"my_feed_system/internal/middleware/ratelimit"
	"my_feed_system/internal/middleware/requestid"
	"my_feed_system/internal/middleware/traffic"
	"my_feed_system/internal/mq"
	"my_feed_system/internal/notification"
	"my_feed_system/internal/observability"
	"my_feed_system/internal/ops"
	"my_feed_system/internal/popularity"
	"my_feed_system/internal/recommend"
	"my_feed_system/internal/report"
	"my_feed_system/internal/response"
	"my_feed_system/internal/social"
	"my_feed_system/internal/video"
	"my_feed_system/internal/wallet"
)

func NewRouter(
	db *gorm.DB,
	redisClient redis.Cmdable,
	popularityService *popularity.Service,
	publisher *mq.Publisher,
	jwtSecret string,
	uploadDir string,
) *gin.Engine {
	return NewRouterWithLocalCaches(db, redisClient, popularityService, publisher, nil, nil, nil, jwtSecret, uploadDir, 0, config.AuditConfig{}, config.AuthConfig{}, config.OpsConfig{}, config.StripeConfig{}, config.FeedConfig{}, config.EmbeddingConfig{})
}

func NewRouterWithLocalCaches(
	db *gorm.DB,
	redisClient redis.Cmdable,
	popularityService *popularity.Service,
	publisher *mq.Publisher,
	localDetailCache *video.LocalDetailCache,
	localLatestCache *feed.LocalLatestPageCache,
	localHotCache *feed.LocalHotPageCache,
	jwtSecret string,
	uploadDir string,
	maxVideoBytes int64,
	auditCfg config.AuditConfig,
	authCfg config.AuthConfig,
	opsCfg config.OpsConfig,
	stripeCfg config.StripeConfig,
	feedCfg config.FeedConfig,
	embeddingCfg config.EmbeddingConfig,
) *gin.Engine {
	// 不用 gin.Default()：它固定绑定 gin.Logger()，而后者只能输出拼好的文本行，
	// 无法交给 slog 分级和结构化。这里自行组装等价的中间件链。
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(requestid.New())
	r.Use(accesslog.New(accesslog.Options{
		// 健康检查每十秒一次，指标端点由 Prometheus 定期抓取，都属于记录了也无人查看的噪音。
		SkipPaths:     []string{"/ping", "/metrics", "/event/report", "/ops/gate"},
		SlowThreshold: time.Second,
	}))

	r.GET("/ping", func(c *gin.Context) {
		response.OK(c, gin.H{"message": "pong"})
	})
	r.GET("/metrics", gin.WrapH(observability.NewMetricsHandler()))
	// 仅暴露处理后的媒体目录，sources 目录只供 Worker 读取，避免原始上传文件被直接访问。
	r.Static("/static/videos", filepath.Join(uploadDir, "videos"))
	r.Static("/static/covers", filepath.Join(uploadDir, "covers"))

	tokenCache := account.NewTokenCache(redisClient)
	detailCache := video.NewDetailCache(redisClient)
	latestCache := feed.NewLatestCache(redisClient)
	hotCache := feed.NewHotPageCache(redisClient)
	// 时间线索引与 latest 页缓存配套使用：前者加速候选集读取，后者缓存最终结果页。
	timelineStore := feed.NewGlobalTimelineStore(redisClient)

	var rateLimiter ratelimit.Checker
	if redisClient != nil {
		rateLimiter = ratelimit.NewFixedWindow(redisClient)
	}
	// OptionalJWT 只为全局账号天花板取身份，无效 token 不拦截。
	// 写接口仍走后面的严格鉴权（含 token 吊销缓存）。
	r.Use(jwtmiddleware.OptionalJWTAuth(jwtSecret))
	r.Use(traffic.New(rateLimiter, redisClient, traffic.DefaultConfig()))

	loginIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "account.login.ip",
		Limit:    10,
		Window:   time.Minute,
		FailOpen: true,
	})
	registerIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "account.register.ip",
		Limit:    5,
		Window:   10 * time.Minute,
		FailOpen: true,
	})
	likeLikeIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "like.like.ip",
		Limit:    60,
		Window:   time.Minute,
		FailOpen: true,
	})
	likeLikeAccountLimit := ratelimit.ByAccountID(rateLimiter, ratelimit.Policy{
		Name:     "like.like.account",
		Limit:    30,
		Window:   time.Minute,
		FailOpen: true,
	})
	likeUnlikeIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "like.unlike.ip",
		Limit:    60,
		Window:   time.Minute,
		FailOpen: true,
	})
	likeUnlikeAccountLimit := ratelimit.ByAccountID(rateLimiter, ratelimit.Policy{
		Name:     "like.unlike.account",
		Limit:    30,
		Window:   time.Minute,
		FailOpen: true,
	})
	commentPublishIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "comment.publish.ip",
		Limit:    30,
		Window:   time.Minute,
		FailOpen: true,
	})
	commentPublishAccountLimit := ratelimit.ByAccountID(rateLimiter, ratelimit.Policy{
		Name:     "comment.publish.account",
		Limit:    15,
		Window:   time.Minute,
		FailOpen: true,
	})
	commentDeleteIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "comment.delete.ip",
		Limit:    40,
		Window:   time.Minute,
		FailOpen: true,
	})
	commentDeleteAccountLimit := ratelimit.ByAccountID(rateLimiter, ratelimit.Policy{
		Name:     "comment.delete.account",
		Limit:    20,
		Window:   time.Minute,
		FailOpen: true,
	})
	socialFollowIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "social.follow.ip",
		Limit:    40,
		Window:   time.Minute,
		FailOpen: true,
	})
	socialFollowAccountLimit := ratelimit.ByAccountID(rateLimiter, ratelimit.Policy{
		Name:     "social.follow.account",
		Limit:    20,
		Window:   time.Minute,
		FailOpen: true,
	})
	socialUnfollowIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "social.unfollow.ip",
		Limit:    40,
		Window:   time.Minute,
		FailOpen: true,
	})
	socialUnfollowAccountLimit := ratelimit.ByAccountID(rateLimiter, ratelimit.Policy{
		Name:     "social.unfollow.account",
		Limit:    20,
		Window:   time.Minute,
		FailOpen: true,
	})

	eventReportIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "event.report.ip",
		Limit:    80,
		Window:   time.Minute,
		FailOpen: true,
	})
	eventGroup := r.Group("/event")
	eventGroup.Use(jwtmiddleware.OptionalJWTAuth(jwtSecret), eventReportIPLimit)
	analytics.NewHandler().RegisterRoutes(eventGroup)

	emailSendIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "account.email.send.ip",
		Limit:    5,
		Window:   time.Minute,
		FailOpen: true,
	})
	emailVerifyIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "account.email.verify.ip",
		Limit:    20,
		Window:   time.Minute,
		FailOpen: true,
	})
	accountLookupIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "account.lookup.ip",
		Limit:    40,
		Window:   time.Minute,
		FailOpen: true,
	})

	notifyWriter := notification.NewWriter(db)

	walletService := wallet.NewService(db)
	walletService.ConfigureStripe(stripeCfg)
	walletService.SetPublisher(publisher, popularityService)
	walletService.SetNotifier(notifyWriter)
	invoiceService := invoice.NewService(db, invoice.OrdersFromWallet(walletService))
	accountService := account.NewServiceWithTokenCache(db, tokenCache, jwtSecret)
	accountService.SetCreatedHook(walletService.GrantRegisterGiftTx)
	var otpStore *account.OTPStore
	if redisClient != nil {
		ttl := time.Duration(authCfg.Email.CodeTTLSeconds) * time.Second
		otpStore = account.NewOTPStore(redisClient, ttl)
	}
	accountService.SetEmail(otpStore, &account.SMTPMailer{
		Host:     authCfg.SMTP.Host,
		Port:     authCfg.SMTP.Port,
		TLS:      authCfg.SMTP.TLS,
		User:     authCfg.SMTP.User,
		Password: authCfg.SMTP.Password,
		From:     authCfg.SMTP.From,
	}, authCfg.Email)
	accountHandler := account.NewHandler(accountService)
	accountGroup := r.Group("/account")
	accountGroup.POST("/register", registerIPLimit, accountHandler.Register)
	accountGroup.POST("/login", loginIPLimit, accountHandler.Login)
	accountGroup.POST("/email/sendCode", emailSendIPLimit, accountHandler.SendEmailCode)
	accountGroup.POST("/email/verify", emailVerifyIPLimit, accountHandler.VerifyEmail)
	accountGroup.POST("/findByID", accountLookupIPLimit, accountHandler.FindByID)
	accountGroup.POST("/findByUsername", accountLookupIPLimit, accountHandler.FindByUsername)
	passkeyLoginIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "account.passkey.login.ip",
		Limit:    30,
		Window:   time.Minute,
		FailOpen: true,
	})
	if redisClient != nil {
		accountService.SetPasskeyStore(account.NewPasskeySessionStore(redisClient))
	}
	accountGroup.POST("/passkey/login/begin", passkeyLoginIPLimit, accountHandler.BeginPasskeyLogin)
	accountGroup.POST("/passkey/login/finish", loginIPLimit, accountHandler.FinishPasskeyLogin)

	protectedAccountGroup := accountGroup.Group("")
	protectedAccountGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	accountHandler.RegisterProtectedRoutes(protectedAccountGroup)
	passkeyManageLimit := ratelimit.ByAccountID(rateLimiter, ratelimit.Policy{
		Name:     "account.passkey.manage.account",
		Limit:    20,
		Window:   time.Minute,
		FailOpen: true,
	})
	protectedAccountGroup.POST("/passkey/register/begin", passkeyManageLimit, accountHandler.BeginPasskeyRegister)
	protectedAccountGroup.POST("/passkey/register/finish", passkeyManageLimit, accountHandler.FinishPasskeyRegister)
	protectedAccountGroup.POST("/passkey/list", accountHandler.ListPasskeys)
	protectedAccountGroup.POST("/passkey/delete", passkeyManageLimit, accountHandler.DeletePasskey)

	opsHandler := ops.NewHandler(ops.NewService(opsCfg, auditCfg.ReviewerAccountIDs), db, tokenCache, jwtSecret)
	opsGroup := r.Group("/ops")
	opsGroup.GET("/gate", opsHandler.Gate)
	opsProtected := opsGroup.Group("")
	opsProtected.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	opsLogsLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "ops.logs.ip",
		Limit:    30,
		Window:   time.Minute,
		FailOpen: true,
	})
	opsProtected.GET("/access", opsHandler.Access)
	opsProtected.GET("/metrics", opsHandler.Metrics)
	opsProtected.POST("/logs", opsLogsLimit, opsHandler.Logs)

	videoService := video.NewServiceWithCachesAndPublisher(
		db,
		popularityService,
		detailCache,
		localDetailCache,
		publisher,
		uploadDir,
		auditCfg.Enabled,
	)
	videoHandler := video.NewHandler(videoService, uploadDir, media.NewService(db, uploadDir, maxVideoBytes), rateLimiter)
	videoGroup := r.Group("/video")
	// 公开路由挂可选鉴权：作者本人需要能看到自己尚未过审的内容，
	// 匿名访问则只看得到已过审的。
	videoReadIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "video.read.ip",
		Limit:    180,
		Window:   time.Minute,
		FailOpen: true,
	})
	videoUploadIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "video.upload.ip",
		Limit:    20,
		Window:   10 * time.Minute,
		FailOpen: true,
	})
	videoUploadAccountLimit := ratelimit.ByAccountID(rateLimiter, ratelimit.Policy{
		Name:     "video.upload.account",
		Limit:    12,
		Window:   10 * time.Minute,
		FailOpen: true,
	})
	videoPublishIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "video.publish.ip",
		Limit:    20,
		Window:   10 * time.Minute,
		FailOpen: true,
	})
	videoPublishAccountLimit := ratelimit.ByAccountID(rateLimiter, ratelimit.Policy{
		Name:     "video.publish.account",
		Limit:    12,
		Window:   10 * time.Minute,
		FailOpen: true,
	})
	videoGroup.Use(jwtmiddleware.OptionalJWTAuth(jwtSecret), videoReadIPLimit)
	videoHandler.RegisterRoutes(videoGroup)

	protectedVideoGroup := videoGroup.Group("")
	protectedVideoGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	videoHandler.RegisterProtectedRoutes(
		protectedVideoGroup,
		[]gin.HandlerFunc{videoUploadIPLimit, videoUploadAccountLimit},
		[]gin.HandlerFunc{videoPublishIPLimit, videoPublishAccountLimit},
	)

	if auditCfg.Enabled {
		auditService := audit.NewService(
			db,
			video.NewAuditStore(db),
			buildModerator(auditCfg),
			video.NewApprovalPublisher(db),
			auditCfg.ReviewerAccountIDs,
		)
		auditHandler := audit.NewHandler(auditService)
		auditGroup := r.Group("/audit")
		auditGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
		auditHandler.RegisterProtectedRoutes(auditGroup)
		slog.Info("content audit enabled")
	} else {
		slog.Info("content audit disabled, publish goes public immediately")
	}

	// 举报独立于 audit.enabled：机审是可选能力，而举报是常设的用户通道，
	// 不能因为机审关闭就连带消失——那会让平台失去接收违规通知的唯一入口。
	reportSubmitIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "report.submit.ip",
		Limit:    20,
		Window:   time.Minute,
		FailOpen: true,
	})
	reportSubmitAccountLimit := ratelimit.ByAccountID(rateLimiter, ratelimit.Policy{
		Name:     "report.submit.account",
		Limit:    10,
		Window:   time.Minute,
		FailOpen: true,
	})
	// 审核员白名单复用审核配置：这里只有「是/不是审核员」一个区分，
	// 为它再引入一套配置或 RBAC 属于过度设计。
	reportService := report.NewService(db, videoService, auditCfg.ReviewerAccountIDs)
	reportHandler := report.NewHandler(reportService)
	reportGroup := r.Group("/report")
	reportGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	reportHandler.RegisterProtectedRoutes(reportGroup, reportSubmitIPLimit, reportSubmitAccountLimit)

	// 管理后台与运维台分开：这里只认审核员白名单，不认测试邮箱。
	// 测试邮箱规则写在公开仓库里，不能当成内容处置权。
	adminReadLimit := ratelimit.ByAccountID(rateLimiter, ratelimit.Policy{
		Name:     "admin.read.account",
		Limit:    60,
		Window:   time.Minute,
		FailOpen: true,
	})
	adminWriteLimit := ratelimit.ByAccountID(rateLimiter, ratelimit.Policy{
		Name:     "admin.write.account",
		Limit:    20,
		Window:   time.Minute,
		FailOpen: true,
	})
	adminService := admin.NewService(reportService, videoService, accountService, invoiceService, walletService)
	adminHandler := admin.NewHandler(adminService)
	adminGroup := r.Group("/admin")
	adminGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	adminHandler.RegisterProtectedRoutes(adminGroup, []gin.HandlerFunc{adminReadLimit}, []gin.HandlerFunc{adminWriteLimit})

	recCfg := feedCfg.Recommend
	recCfg.ApplyDefaults()
	embeddingCfg.ApplyDefaults()
	recommendService := recommend.NewService(
		db,
		recommend.NewHTTPEmbedder(embeddingCfg),
		redisClient,
		popularityService,
		video.NewMediaValidator(uploadDir),
		recCfg,
	)

	likeService := like.NewServiceWithCachesAndPublisher(db, popularityService, detailCache, localDetailCache, publisher)
	likeService.SetNotifier(notifyWriter)
	likeService.SetInterestInvalidator(recommendService)
	likeService.SetInterestRecorder(accountService)
	walletService.SetInterestInvalidator(recommendService)
	walletService.SetInterestRecorder(accountService)
	adminService.SetInterests(recommendService)
	likeHandler := like.NewHandler(likeService)
	likeGroup := r.Group("/like")
	likeGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	likeGroup.POST("/like", likeLikeIPLimit, likeLikeAccountLimit, likeHandler.Like)
	likeGroup.POST("/unlike", likeUnlikeIPLimit, likeUnlikeAccountLimit, likeHandler.Unlike)
	likeGroup.POST("/isLiked", likeHandler.IsLiked)
	likeGroup.POST("/listLikedVideoIDs", likeHandler.ListLikedVideoIDs)

	commentService := comment.NewServiceWithDetailCacheAndPublisher(db, popularityService, detailCache, publisher)
	commentService.SetNotifier(notifyWriter)
	commentService.SetInterestRecorder(accountService)
	commentHandler := comment.NewHandler(commentService)
	commentGroup := r.Group("/comment")
	commentHandler.RegisterRoutes(commentGroup)

	protectedCommentGroup := commentGroup.Group("")
	protectedCommentGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	protectedCommentGroup.POST("/publish", commentPublishIPLimit, commentPublishAccountLimit, commentHandler.Publish)
	protectedCommentGroup.POST("/delete", commentDeleteIPLimit, commentDeleteAccountLimit, commentHandler.Delete)

	danmakuSendIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "danmaku.send.ip",
		Limit:    40,
		Window:   time.Minute,
		FailOpen: true,
	})
	danmakuSendAccountLimit := ratelimit.ByAccountID(rateLimiter, ratelimit.Policy{
		Name:     "danmaku.send.account",
		Limit:    20,
		Window:   time.Minute,
		FailOpen: true,
	})
	danmakuHandler := danmaku.NewHandler(danmaku.NewService(db, danmaku.NewVideoAccess(videoService)))
	danmakuGroup := r.Group("/danmaku")
	// 列表挂可选鉴权：作者需要能给自己尚未过审的视频看弹幕，其他人一律当视频不存在。
	danmakuGroup.Use(jwtmiddleware.OptionalJWTAuth(jwtSecret))
	danmakuHandler.RegisterRoutes(danmakuGroup)
	protectedDanmakuGroup := danmakuGroup.Group("")
	protectedDanmakuGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	protectedDanmakuGroup.Use(danmakuSendIPLimit, danmakuSendAccountLimit)
	danmakuHandler.RegisterProtectedRoutes(protectedDanmakuGroup)

	historyUpsertIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "history.upsert.ip",
		Limit:    90,
		Window:   time.Minute,
		FailOpen: true,
	})
	historyUpsertAccountLimit := ratelimit.ByAccountID(rateLimiter, ratelimit.Policy{
		Name:     "history.upsert.account",
		Limit:    60,
		Window:   time.Minute,
		FailOpen: true,
	})
	historyReadIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "history.read.ip",
		Limit:    60,
		Window:   time.Minute,
		FailOpen: true,
	})
	historyHandler := history.NewHandler(history.NewService(db, videoService))
	historyGroup := r.Group("/history")
	historyGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	historyGroup.POST("/upsert", historyUpsertIPLimit, historyUpsertAccountLimit, historyHandler.Upsert)
	historyGroup.POST("/list", historyReadIPLimit, historyHandler.List)
	historyGroup.POST("/progress", historyReadIPLimit, historyHandler.Progress)

	socialService := social.NewServiceWithPublisher(db, publisher)
	socialService.SetNotifier(notifyWriter)
	socialHandler := social.NewHandler(socialService)
	socialGroup := r.Group("/social")
	socialHandler.RegisterRoutes(socialGroup)

	protectedSocialGroup := socialGroup.Group("")
	protectedSocialGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	protectedSocialGroup.POST("/follow", socialFollowIPLimit, socialFollowAccountLimit, socialHandler.Follow)
	protectedSocialGroup.POST("/unfollow", socialUnfollowIPLimit, socialUnfollowAccountLimit, socialHandler.Unfollow)
	protectedSocialGroup.POST("/getAllFollowers", socialHandler.GetAllFollowers)
	protectedSocialGroup.POST("/getAllVloggers", socialHandler.GetAllVloggers)

	feedService := feed.NewServiceWithCachesAndTimeline(
		db,
		popularityService,
		latestCache,
		localLatestCache,
		hotCache,
		localHotCache,
		timelineStore,
		uploadDir,
	)
	if redisClient != nil {
		fanoutCfg := feedCfg.Fanout
		fanoutCfg.ApplyDefaults()
		feedService = feedService.WithFollowingFanout(&feed.FollowingFanout{
			Inbox:          feed.NewInboxStore(redisClient, fanoutCfg.InboxMaxSize, fanoutCfg.ActiveTTL()),
			Outbox:         feed.NewOutboxStore(redisClient, fanoutCfg.OutboxMaxSize),
			Following:      feed.NewFollowingCache(redisClient, 0),
			PullThreshold:  fanoutCfg.PullThreshold,
			MaxPullAuthors: fanoutCfg.MaxPullAuthors,
		})
	}
	feedHandler := feed.NewHandler(feedService)
	feedHandler.SetRecommender(recommendService)
	feedReadIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "feed.read.ip",
		Limit:    120,
		Window:   time.Minute,
		FailOpen: true,
	})
	feedRecommendIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "feed.recommend.ip",
		Limit:    40,
		Window:   time.Minute,
		FailOpen: true,
	})
	feedGroup := r.Group("/feed")
	// 推荐需要可选登录身份；其它公开信息流忽略 account_id。
	feedGroup.Use(jwtmiddleware.OptionalJWTAuth(jwtSecret), feedReadIPLimit)
	feedHandler.RegisterRoutes(feedGroup, feedRecommendIPLimit)

	protectedFeedGroup := feedGroup.Group("")
	protectedFeedGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	feedHandler.RegisterProtectedRoutes(protectedFeedGroup)

	walletTipIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "wallet.tip.ip",
		Limit:    40,
		Window:   time.Minute,
		FailOpen: true,
	})
	walletTipAccountLimit := ratelimit.ByAccountID(rateLimiter, ratelimit.Policy{
		Name:     "wallet.tip.account",
		Limit:    20,
		Window:   time.Minute,
		FailOpen: true,
	})
	walletPayIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "wallet.recharge.ip",
		Limit:    20,
		Window:   time.Minute,
		FailOpen: true,
	})
	walletDailyIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "wallet.daily.ip",
		Limit:    20,
		Window:   time.Minute,
		FailOpen: true,
	})

	notifyListIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "notification.list.ip",
		Limit:    60,
		Window:   time.Minute,
		FailOpen: true,
	})
	notifyUnreadIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "notification.unread.ip",
		Limit:    120,
		Window:   time.Minute,
		FailOpen: true,
	})
	notifyWriteAccountLimit := ratelimit.ByAccountID(rateLimiter, ratelimit.Policy{
		Name:     "notification.write.account",
		Limit:    40,
		Window:   time.Minute,
		FailOpen: true,
	})
	notifyHandler := notification.NewHandler(notification.NewService(db))
	notifyGroup := r.Group("/notification")
	notifyGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	notifyGroup.POST("/list", notifyListIPLimit, notifyHandler.List)
	notifyGroup.POST("/unreadCount", notifyUnreadIPLimit, notifyHandler.UnreadCount)
	notifyGroup.POST("/markRead", notifyWriteAccountLimit, notifyHandler.MarkRead)
	notifyGroup.POST("/markAllRead", notifyWriteAccountLimit, notifyHandler.MarkAllRead)

	dmSendIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "dm.send.ip",
		Limit:    40,
		Window:   time.Minute,
		FailOpen: true,
	})
	dmSendAccountLimit := ratelimit.ByAccountID(rateLimiter, ratelimit.Policy{
		Name:     "dm.send.account",
		Limit:    30,
		Window:   time.Minute,
		FailOpen: true,
	})
	dmReadIPLimit := ratelimit.ByIP(rateLimiter, ratelimit.Policy{
		Name:     "dm.read.ip",
		Limit:    80,
		Window:   time.Minute,
		FailOpen: true,
	})
	dmHandler := dm.NewHandler(dm.NewService(db))
	dmGroup := r.Group("/dm")
	dmGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	dmGroup.POST("/inbox", dmReadIPLimit, dmHandler.Inbox)
	dmGroup.POST("/thread", dmReadIPLimit, dmHandler.Thread)
	dmGroup.POST("/unreadCount", notifyUnreadIPLimit, dmHandler.UnreadCount)
	dmGroup.POST("/markRead", notifyWriteAccountLimit, dmHandler.MarkRead)
	dmGroup.POST("/send", dmSendIPLimit, dmSendAccountLimit, dmHandler.Send)

	walletHandler := wallet.NewHandler(walletService)
	walletGroup := r.Group("/wallet")
	walletHandler.RegisterPublicRoutes(walletGroup)
	protectedWallet := walletGroup.Group("")
	protectedWallet.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	protectedWallet.POST("/summary", walletHandler.Summary)
	protectedWallet.POST("/ledger", walletHandler.ListLedger)
	protectedWallet.POST("/checkin", walletDailyIPLimit, walletHandler.Checkin)
	protectedWallet.POST("/checkin/month", walletHandler.CheckinMonth)
	protectedWallet.POST("/lottery", walletDailyIPLimit, walletHandler.Lottery)
	protectedWallet.POST("/recharge/create", walletPayIPLimit, walletHandler.CreateRecharge)
	protectedWallet.POST("/recharge/query", walletHandler.QueryOrder)
	protectedWallet.POST("/tip", walletTipIPLimit, walletTipAccountLimit, walletHandler.Tip)
	protectedWallet.POST("/tips/mine", walletHandler.ListMyTips)
	protectedWallet.POST("/tips/byVideo", walletHandler.ListVideoTips)

	invoiceApplyLimit := ratelimit.ByAccountID(rateLimiter, ratelimit.Policy{
		Name:     "invoice.write.account",
		Limit:    10,
		Window:   time.Minute,
		FailOpen: true,
	})
	invoiceHandler := invoice.NewHandler(invoiceService)
	invoiceGroup := r.Group("/invoice")
	invoiceGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	invoiceHandler.RegisterProtectedRoutes(invoiceGroup, invoiceApplyLimit)

	return r
}

// buildModerator 按配置装配审核实现。
//
// 当前只有本地词库；日后接入云厂商时在这里替换或用装饰器串联即可，
// 业务侧的状态机、流水与查询过滤都不需要改动。
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
