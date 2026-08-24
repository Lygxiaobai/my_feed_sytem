package account

import (
	"errors"

	"gorm.io/gorm"
)

// CreatedHook 在账号插入成功后、同一事务内执行。
// 用来发放注册赠金，避免出现「账号已建、赠金没到」的半成品。
type CreatedHook func(tx *gorm.DB, accountID uint64) error

// Repo 负责账号表的数据库读写。
type Repo struct {
	db        *gorm.DB
	onCreated CreatedHook
}

// NewRepo 创建账号仓储。
func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

func (r *Repo) SetCreatedHook(hook CreatedHook) {
	r.onCreated = hook
}

// Create 插入一条新的账号记录。
func (r *Repo) Create(account *Account) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(account).Error; err != nil {
			return err
		}
		if r.onCreated != nil {
			return r.onCreated(tx, account.ID)
		}
		return nil
	})
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

// FindUsernamesByIDs 只取公开用户名，不读密码或 token。
func (r *Repo) FindUsernamesByIDs(ids []uint64) (map[uint64]string, error) {
	out := make(map[uint64]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []Account
	if err := r.db.Select("id, username").Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		out[rows[i].ID] = rows[i].Username
	}
	return out, nil
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

// FindEmailSubject 返回账号绑定的邮箱。未绑定则空字符串。
func (r *Repo) FindEmailSubject(accountID uint64) (string, error) {
	var identity Identity
	if err := r.db.Where("account_id = ? AND provider = ?", accountID, ProviderEmail).First(&identity).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}
	return identity.Subject, nil
}

// FindByIdentity 按登录方式查找账号。
func (r *Repo) FindByIdentity(provider string, subject string) (*Account, error) {
	var identity Identity
	if err := r.db.Where("provider = ? AND subject = ?", provider, subject).First(&identity).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return r.FindByID(identity.AccountID)
}

// CreateWithEmailIdentity 在同一事务里创建账号并绑定邮箱。
func (r *Repo) CreateWithEmailIdentity(account *Account, email string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(account).Error; err != nil {
			return err
		}
		identity := Identity{
			AccountID: account.ID,
			Provider:  ProviderEmail,
			Subject:   email,
		}
		if err := tx.Create(&identity).Error; err != nil {
			return err
		}
		if r.onCreated != nil {
			return r.onCreated(tx, account.ID)
		}
		return nil
	})
}
