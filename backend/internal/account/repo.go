package account

import (
	"errors"

	"gorm.io/gorm"
)

// Repo 负责账号表的数据库读写。
type Repo struct {
	db *gorm.DB
}

// NewRepo 创建账号仓储。
func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

// Create 插入一条新的账号记录。
func (r *Repo) Create(account *Account) error {
	return r.db.Create(account).Error
}

// FindByUsername 按用户名查询账号，未命中时返回 nil。
func (r *Repo) FindByUsername(username string) (*Account, error) {
	var account Account
	if err := r.db.Where("username = ?", username).First(&account).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return &account, nil
}

// FindByID 按主键查询账号，未命中时返回 nil。
func (r *Repo) FindByID(id uint64) (*Account, error) {
	var account Account
	if err := r.db.Where("id = ?", id).First(&account).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return &account, nil
}

// UpdateToken 更新账号当前持有的 token。
func (r *Repo) UpdateToken(id uint64, token string) error {
	return r.db.Model(&Account{}).Where("id = ?", id).Update("token", token).Error
}

// ClearToken 清空账号 token。
func (r *Repo) ClearToken(id uint64) error {
	return r.db.Model(&Account{}).Where("id = ?", id).Update("token", "").Error
}

// UpdatePasswordAndToken 同时更新密码和 token，便于修改密码后使旧 token 失效。
func (r *Repo) UpdatePasswordAndToken(id uint64, password string, token string) error {
	return r.db.Model(&Account{}).Where("id = ?", id).Updates(map[string]any{
		"password": password,
		"token":    token,
	}).Error
}

// UpdateUsernameAndToken 同时更新用户名和 token，保持 JWT 载荷与数据库一致。
func (r *Repo) UpdateUsernameAndToken(id uint64, username string, token string) error {
	return r.db.Model(&Account{}).Where("id = ?", id).Updates(map[string]any{
		"username": username,
		"token":    token,
	}).Error
}
