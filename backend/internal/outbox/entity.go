package outbox

import (
	"encoding/json"
	"time"

	"my_feed_system/internal/mq"
)

const (
	StatusPending    = "pending"
	StatusPublishing = "publishing"
)

// Message stores business events that must be published after the local transaction commits.
type Message struct {
	ID         uint64    `gorm:"primaryKey"`
	EventID    string    `gorm:"size:64;not null;uniqueIndex:uk_outbox_messages_event_id"`
	EventType  string    `gorm:"size:64;not null;index:idx_outbox_messages_status_next_attempt,priority:4"`
	Producer   string    `gorm:"size:64;not null"`
	OccurredAt time.Time `gorm:"not null"`
	Version    int       `gorm:"not null"`
	Payload    string    `gorm:"type:longtext;not null"`
	// Status/NextAttemptAt/LockedAt 一起描述这条消息是否可被 Poller 继续领取。
	Status        string     `gorm:"size:20;not null;index:idx_outbox_messages_status_next_attempt,priority:1"`
	AttemptCount  int        `gorm:"not null;default:0"`
	NextAttemptAt time.Time  `gorm:"not null;index:idx_outbox_messages_status_next_attempt,priority:2"`
	LockedAt      *time.Time `gorm:"index:idx_outbox_messages_status_next_attempt,priority:3"`
	// LeaseToken 用来标记本轮 claim 到的消息，避免并发 Poller 误拿到同一批记录。
	LeaseToken string    `gorm:"size:64;not null;default:'';index:idx_outbox_messages_lease_token"`
	LastError  string    `gorm:"type:text"`
	CreatedAt  time.Time `gorm:"autoCreateTime"`
	UpdatedAt  time.Time `gorm:"autoUpdateTime"`
}

func (Message) TableName() string {
	return "outbox_messages"
}

func NewMessage(event mq.Envelope) (*Message, error) {
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	return &Message{
		EventID:       event.EventID,
		EventType:     event.EventType,
		Producer:      event.Producer,
		OccurredAt:    event.OccurredAt,
		Version:       event.Version,
		Payload:       string(payload),
		Status:        StatusPending,
		NextAttemptAt: now,
	}, nil
}

func (m Message) Envelope() mq.Envelope {
	// Outbox 回放时需要尽量还原最初落库的事件内容，而不是重新生成一份新事件。
	return mq.Envelope{
		EventID:    m.EventID,
		EventType:  m.EventType,
		OccurredAt: m.OccurredAt,
		Producer:   m.Producer,
		Version:    m.Version,
		Payload:    json.RawMessage(m.Payload),
	}
}
