package mq

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

type ProcessedMessage struct {
	ID            uint64    `gorm:"primaryKey"`
	ConsumerGroup string    `gorm:"size:64;not null;uniqueIndex:uk_processed_messages_consumer_event,priority:1"`
	EventID       string    `gorm:"size:64;not null;uniqueIndex:uk_processed_messages_consumer_event,priority:2"`
	EventType     string    `gorm:"size:64;not null"`
	Status        string    `gorm:"size:20;not null"`
	CreatedAt     time.Time `gorm:"autoCreateTime"`
}

func (ProcessedMessage) TableName() string {
	return "processed_messages"
}

var ErrAlreadyProcessed = errors.New("event already processed")

func MarkProcessed(tx *gorm.DB, consumerGroup string, event Envelope) error {
	// 依赖唯一索引 (consumer_group, event_id) 实现消费幂等。
	row := &ProcessedMessage{
		ConsumerGroup: consumerGroup,
		EventID:       event.EventID,
		EventType:     event.EventType,
		Status:        "done",
	}
	err := tx.Create(row).Error
	if err == nil {
		return nil
	}
	if isDuplicateError(err) {
		return ErrAlreadyProcessed
	}
	return err
}

func isDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Duplicate entry") || strings.Contains(msg, "UNIQUE constraint failed")
}
