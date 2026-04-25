package account

import "time"

type Account struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	Username  string    `gorm:"size:64;not null;uniqueIndex" json:"username"`
	Password  string    `gorm:"size:255;not null" json:"-"`
	Token     string    `gorm:"size:512" json:"token,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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
