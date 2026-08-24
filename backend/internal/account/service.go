package account

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	jwtv5 "github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"my_feed_system/internal/config"
)

var (
	ErrUsernameTaken     = errors.New("username already exists")
	ErrInvalidCredential = errors.New("invalid username or password")
	ErrAccountNotFound   = errors.New("account not found")
	ErrEmailInvalid      = errors.New("invalid email")
	ErrEmailCodeInvalid  = errors.New("invalid email code")
	ErrEmailCooldown     = errors.New("email code cooldown")
	ErrMailNotConfigured = errors.New("mail service not configured")
	ErrMailSendFailed    = errors.New("send email code failed")
	ErrEmailStoreMissing = errors.New("email otp store unavailable")
)

// Service 封装账号模块的核心业务逻辑。
type Service struct {
	repo       *Repo
	tokenCache *TokenCache
	jwtSecret  []byte
	otp        *OTPStore
	mailer     Mailer
	emailCfg   config.EmailAuthConfig
	passkeys   *PasskeySessionStore
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

// SetEmail 接入邮箱验证码登录。未设置时相关接口返回存储不可用。
func (s *Service) SetEmail(otp *OTPStore, mailer Mailer, cfg config.EmailAuthConfig) {
	s.otp = otp
	s.mailer = mailer
	s.emailCfg = cfg
}

// SetCreatedHook 在新建账号的同一事务里回调，用于发放注册赠金。
func (s *Service) SetCreatedHook(hook CreatedHook) {
	s.repo.SetCreatedHook(hook)
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
	if account == nil || account.Password == "" {
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

// UsernamesByIDs 给管理面批量补用户名，不带密码或 token。
func (s *Service) UsernamesByIDs(ids []uint64) (map[uint64]string, error) {
	return s.repo.FindUsernamesByIDs(ids)
}

// FindEmailSubject 返回账号绑定的邮箱，未绑定则空字符串。
func (s *Service) FindEmailSubject(accountID uint64) (string, error) {
	return s.repo.FindEmailSubject(accountID)
}

// FindByIdentity 按登录方式查找账号。
func (s *Service) FindByIdentity(provider string, subject string) (*Account, error) {
	account, err := s.repo.FindByIdentity(provider, subject)
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

	if account.Password == "" {
		return ErrInvalidCredential
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

// SendEmailCode 为邮箱签发验证码会话。测试域不发信，其它邮箱走 SMTP。
func (s *Service) SendEmailCode(ctx context.Context, req SendEmailCodeRequest) error {
	email := normalizeEmail(req.Email)
	if !isValidEmail(email) {
		return ErrEmailInvalid
	}
	if s.otp == nil || !s.otp.available() {
		return ErrEmailStoreMissing
	}

	allowed, err := s.otp.MarkCooldown(ctx, email)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrEmailCooldown
	}

	if s.isTestEmail(email) {
		return s.otp.SaveTest(ctx, email)
	}
	if s.mailer == nil || !s.mailer.Configured() {
		return ErrMailNotConfigured
	}

	code, err := randomDigits6()
	if err != nil {
		return err
	}
	if err := s.otp.SaveHash(ctx, email, hashOTP(email, code)); err != nil {
		return err
	}
	if err := s.mailer.SendCode(email, code, s.otpTTLMinutes()); err != nil {
		slog.ErrorContext(ctx, "send email otp failed", slog.String("error", err.Error()))
		return ErrMailSendFailed
	}
	return nil
}

// VerifyEmail 校验验证码并登录或创建账号。
func (s *Service) VerifyEmail(ctx context.Context, req VerifyEmailRequest) (*LoginResult, error) {
	email := normalizeEmail(req.Email)
	code := strings.TrimSpace(req.Code)
	if !isValidEmail(email) {
		return nil, ErrEmailInvalid
	}
	if !isValidCode(code) {
		return nil, ErrEmailCodeInvalid
	}
	if s.otp == nil || !s.otp.available() {
		return nil, ErrEmailStoreMissing
	}

	if s.isTestEmail(email) {
		ok, err := s.otp.ConsumeTest(ctx, email)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrEmailCodeInvalid
		}
	} else {
		ok, err := s.otp.MatchHash(ctx, email, hashOTP(email, code))
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrEmailCodeInvalid
		}
	}

	account, err := s.repo.FindByIdentity(ProviderEmail, email)
	if err != nil {
		return nil, err
	}
	created := false
	if account == nil {
		account, err = s.createEmailAccount(email)
		if err != nil {
			return nil, err
		}
		created = true
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
	if created {
		slog.InfoContext(ctx, "email account created", slog.Uint64("account_id", account.ID))
	}
	return &LoginResult{Account: account, Token: token, Created: created}, nil
}

func (s *Service) createEmailAccount(email string) (*Account, error) {
	base := usernameFromEmail(email)
	username, err := uniqueUsername(base, func(name string) (bool, error) {
		existing, err := s.repo.FindByUsername(name)
		if err != nil {
			return false, err
		}
		return existing != nil, nil
	})
	if err != nil {
		return nil, err
	}
	account := &Account{Username: username}
	if err := s.repo.CreateWithEmailIdentity(account, email); err != nil {
		return nil, err
	}
	return account, nil
}

func (s *Service) isTestEmail(email string) bool {
	return isTestEmailDomain(email, s.emailCfg.TestDomain)
}

// HasTestEmailIdentity 判断账号是否绑定了测试域邮箱。
// 运维台已改走审核员白名单，这个方法不再授权任何观测接口。
func (s *Service) HasTestEmailIdentity(accountID uint64) (bool, error) {
	email, err := s.repo.FindEmailSubject(accountID)
	if err != nil || email == "" {
		return false, err
	}
	return s.isTestEmail(email), nil
}

func (s *Service) otpTTLMinutes() int {
	seconds := s.emailCfg.CodeTTLSeconds
	if seconds <= 0 {
		return int(defaultOTPTTL / time.Minute)
	}
	minutes := seconds / 60
	if minutes < 1 {
		return 1
	}
	return minutes
}

func randomDigits6() (string, error) {
	var n uint32
	if err := binary.Read(rand.Reader, binary.BigEndian, &n); err != nil {
		return "", fmt.Errorf("generate otp: %w", err)
	}
	return fmt.Sprintf("%06d", n%1_000_000), nil
}

func (s *Service) writeTokenCache(accountID uint64, token string) {
	if s.tokenCache == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := s.tokenCache.Set(ctx, accountID, token); err != nil {
		slog.Warn("write token cache failed", slog.Uint64("account_id", accountID), slog.String("error", err.Error()))
	}
}

func (s *Service) deleteTokenCache(accountID uint64) {
	if s.tokenCache == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := s.tokenCache.Delete(ctx, accountID); err != nil {
		slog.Warn("delete token cache failed", slog.Uint64("account_id", accountID), slog.String("error", err.Error()))
	}
}
