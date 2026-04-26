package popularity

import (
	"time"

	"my_feed_system/internal/mq"
)

const (
	projectionStatusPending  = "pending"
	projectionStatusApplying = "applying"
)

// Projection stores popularity deltas that still need to be projected into Redis.
type Projection struct {
	ID            uint64     `gorm:"primaryKey"`
	EventID       string     `gorm:"size:64;not null;uniqueIndex:uk_popularity_projections_event_id"`
	VideoID       uint64     `gorm:"not null;index:idx_popularity_projections_status_next_attempt,priority:4"`
	Delta         int64      `gorm:"not null"`
	OccurredAt    time.Time  `gorm:"not null"`
	Status        string     `gorm:"size:20;not null;index:idx_popularity_projections_status_next_attempt,priority:1"`
	AttemptCount  int        `gorm:"not null;default:0"`
	NextAttemptAt time.Time  `gorm:"not null;index:idx_popularity_projections_status_next_attempt,priority:2"`
	LockedAt      *time.Time `gorm:"index:idx_popularity_projections_status_next_attempt,priority:3"`
	LeaseToken    string     `gorm:"size:64;not null;default:'';index:idx_popularity_projections_lease_token"`
	LastError     string     `gorm:"type:text"`
	CreatedAt     time.Time  `gorm:"autoCreateTime"`
	UpdatedAt     time.Time  `gorm:"autoUpdateTime"`
}

func (Projection) TableName() string {
	return "popularity_projections"
}

func NewProjection(event mq.Envelope, payload mq.PopularityChangedPayload, occurredAt time.Time) *Projection {
	return &Projection{
		EventID:       event.EventID,
		VideoID:       payload.VideoID,
		Delta:         payload.Delta,
		OccurredAt:    occurredAt.UTC(),
		Status:        projectionStatusPending,
		NextAttemptAt: time.Now().UTC(),
	}
}
