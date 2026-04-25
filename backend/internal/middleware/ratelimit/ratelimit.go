package ratelimit

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const (
	defaultMessage = "too many requests"
	defaultTimeout = 200 * time.Millisecond
)

// Result describes the current request outcome under a fixed-window limit.
type Result struct {
	Allowed    bool
	Count      int64
	Remaining  int64
	RetryAfter time.Duration
}

// Checker defines the limiter contract used by the Gin middleware.
type Checker interface {
	Allow(ctx context.Context, scope string, subject string, limit int64, window time.Duration) (Result, error)
}

// FixedWindow implements a Redis INCR + EXPIRE based fixed-window rate limiter.
type FixedWindow struct {
	client redis.Cmdable
	prefix string
}

// Policy 描述一条限流规则。
type Policy struct {
	Name     string
	Limit    int64
	Window   time.Duration
	Timeout  time.Duration
	Message  string
	FailOpen bool
}

// NewFixedWindow creates a Redis-backed fixed-window limiter.
func NewFixedWindow(client redis.Cmdable) *FixedWindow {
	return &FixedWindow{
		client: client,
		prefix: "ratelimit",
	}
}

// Allow 使用 Redis 的 INCR + EXPIRE 实现固定窗口计数。
// 首次命中时设置 TTL，后续窗口内继续累加；超过阈值则返回剩余等待时间。
func (l *FixedWindow) Allow(ctx context.Context, scope string, subject string, limit int64, window time.Duration) (Result, error) {
	if l == nil || l.client == nil {
		return Result{}, fmt.Errorf("rate limit store unavailable")
	}
	if limit <= 0 {
		return Result{Allowed: true}, nil
	}
	if window <= 0 {
		return Result{}, fmt.Errorf("invalid rate limit window")
	}

	key := fmt.Sprintf("%s:%s:%s", l.prefix, scope, subject)

	count, err := l.client.Incr(ctx, key).Result()
	if err != nil {
		return Result{}, fmt.Errorf("increment rate limit counter: %w", err)
	}

	if count == 1 {
		// 只有窗口内第一次请求才需要补 TTL，避免每次请求都把窗口往后推。
		if err := l.client.Expire(ctx, key, window).Err(); err != nil {
			// 如果 TTL 设置失败，主动删除刚写入的计数，避免生成永不过期的脏 key。
			_ = l.client.Del(context.Background(), key).Err()
			return Result{}, fmt.Errorf("set rate limit ttl: %w", err)
		}
	}

	result := Result{
		Allowed:   count <= limit,
		Count:     count,
		Remaining: maxInt64(0, limit-count),
	}
	if result.Allowed {
		return result, nil
	}

	ttl, err := l.client.TTL(ctx, key).Result()
	if err != nil || ttl <= 0 {
		ttl = window
	}
	result.RetryAfter = ttl
	return result, nil
}

// ByIP returns a middleware that limits requests by client IP.
func ByIP(checker Checker, policy Policy) gin.HandlerFunc {
	return newMiddleware(checker, policy, func(c *gin.Context) (string, bool) {
		ip := c.ClientIP()
		if ip == "" {
			return "", false
		}
		return ip, true
	})
}

// ByAccountID returns a middleware that limits requests by authenticated account ID.
func ByAccountID(checker Checker, policy Policy) gin.HandlerFunc {
	return newMiddleware(checker, policy, func(c *gin.Context) (string, bool) {
		accountID := c.GetUint64("account_id")
		if accountID == 0 {
			// 未鉴权请求不参与账号维度限流，由上游鉴权中间件决定是否放行。
			return "", false
		}
		return strconv.FormatUint(accountID, 10), true
	})
}

func newMiddleware(checker Checker, policy Policy, resolveSubject func(c *gin.Context) (string, bool)) gin.HandlerFunc {
	policy = normalizePolicy(policy)
	if checker == nil || policy.Limit <= 0 {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	return func(c *gin.Context) {
		subject, ok := resolveSubject(c)
		if !ok {
			c.Next()
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), policy.Timeout)
		result, err := checker.Allow(ctx, policy.Name, subject, policy.Limit, policy.Window)
		cancel()
		if err != nil {
			if policy.FailOpen {
				// Redis 异常时默认降级放行，避免限流组件故障拖垮主业务链路。
				log.Printf("ratelimit bypassed: scope=%s subject=%s err=%v", policy.Name, subject, err)
				c.Next()
				return
			}

			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"message": "rate limit unavailable",
			})
			return
		}

		if result.Allowed {
			c.Next()
			return
		}

		// Retry-After 让客户端知道最早何时可以重试，方便做退避控制。
		retryAfterSeconds := int(math.Ceil(result.RetryAfter.Seconds()))
		if retryAfterSeconds < 1 {
			retryAfterSeconds = 1
		}
		c.Header("Retry-After", strconv.Itoa(retryAfterSeconds))
		c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
			"message": policy.Message,
		})
	}
}

func normalizePolicy(policy Policy) Policy {
	if policy.Message == "" {
		policy.Message = defaultMessage
	}
	if policy.Timeout <= 0 {
		policy.Timeout = defaultTimeout
	}
	if policy.Window <= 0 {
		policy.Window = time.Minute
	}
	if policy.Name == "" {
		policy.Name = "default"
	}
	return policy
}

func maxInt64(a int64, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
