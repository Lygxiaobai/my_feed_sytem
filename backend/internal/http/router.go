package http

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"my_feed_system/internal/account"
	"my_feed_system/internal/comment"
	"my_feed_system/internal/feed"
	"my_feed_system/internal/like"
	jwtmiddleware "my_feed_system/internal/middleware/jwt"
	"my_feed_system/internal/middleware/ratelimit"
	"my_feed_system/internal/mq"
	"my_feed_system/internal/popularity"
	"my_feed_system/internal/social"
	"my_feed_system/internal/video"
)

// NewRouter 负责装配各业务模块依赖并注册 HTTP 路由。
func NewRouter(
	db *gorm.DB,
	redisClient redis.Cmdable,
	popularityService *popularity.Service,
	publisher *mq.Publisher,
	jwtSecret string,
	uploadDir string,
) *gin.Engine {
	r := gin.Default()

	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
		})
	})
	r.Static("/static", uploadDir)

	// 共享缓存与时间线索引在路由装配阶段统一创建，供各模块复用。
	tokenCache := account.NewTokenCache(redisClient)
	detailCache := video.NewDetailCache(redisClient)
	latestCache := feed.NewLatestCache(redisClient)
	timelineStore := feed.NewGlobalTimelineStore(redisClient)

	var rateLimiter ratelimit.Checker
	if redisClient != nil {
		rateLimiter = ratelimit.NewFixedWindow(redisClient)
	}

	// 登录注册只按 IP 控制，主要防刷接口和撞库。
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
	// 点赞/取消点赞同时按 IP 和账号限流，既拦截单机刷请求，也限制单账号频繁操作。
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
	// 评论发布阈值更严格，避免短时间灌评论；删除给更宽一点，减少误伤正常清理操作。
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
	// 关注/取关也拆成独立桶，避免 follow 的高频把 unfollow 一起误伤。
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

	accountHandler := account.NewHandler(account.NewServiceWithTokenCache(db, tokenCache, jwtSecret))
	accountGroup := r.Group("/account")
	// 只给高风险匿名写接口挂限流，其余读接口保持原样。
	accountGroup.POST("/register", registerIPLimit, accountHandler.Register)
	accountGroup.POST("/login", loginIPLimit, accountHandler.Login)
	accountGroup.POST("/findByID", accountHandler.FindByID)
	accountGroup.POST("/findByUsername", accountHandler.FindByUsername)

	// 每个业务域都拆分匿名路由与鉴权路由，避免在 handler 内重复判断登录态。
	protectedAccountGroup := accountGroup.Group("")
	protectedAccountGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	accountHandler.RegisterProtectedRoutes(protectedAccountGroup)

	videoHandler := video.NewHandler(video.NewServiceWithDetailCacheAndPublisher(db, popularityService, detailCache, publisher, uploadDir), uploadDir)
	videoGroup := r.Group("/video")
	videoHandler.RegisterRoutes(videoGroup)

	protectedVideoGroup := videoGroup.Group("")
	protectedVideoGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	videoHandler.RegisterProtectedRoutes(protectedVideoGroup)

	// 写接口服务注入 publisher 后，可切换到异步事件写路径。
	likeHandler := like.NewHandler(like.NewServiceWithDetailCacheAndPublisher(db, popularityService, detailCache, publisher))
	likeGroup := r.Group("/like")
	likeGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	// 账号维度限流必须放在 JWT 鉴权之后，这样中间件才能拿到 account_id。
	likeGroup.POST("/like", likeLikeIPLimit, likeLikeAccountLimit, likeHandler.Like)
	likeGroup.POST("/unlike", likeUnlikeIPLimit, likeUnlikeAccountLimit, likeHandler.Unlike)
	likeGroup.POST("/isLiked", likeHandler.IsLiked)
	likeGroup.POST("/listLikedVideoIDs", likeHandler.ListLikedVideoIDs)

	commentHandler := comment.NewHandler(comment.NewServiceWithDetailCacheAndPublisher(db, popularityService, detailCache, publisher))
	commentGroup := r.Group("/comment")
	commentHandler.RegisterRoutes(commentGroup)

	protectedCommentGroup := commentGroup.Group("")
	protectedCommentGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	protectedCommentGroup.POST("/publish", commentPublishIPLimit, commentPublishAccountLimit, commentHandler.Publish)
	protectedCommentGroup.POST("/delete", commentDeleteIPLimit, commentDeleteAccountLimit, commentHandler.Delete)

	socialHandler := social.NewHandler(social.NewServiceWithPublisher(db, publisher))
	socialGroup := r.Group("/social")
	socialHandler.RegisterRoutes(socialGroup)

	protectedSocialGroup := socialGroup.Group("")
	protectedSocialGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	protectedSocialGroup.POST("/follow", socialFollowIPLimit, socialFollowAccountLimit, socialHandler.Follow)
	protectedSocialGroup.POST("/unfollow", socialUnfollowIPLimit, socialUnfollowAccountLimit, socialHandler.Unfollow)
	protectedSocialGroup.POST("/getAllFollowers", socialHandler.GetAllFollowers)
	protectedSocialGroup.POST("/getAllVloggers", socialHandler.GetAllVloggers)

	feedHandler := feed.NewHandler(feed.NewServiceWithLatestCacheAndTimeline(db, popularityService, latestCache, timelineStore, uploadDir))
	feedGroup := r.Group("/feed")
	feedHandler.RegisterRoutes(feedGroup)

	protectedFeedGroup := feedGroup.Group("")
	protectedFeedGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	feedHandler.RegisterProtectedRoutes(protectedFeedGroup)

	return r
}
