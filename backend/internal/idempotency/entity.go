package idempotency

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	StatusProcessing = "processing"
	StatusDone       = "done"
)

var ErrNotFound = errors.New("idempotency key not found")

// Key stores request-level idempotency state for write APIs.
type Key struct {
	ID uint64 `gorm:"primaryKey"`
	// 作用域唯一键：同一账号、同一业务、同一个幂等键只允许占一条记录。
	AccountID    uint64    `gorm:"not null;uniqueIndex:uk_idempotency_keys_account_biz_key,priority:1"`
	BizType      string    `gorm:"size:64;not null;uniqueIndex:uk_idempotency_keys_account_biz_key,priority:2"`
	IdemKey      string    `gorm:"column:idem_key;size:128;not null;uniqueIndex:uk_idempotency_keys_account_biz_key,priority:3"`
	RequestHash  string    `gorm:"size:64;not null"`
	ResourceID   uint64    `gorm:"not null;default:0"`
	Status       string    `gorm:"size:20;not null"`
	ResponseJSON string    `gorm:"type:longtext"`
	CreatedAt    time.Time `gorm:"autoCreateTime"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime"`
}

func (Key) TableName() string {
	return "idempotency_keys"
}

type Repo struct {
	db *gorm.DB
}

func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

// CreateProcessing tries to reserve an idempotency slot. inserted=false means the key already exists.
func (r *Repo) CreateProcessing(tx *gorm.DB, row *Key) (inserted bool, err error) {
	db := r.db
	if tx != nil {
		db = tx
	}

	// 依赖唯一索引抢占幂等槽位，冲突时不报错，交给上层根据已有记录继续判断。
	result := db.Clauses(clause.OnConflict{DoNothing: true}).Create(row)
	if result.Error != nil {
		return false, result.Error
	}

	return result.RowsAffected == 1, nil
}

func (r *Repo) FindByScope(tx *gorm.DB, accountID uint64, bizType string, idemKey string) (*Key, error) {
	db := r.db
	if tx != nil {
		db = tx
	}

	// 按业务作用域精确读取幂等记录，供冲突请求做摘要校验和结果回放。
	var row Key
	if err := db.Where("account_id = ? AND biz_type = ? AND idem_key = ?", accountID, bizType, idemKey).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &row, nil
}

func (r *Repo) MarkDone(tx *gorm.DB, id uint64, resourceID uint64, responseJSON string) error {
	db := r.db
	if tx != nil {
		db = tx
	}

	// 只在创建资源成功后落 done，后续重试请求即可直接回放这次结果。
	return db.Model(&Key{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":        StatusDone,
			"resource_id":   resourceID,
			"response_json": responseJSON,
		}).Error
}
