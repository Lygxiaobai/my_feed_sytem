// Package traffic 在现有按路由限流之上补一层流量治理：
// 全局 IP/账号天花板、进程内并发保护，以及把反复撞限的主体送进处罚箱。
//
// 阈值写在代码默认值里，不新增 YAML。部署 overlay 会盖掉仓库配置，
// 新配置项若只写在仓库里会静默失效；治理规则属于安全底线，不能依赖那条路径。
package traffic

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"my_feed_system/internal/middleware/ratelimit"
	"my_feed_system/internal/observability"
	"my_feed_system/internal/response"
)

const (
	scopeGlobalIP      = "http.global.ip"
	scopeGlobalAccount = "http.global.account"
	scopeInFlight      = "http.inflight"
	dimIP              = "ip"
	dimAccount         = "account"
	defaultMessage     = "操作过于频繁，请稍后再试"
	defaultTimeout     = 200 * time.Millisecond
)

// Config 是流量治理的阈值。零值会在 New 里补成默认值。
type Config struct {
	IPLimit             int64
	AccountLimit        int64
	Window              time.Duration
	PenaltyIPLimit      int64
	PenaltyAccountLimit int64
	DenyThreshold       int64
	DenyWindow          time.Duration
	PenaltyTTL          time.Duration
	MaxInFlight         int64
	Timeout             time.Duration
	FailOpen            bool
	SkipPaths           []string
	SkipPrefixes        []string
}

// DefaultConfig 面向公测单机：全局天花板只挡洪水，真正收紧靠按路由限流和处罚箱。
func DefaultConfig() Config {
	return Config{
		IPLimit:             600,
		AccountLimit:        800,
		Window:              time.Minute,
		PenaltyIPLimit:      60,
		PenaltyAccountLimit: 80,
		DenyThreshold:       10,
		DenyWindow:          time.Minute,
		PenaltyTTL:          10 * time.Minute,
		MaxInFlight:         512,
		Timeout:             defaultTimeout,
		FailOpen:            true,
		SkipPaths: []string{
			"/ping",
			"/metrics",
			"/ops/gate",
			"/wallet/stripe/notify",
		},
		SkipPrefixes: []string{"/static/"},
	}
}

func (c Config) withDefaults() Config {
	d := DefaultConfig()
	if c.IPLimit < 0 {
		c.IPLimit = 0
	} else if c.IPLimit == 0 {
		c.IPLimit = d.IPLimit
	}
	if c.AccountLimit < 0 {
		c.AccountLimit = 0
	} else if c.AccountLimit == 0 {
		c.AccountLimit = d.AccountLimit
	}
	if c.Window <= 0 {
		c.Window = d.Window
	}
	if c.PenaltyIPLimit <= 0 {
		c.PenaltyIPLimit = d.PenaltyIPLimit
	}
	if c.PenaltyAccountLimit <= 0 {
		c.PenaltyAccountLimit = d.PenaltyAccountLimit
	}
	if c.DenyThreshold <= 0 {
		c.DenyThreshold = d.DenyThreshold
	}
	if c.DenyWindow <= 0 {
		c.DenyWindow = d.DenyWindow
	}
	if c.PenaltyTTL <= 0 {
		c.PenaltyTTL = d.PenaltyTTL
	}
	if c.MaxInFlight < 0 {
		c.MaxInFlight = 0
	} else if c.MaxInFlight == 0 {
		c.MaxInFlight = d.MaxInFlight
	}
	if c.Timeout <= 0 {
		c.Timeout = d.Timeout
	}
	if c.SkipPaths == nil {
		c.SkipPaths = d.SkipPaths
	}
	if c.SkipPrefixes == nil {
		c.SkipPrefixes = d.SkipPrefixes
	}
	c.FailOpen = true
	return c
}

// Guard 是挂在引擎上的治理中间件。
type Guard struct {
	checker  ratelimit.Checker
	box      Box
	cfg      Config
	skip     map[string]struct{}
	inflight atomic.Int64
}

// New 组装治理中间件。checker 或 Redis 缺失时跳过计数类规则，进程内并发保护仍生效。
func New(checker ratelimit.Checker, redisClient redis.Cmdable, cfg Config) gin.HandlerFunc {
	cfg = cfg.withDefaults()
	var box Box
	if redisClient != nil {
		box = newRedisBox(redisClient)
	}
	return newGuard(checker, box, cfg).Middleware()
}

func newGuard(checker ratelimit.Checker, box Box, cfg Config) *Guard {
	g := &Guard{
		checker: checker,
		box:     box,
		cfg:     cfg,
		skip:    make(map[string]struct{}, len(cfg.SkipPaths)),
	}
	for _, p := range cfg.SkipPaths {
		g.skip[p] = struct{}{}
	}
	return g
}

