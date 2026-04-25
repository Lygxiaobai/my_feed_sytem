package mq

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

const (
	MessageVersionV1 = 1

	// ProducerAPIServer 表示事件由 API 请求路径产生。
	ProducerAPIServer = "api-server"
	// ProducerWorker 表示事件由 Worker 消费后继续派生产生。
	ProducerWorker = "worker"
)

const (
	EventTypeLikeCreated       = "like.created"
	EventTypeLikeDeleted       = "like.deleted"
	EventTypeCommentCreated    = "comment.created"
	EventTypeCommentDeleted    = "comment.deleted"
	EventTypeSocialFollowed    = "social.followed"
	EventTypeSocialUnfollowed  = "social.unfollowed"
	EventTypePopularityChanged = "popularity.changed"
	EventTypeVideoTimelinePush = "video.timeline.publish"
)

type Envelope struct {
	// EventID 全局唯一，用于消费端幂等去重。
	EventID    string    `json:"event_id"`
	EventType  string    `json:"event_type"`
	OccurredAt time.Time `json:"occurred_at"`
	Producer   string    `json:"producer"`
	// Version 预留给后续消息结构升级。
	Version int             `json:"version"`
	Payload json.RawMessage `json:"payload"`
}

func NewEnvelope(eventType string, producer string, payload any) (Envelope, error) {
	payloadRaw, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, fmt.Errorf("marshal payload: %w", err)
	}

	return Envelope{
		EventID:    newEventID(),
		EventType:  eventType,
		OccurredAt: time.Now().UTC(),
		Producer:   producer,
		Version:    MessageVersionV1,
		Payload:    payloadRaw,
	}, nil
}

func (e Envelope) DecodePayload(dst any) error {
	return json.Unmarshal(e.Payload, dst)
}

func newEventID() string {
	// 时间戳 + 随机串，兼顾可排序性与低碰撞概率。
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%d-%s", time.Now().UnixNano(), hex.EncodeToString(b[:]))
}

type LikePayload struct {
	AccountID uint64 `json:"account_id"`
	VideoID   uint64 `json:"video_id"`
}

type CommentCreatedPayload struct {
	CommentID       uint64 `json:"comment_id"`
	VideoID         uint64 `json:"video_id"`
	AuthorID        uint64 `json:"author_id"`
	Username        string `json:"username"`
	Content         string `json:"content"`
	ParentCommentID uint64 `json:"parent_comment_id"`
	RootCommentID   uint64 `json:"root_comment_id"`
	ReplyToUserID   uint64 `json:"reply_to_user_id"`
	ReplyToUsername string `json:"reply_to_username"`
}

type CommentDeletedPayload struct {
	CommentID  uint64 `json:"comment_id"`
	VideoID    uint64 `json:"video_id"`
	OperatorID uint64 `json:"operator_id"`
}

type SocialPayload struct {
	FollowerID uint64 `json:"follower_id"`
	VloggerID  uint64 `json:"vlogger_id"`
}

type PopularityChangedPayload struct {
	VideoID uint64 `json:"video_id"`
	Delta   int64  `json:"delta"`
	Reason  string `json:"reason"`
}

type VideoTimelinePayload struct {
	VideoID   uint64    `json:"video_id"`
	AuthorID  uint64    `json:"author_id"`
	CreatedAt time.Time `json:"created_at"`
}
