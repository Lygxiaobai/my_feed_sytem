package account

import (
	"context"
	"errors"
	"log"
	"time"

	jwtv5 "github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var (
	ErrUsernameTaken     = errors.New("username already exists")
	ErrInvalidCredential = errors.New("invalid username or password")
	ErrAccountNotFound   = errors.New("account not found")
)

// Service 封装账号模块的核心业务逻辑。
type Service struct {
	repo       *Repo
	tokenCache *TokenCache
	jwtSecret  []byte
}

// NewService 创建账号服务。
func NewService(db *gorm.DB, jwtSecret string) *Service {
	return NewServiceWithTokenCache(db, nil, jwtSecret)
}

// NewServiceWithTokenCache 创建带 token 缓存能力的账号服务。
func NewServiceWithTokenCache(db *gorm.DB, tokenCache *TokenCache, jwtSecret string) *Service {
	return &Service{
		repo:       NewRepo(db),
		tokenCache: tokenCache,
		jwtSecret:  []byte(jwtSecret),
	}
}

// Register 创建新账号，并在写入前做用户名唯一性校验。
func (s *Service) Register(req RegisterRequest) (*Account, error) {
	existing, err := s.repo.FindByUsername(req.Username)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrUsernameTaken
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	account := &Account{
		Username: req.Username,
		Password: string(hashedPassword),
	}
	if err := s.repo.Create(account); err != nil {
		return nil, err
	}

	return account, nil
}

// Login 校验密码并生成新的 JWT，同时把 token 持久化到数据库。
func (s *Service) Login(req LoginRequest) (*LoginResult, error) {
	account, err := s.repo.FindByUsername(req.Username)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, ErrInvalidCredential
	}

	if err := bcrypt.CompareHashAndPassword([]byte(account.Password), []byte(req.Password)); err != nil {
		return nil, ErrInvalidCredential
	}

	token, err := s.generateToken(account)
	if err != nil {
		return nil, err
	}

	if err := s.repo.UpdateToken(account.ID, token); err != nil {
		return nil, err
	}
	account.Token = token
	s.writeTokenCache(account.ID, token)

	return &LoginResult{Account: account, Token: token}, nil
}

// FindByID 按 ID 查询账号。
func (s *Service) FindByID(req FindByIDRequest) (*Account, error) {
	account, err := s.repo.FindByID(req.ID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, ErrAccountNotFound
	}

	return account, nil
}

// FindByUsername 按用户名查询账号。
func (s *Service) FindByUsername(req FindByUsernameRequest) (*Account, error) {
	account, err := s.repo.FindByUsername(req.Username)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, ErrAccountNotFound
	}

	return account, nil
}

// Logout 清空指定账号的 token。
func (s *Service) Logout(accountID uint64) error {
	account, err := s.repo.FindByID(accountID)
	if err != nil {
		return err
	}
	if account == nil {
		return ErrAccountNotFound
	}

	if err := s.repo.ClearToken(accountID); err != nil {
		return err
	}
	s.deleteTokenCache(accountID)
	return nil
}

// ChangePassword 验证旧密码后更新密码，并清空 token 迫使重新登录。
func (s *Service) ChangePassword(accountID uint64, req ChangePasswordRequest) error {
	account, err := s.repo.FindByID(accountID)
	if err != nil {
		return err
	}
	if account == nil {
		return ErrAccountNotFound
	}

	if err := bcrypt.CompareHashAndPassword([]byte(account.Password), []byte(req.OldPassword)); err != nil {
		return ErrInvalidCredential
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	if err := s.repo.UpdatePasswordAndToken(accountID, string(hashedPassword), ""); err != nil {
		return err
	}
	s.deleteTokenCache(accountID)
	return nil
}

// Rename 更新用户名并重新签发 token，避免 JWT 中的用户名过期。
func (s *Service) Rename(accountID uint64, req RenameRequest) (*LoginResult, error) {
	account, err := s.repo.FindByID(accountID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, ErrAccountNotFound
	}

	existing, err := s.repo.FindByUsername(req.NewUsername)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.ID != accountID {
		return nil, ErrUsernameTaken
	}

	account.Username = req.NewUsername
	token, err := s.generateToken(account)
	if err != nil {
		return nil, err
	}

	if err := s.repo.UpdateUsernameAndToken(accountID, req.NewUsername, token); err != nil {
		return nil, err
	}
	account.Token = token
	s.writeTokenCache(accountID, token)

	return &LoginResult{Account: account, Token: token}, nil
}

// generateToken 生成仅包含账号 ID 和用户名的 JWT。
func (s *Service) generateToken(account *Account) (string, error) {
	token := jwtv5.NewWithClaims(jwtv5.SigningMethodHS256, jwtv5.MapClaims{
		"account_id": account.ID,
		"username":   account.Username,
	})

	return token.SignedString(s.jwtSecret)
}

func (s *Service) writeTokenCache(accountID uint64, token string) {
	if s.tokenCache == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := s.tokenCache.Set(ctx, accountID, token); err != nil {
		log.Printf("account service: write token cache failed for account %d: %v", accountID, err)
	}
}

func (s *Service) deleteTokenCache(accountID uint64) {
	if s.tokenCache == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := s.tokenCache.Delete(ctx, accountID); err != nil {
		log.Printf("account service: delete token cache failed for account %d: %v", accountID, err)
	}
}
