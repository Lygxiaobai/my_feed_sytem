package account

import (
	"time"

	"my_feed_system/internal/tag"
)

const (
	ProviderEmail  = "email"
	ProviderWechat = "wechat"
	ProviderQQ     = "qq"
)

type Account struct {
	ID       uint64 `gorm:"primaryKey" json:"id"`
	Username string `gorm:"size:64;not null;uniqueIndex" json:"username"`
	Password string `gorm:"size:255" json:"-"`
	Token    string `gorm:"size:512" json:"token,omitempty"`
	// FollowerCount 是粉丝数冗余计数，关注流的推拉分级完全依赖它。
	// 不实时 COUNT social_relations 的原因：那正是要避免的大表聚合，
	// 而分级判定处在视频发布的写路径上，每次发布都要读一次。
	FollowerCount int64 `gorm:"not null;default:0" json:"follower_count"`
	// Interests 存 JSON 字符串数组，最多 7 个 tag。不进公开账号响应。
	// 用 tag.List 是因为存量行是 NULL，扫进 string 会让登录推荐直接 500。
	Interests tag.List `gorm:"type:text" json:"-"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Identity 把一种登录方式绑到账号上。同一 provider+subject 只能对应一个账号。
type Identity struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	AccountID uint64    `gorm:"index;not null" json:"account_id"`
	Provider  string    `gorm:"size:32;not null;uniqueIndex:uk_ident_prov_subj" json:"provider"`
	Subject   string    `gorm:"size:191;not null;uniqueIndex:uk_ident_prov_subj" json:"subject"`
	CreatedAt time.Time `json:"created_at"`
}

func (Identity) TableName() string {
	return "account_identities"
}

func (Account) TableName() string {
	return "accounts"
}

type RegisterRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type LoginResult struct {
	Account *Account `json:"account"`
	Token   string   `json:"token"`
	Created bool     `json:"created,omitempty"`
}

type FindByIDRequest struct {
	ID uint64 `json:"id" binding:"required"`
}

type FindByUsernameRequest struct {
	Username string `json:"username" binding:"required"`
}

type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

type RenameRequest struct {
	NewUsername string `json:"new_username" binding:"required"`
}

type SendEmailCodeRequest struct {
	Email string `json:"email" binding:"required"`
}

type VerifyEmailRequest struct {
	Email string `json:"email" binding:"required"`
	Code  string `json:"code" binding:"required"`
}
