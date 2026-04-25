package account

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultTokenCacheTTL = 24 * time.Hour

// TokenCache 封装账号 token 的 Redis 缓存读写。
type TokenCache struct {
	client redis.Cmdable
	ttl    time.Duration
}

// NewTokenCache 使用默认 TTL 创建 token 缓存。
func NewTokenCache(client redis.Cmdable) *TokenCache {
	return NewTokenCacheWithTTL(client, defaultTokenCacheTTL)
}

// NewTokenCacheWithTTL 使用指定 TTL 创建 token 缓存。
func NewTokenCacheWithTTL(client redis.Cmdable, ttl time.Duration) *TokenCache {
	if ttl <= 0 {
		ttl = defaultTokenCacheTTL
	}

	return &TokenCache{
		client: client,
		ttl:    ttl,
	}
}

// Enabled 判断 token 缓存是否可用。
func (c *TokenCache) Enabled() bool {
	return c != nil && c.client != nil
}

// Set 写入账号当前有效 token。
func (c *TokenCache) Set(ctx context.Context, accountID uint64, token string) error {
	if !c.Enabled() {
		return nil
	}

	return c.client.Set(ctx, c.key(accountID), token, c.ttl).Err()
}

// Get 读取账号当前有效 token；未命中时 ok=false。
func (c *TokenCache) Get(ctx context.Context, accountID uint64) (token string, ok bool, err error) {
	if !c.Enabled() {
		return "", false, nil
	}

	token, err = c.client.Get(ctx, c.key(accountID)).Result()
	if err != nil {
		if err == redis.Nil {
			return "", false, nil
		}
		return "", false, err
	}

	return token, true, nil
}

// Delete 删除账号 token 缓存。
func (c *TokenCache) Delete(ctx context.Context, accountID uint64) error {
	if !c.Enabled() {
		return nil
	}

	return c.client.Del(ctx, c.key(accountID)).Err()
}

func (c *TokenCache) key(accountID uint64) string {
	return fmt.Sprintf("account:token:%d", accountID)
}
