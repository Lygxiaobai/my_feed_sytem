package http

import (
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"my_feed_system/internal/account"
	"my_feed_system/internal/comment"
	"my_feed_system/internal/feed"
	"my_feed_system/internal/like"
	jwtmiddleware "my_feed_system/internal/middleware/jwt"
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

	accountHandler := account.NewHandler(account.NewServiceWithTokenCache(db, tokenCache, jwtSecret))
	accountGroup := r.Group("/account")
	accountHandler.RegisterRoutes(accountGroup)

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
	likeHandler.RegisterProtectedRoutes(likeGroup)

	commentHandler := comment.NewHandler(comment.NewServiceWithDetailCacheAndPublisher(db, popularityService, detailCache, publisher))
	commentGroup := r.Group("/comment")
	commentHandler.RegisterRoutes(commentGroup)

	protectedCommentGroup := commentGroup.Group("")
	protectedCommentGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	commentHandler.RegisterProtectedRoutes(protectedCommentGroup)

	socialHandler := social.NewHandler(social.NewServiceWithPublisher(db, publisher))
	socialGroup := r.Group("/social")
	socialHandler.RegisterRoutes(socialGroup)

	protectedSocialGroup := socialGroup.Group("")
	protectedSocialGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	socialHandler.RegisterProtectedRoutes(protectedSocialGroup)

	feedHandler := feed.NewHandler(feed.NewServiceWithLatestCacheAndTimeline(db, popularityService, latestCache, timelineStore, uploadDir))
	feedGroup := r.Group("/feed")
	feedHandler.RegisterRoutes(feedGroup)

	protectedFeedGroup := feedGroup.Group("")
	protectedFeedGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	feedHandler.RegisterProtectedRoutes(protectedFeedGroup)

	return r
}
