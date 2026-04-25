package outbox

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"my_feed_system/internal/mq"
)

type Repo struct {
	db *gorm.DB
}

func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

func (r *Repo) Enqueue(tx *gorm.DB, event mq.Envelope) error {
	db := r.db
	if tx != nil {
		db = tx
	}

	row, err := NewMessage(event)
	if err != nil {
		return fmt.Errorf("build outbox message: %w", err)
	}

	return db.Create(row).Error
}

func (r *Repo) ClaimBatch(limit int, now time.Time, lockTimeout time.Duration) ([]Message, error) {
	if limit <= 0 {
		limit = 20
	}

	now = now.UTC()
	cutoff := now.Add(-lockTimeout)
	leaseToken := newLeaseToken()
	claimed := make([]Message, 0, limit)

	err := r.db.Transaction(func(tx *gorm.DB) error {
		var candidates []Message
		// 先筛出“到期可重试”的 pending 消息，以及“锁超时可接管”的 publishing 消息。
		if err := tx.
			Where("(status = ? AND next_attempt_at <= ?) OR (status = ? AND locked_at <= ?)",
				StatusPending, now, StatusPublishing, cutoff).
			Order("id ASC").
			Limit(limit).
			Find(&candidates).Error; err != nil {
			return err
		}
		if len(candidates) == 0 {
			return nil
		}

		ids := make([]uint64, 0, len(candidates))
		for _, item := range candidates {
			ids = append(ids, item.ID)
		}

		// 二次条件更新把候选消息 claim 成当前这轮处理，避免并发 Poller 重复消费。
		result := tx.Model(&Message{}).
			Where("id IN ?", ids).
			Where("(status = ? AND next_attempt_at <= ?) OR (status = ? AND locked_at <= ?)",
				StatusPending, now, StatusPublishing, cutoff).
			Updates(map[string]any{
				"status":          StatusPublishing,
				"locked_at":       now,
				"lease_token":     leaseToken,
				"last_error":      "",
				"attempt_count":   gorm.Expr("attempt_count + 1"),
				"next_attempt_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}

		return tx.Where("lease_token = ?", leaseToken).Order("id ASC").Find(&claimed).Error
	})
	if err != nil {
		return nil, err
	}

	return claimed, nil
}

func (r *Repo) Delete(id uint64) error {
	return r.db.Delete(&Message{}, id).Error
}

func (r *Repo) MarkPending(id uint64, retryAt time.Time, publishErr error) error {
	lastError := ""
	if publishErr != nil {
		lastError = publishErr.Error()
		if len(lastError) > 2000 {
			lastError = lastError[:2000]
		}
	}

	// 投递失败后不删记录，只回到 pending 并推迟下次重试时间。
	return r.db.Model(&Message{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":          StatusPending,
			"next_attempt_at": retryAt.UTC(),
			"locked_at":       nil,
			"lease_token":     "",
			"last_error":      lastError,
		}).Error
}

func newLeaseToken() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		// 随机数不可用时退化到时间戳 token，保证 claim 标记仍然可用。
		return strings.ReplaceAll(fmt.Sprintf("lease-%d", time.Now().UnixNano()), " ", "")
	}
	return hex.EncodeToString(b[:])
}