// Middleware 按「跳过 → 并发 → IP 天花板 → 账号天花板」的顺序执行。
func (g *Guard) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if g.shouldSkip(c.Request.URL.Path) {
			c.Next()
			return
		}

		if g.cfg.MaxInFlight > 0 {
			n := g.inflight.Add(1)
			defer g.inflight.Add(-1)
			if n > g.cfg.MaxInFlight {
				observability.ObserveRateLimit(scopeInFlight, observability.RateLimitDeny)
				c.Header("Retry-After", "1")
				response.Abort(c, http.StatusServiceUnavailable, response.SystemError, nil)
				return
			}
			observability.ObserveRateLimit(scopeInFlight, observability.RateLimitAllow)
		}

		if !g.allowDimension(c, dimIP, scopeGlobalIP, clientIP(c), g.cfg.IPLimit, g.cfg.PenaltyIPLimit) {
			return
		}
		if accountID := c.GetUint64("account_id"); accountID != 0 {
			if !g.allowDimension(c, dimAccount, scopeGlobalAccount, strconv.FormatUint(accountID, 10), g.cfg.AccountLimit, g.cfg.PenaltyAccountLimit) {
				return
			}
		}
		c.Next()
	}
}

func (g *Guard) shouldSkip(path string) bool {
	if _, ok := g.skip[path]; ok {
		return true
	}
	for _, prefix := range g.cfg.SkipPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func clientIP(c *gin.Context) string {
	ip := c.ClientIP()
	return strings.TrimSpace(ip)
}

func (g *Guard) allowDimension(c *gin.Context, dim string, scope string, subject string, limit int64, penaltyLimit int64) bool {
	if subject == "" || limit <= 0 || g.checker == nil {
		return true
	}

	effective := limit
	penalized := false
	if g.box != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), g.cfg.Timeout)
		hasPenalty, err := g.box.HasPenalty(ctx, dim, subject)
		cancel()
		if err != nil {
			if !g.cfg.FailOpen {
				response.Abort(c, http.StatusServiceUnavailable, response.CacheError, err)
				return false
			}
			slog.WarnContext(c.Request.Context(), "traffic penalty lookup bypassed",
				slog.String("dimension", dim), slog.String("error", err.Error()))
		} else if hasPenalty {
			penalized = true
			effective = penaltyLimit
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), g.cfg.Timeout)
	result, err := g.checker.Allow(ctx, scope, subject, effective, g.cfg.Window)
	cancel()
	if err != nil {
		observability.ObserveRateLimit(scope, observability.RateLimitBypass)
		if g.cfg.FailOpen {
			slog.WarnContext(c.Request.Context(), "traffic limit bypassed due to checker failure",
				slog.String("scope", scope), slog.String("error", err.Error()))
			return true
		}
		response.Abort(c, http.StatusServiceUnavailable, response.CacheError, err)
		return false
	}
	if result.Allowed {
		observability.ObserveRateLimit(scope, observability.RateLimitAllow)
		return true
	}

	observability.ObserveRateLimit(scope, observability.RateLimitDeny)
	g.recordDeny(c, dim, subject)
	retryAfterSeconds := int(math.Ceil(result.RetryAfter.Seconds()))
	if retryAfterSeconds < 1 {
		retryAfterSeconds = 1
	}
	c.Header("Retry-After", strconv.Itoa(retryAfterSeconds))
	if penalized {
		slog.WarnContext(c.Request.Context(), "traffic rejected under penalty",
			slog.String("scope", scope), slog.String("subject", subject))
	}
	response.FailTip(c, http.StatusTooManyRequests, response.RateLimited, defaultMessage, nil)
	c.Abort()
	return false
}

func (g *Guard) recordDeny(c *gin.Context, dim string, subject string) {
	if g.box == nil {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), g.cfg.Timeout)
	defer cancel()

	count, err := g.box.Denied(ctx, dim, subject, g.cfg.DenyWindow)
	if err != nil {
		slog.WarnContext(c.Request.Context(), "traffic deny counter failed",
			slog.String("dimension", dim), slog.String("error", err.Error()))
		return
	}
	if count < g.cfg.DenyThreshold {
		return
	}
	if err := g.box.ApplyPenalty(ctx, dim, subject, g.cfg.PenaltyTTL); err != nil {
		slog.WarnContext(c.Request.Context(), "traffic penalty apply failed",
			slog.String("dimension", dim), slog.String("error", err.Error()))
		return
	}
	observability.IncTrafficPenalty(dim)
	slog.WarnContext(c.Request.Context(), "traffic penalty applied",
		slog.String("dimension", dim),
		slog.String("subject", subject),
		slog.Int64("denies", count),
		slog.Duration("ttl", g.cfg.PenaltyTTL))
}
