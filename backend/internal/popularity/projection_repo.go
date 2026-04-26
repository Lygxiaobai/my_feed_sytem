package popularity

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

type ProjectionRepo struct {
	db *gorm.DB
}

func NewProjectionRepo(db *gorm.DB) *ProjectionRepo {
	return &ProjectionRepo{db: db}
}

func (r *ProjectionRepo) Enqueue(tx *gorm.DB, row *Projection) error {
	db := r.db
	if tx != nil {
		db = tx
	}
	return db.Create(row).Error
}

func (r *ProjectionRepo) ClaimBatch(limit int, now time.Time, lockTimeout time.Duration) ([]Projection, error) {
	if limit <= 0 {
		limit = 20
	}

	now = now.UTC()
	cutoff := now.Add(-lockTimeout)
	leaseToken := newProjectionLeaseToken()
	claimed := make([]Projection, 0, limit)

	err := r.db.Transaction(func(tx *gorm.DB) error {
		var candidates []Projection
		if err := tx.
			Where("(status = ? AND next_attempt_at <= ?) OR (status = ? AND locked_at <= ?)",
				projectionStatusPending, now, projectionStatusApplying, cutoff).
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

		result := tx.Model(&Projection{}).
			Where("id IN ?", ids).
			Where("(status = ? AND next_attempt_at <= ?) OR (status = ? AND locked_at <= ?)",
				projectionStatusPending, now, projectionStatusApplying, cutoff).
			Updates(map[string]any{
				"status":          projectionStatusApplying,
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

func (r *ProjectionRepo) Delete(id uint64) error {
	return r.db.Delete(&Projection{}, id).Error
}

func (r *ProjectionRepo) MarkPending(id uint64, retryAt time.Time, applyErr error) error {
	lastError := ""
	if applyErr != nil {
		lastError = applyErr.Error()
		if len(lastError) > 2000 {
			lastError = lastError[:2000]
		}
	}

	return r.db.Model(&Projection{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":          projectionStatusPending,
			"next_attempt_at": retryAt.UTC(),
			"locked_at":       nil,
			"lease_token":     "",
			"last_error":      lastError,
		}).Error
}

func newProjectionLeaseToken() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strings.ReplaceAll(fmt.Sprintf("lease-%d", time.Now().UnixNano()), " ", "")
	}
	return hex.EncodeToString(b[:])
}
