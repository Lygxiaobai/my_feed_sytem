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
	"my_feed_system/internal/observability"
	"my_feed_system/internal/popularity"
	"my_feed_system/internal/social"
	"my_feed_system/internal/video"
)

func NewRouter(
	db *gorm.DB,
	redisClient redis.Cmdable,
	popularityService *popularity.Service,
	publisher *mq.Publisher,
	jwtSecret string,
	uploadDir string,
) *gin.Engine {
	return NewRouterWithLocalCaches(db, redisClient, popularityService, publisher, nil, nil, nil, jwtSecret, uploadDir)
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
) *gin.Engine {
	r := gin.Default()

	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "pong"})
	})
	r.GET("/metrics", gin.WrapH(observability.NewMetricsHandler()))
	r.Static("/static", uploadDir)

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

	accountHandler := account.NewHandler(account.NewServiceWithTokenCache(db, tokenCache, jwtSecret))
	accountGroup := r.Group("/account")
	accountGroup.POST("/register", registerIPLimit, accountHandler.Register)
	accountGroup.POST("/login", loginIPLimit, accountHandler.Login)
	accountGroup.POST("/findByID", accountHandler.FindByID)
	accountGroup.POST("/findByUsername", accountHandler.FindByUsername)

	protectedAccountGroup := accountGroup.Group("")
	protectedAccountGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	accountHandler.RegisterProtectedRoutes(protectedAccountGroup)

	videoHandler := video.NewHandler(video.NewServiceWithCachesAndPublisher(
		db,
		popularityService,
		detailCache,
		localDetailCache,
		publisher,
		uploadDir,
	), uploadDir)
	videoGroup := r.Group("/video")
	videoHandler.RegisterRoutes(videoGroup)

	protectedVideoGroup := videoGroup.Group("")
	protectedVideoGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	videoHandler.RegisterProtectedRoutes(protectedVideoGroup)

	likeHandler := like.NewHandler(like.NewServiceWithDetailCacheAndPublisher(db, popularityService, detailCache, publisher))
	likeGroup := r.Group("/like")
	likeGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
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

	feedHandler := feed.NewHandler(feed.NewServiceWithCachesAndTimeline(
		db,
		popularityService,
		latestCache,
		localLatestCache,
		hotCache,
		localHotCache,
		timelineStore,
		uploadDir,
	))
	feedGroup := r.Group("/feed")
	feedHandler.RegisterRoutes(feedGroup)

	protectedFeedGroup := feedGroup.Group("")
	protectedFeedGroup.Use(jwtmiddleware.JWTAuthWithTokenCache(db, tokenCache, jwtSecret))
	feedHandler.RegisterProtectedRoutes(protectedFeedGroup)

	return r
}
