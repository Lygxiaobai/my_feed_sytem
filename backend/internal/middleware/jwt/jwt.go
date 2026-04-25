package jwt

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	jwtv5 "github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"

	"my_feed_system/internal/account"
)

func JWTAuth(db *gorm.DB, secret string) gin.HandlerFunc {
	return JWTAuthWithTokenCache(db, nil, secret)
}

// JWTAuthWithTokenCache 优先从 Redis 校验 token，未命中时再回源 MySQL。
func JWTAuthWithTokenCache(db *gorm.DB, tokenCache *account.TokenCache, secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "missing bearer token"})
			c.Abort()
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		token, err := jwtv5.Parse(tokenString, func(token *jwtv5.Token) (interface{}, error) {
			if token.Method != jwtv5.SigningMethodHS256 {
				return nil, jwtv5.ErrTokenSignatureInvalid
			}
			return []byte(secret), nil
		})
		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "invalid token"})
			c.Abort()
			return
		}

		claims, ok := token.Claims.(jwtv5.MapClaims)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "invalid token claims"})
			c.Abort()
			return
		}

		accountIDValue, ok := claims["account_id"].(float64)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "invalid token payload"})
			c.Abort()
			return
		}

		accountID := uint64(accountIDValue)
		username, _ := claims["username"].(string)
		repo := account.NewRepo(db)

		if tokenCache != nil {
			ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
			cachedToken, ok, err := tokenCache.Get(ctx, accountID)
			cancel()
			if err != nil {
				log.Printf("jwt auth: read token cache failed for account %d: %v", accountID, err)
			} else if ok {
				if cachedToken != tokenString {
					c.JSON(http.StatusUnauthorized, gin.H{"message": "token expired"})
					c.Abort()
					return
				}

				if username == "" {
					currentAccount, err := repo.FindByID(accountID)
					if err != nil || currentAccount == nil {
						c.JSON(http.StatusUnauthorized, gin.H{"message": "account not found"})
						c.Abort()
						return
					}
					username = currentAccount.Username
				}

				c.Set("account_id", accountID)
				c.Set("account_username", username)
				c.Next()
				return
			}
		}

		currentAccount, err := repo.FindByID(accountID)
		if err != nil || currentAccount == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "account not found"})
			c.Abort()
			return
		}

		if currentAccount.Token != tokenString {
			c.JSON(http.StatusUnauthorized, gin.H{"message": "token expired"})
			c.Abort()
			return
		}

		if tokenCache != nil {
			ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
			if err := tokenCache.Set(ctx, accountID, tokenString); err != nil {
				log.Printf("jwt auth: refill token cache failed for account %d: %v", accountID, err)
			}
			cancel()
		}

		c.Set("account_id", currentAccount.ID)
		c.Set("account_username", currentAccount.Username)
		c.Next()
	}
}
