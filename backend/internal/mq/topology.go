package mq

import (
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	ExchangeLikeEvents       = "like.events"
	ExchangeCommentEvents    = "comment.events"
	ExchangeSocialEvents     = "social.events"
	ExchangePopularityEvents = "popularity.events"
	ExchangeVideoTimeline    = "video.timeline.events"
)

const (
	QueueLikeWrite        = "like.write.q"
	QueueCommentWrite     = "comment.write.q"
	QueueSocialWrite      = "social.write.q"
	QueuePopularityUpdate = "popularity.update.q"
	QueueTimelineUpdate   = "timeline.update.q"
)

const (
	QueueLikeWriteDLQ        = "like.write.dlq"
	QueueCommentWriteDLQ     = "comment.write.dlq"
	QueueSocialWriteDLQ      = "social.write.dlq"
	QueuePopularityUpdateDLQ = "popularity.update.dlq"
	QueueTimelineUpdateDLQ   = "timeline.update.dlq"
)

const (
	consumerGroupLike       = "like-worker"
	consumerGroupComment    = "comment-worker"
	consumerGroupSocial     = "social-worker"
	consumerGroupPopularity = "popularity-worker"
	consumerGroupTimeline   = "timeline-worker"
)

const (
	LikeDLX       = "like.events.dlx"
	CommentDLX    = "comment.events.dlx"
	SocialDLX     = "social.events.dlx"
	PopularityDLX = "popularity.events.dlx"
	TimelineDLX   = "video.timeline.events.dlx"
)

type QueueSpec struct {
	Exchange     string
	ExchangeType string
	Queue        string
	DLX          string
	DLQ          string
	DLRoutingKey string
	BindingKeys  []string
}

// queueSpecs 定义每个业务域的主队列与死信队列配对关系。
var queueSpecs = []QueueSpec{
	{
		Exchange:     ExchangeLikeEvents,
		ExchangeType: "topic",
		Queue:        QueueLikeWrite,
		DLX:          LikeDLX,
		DLQ:          QueueLikeWriteDLQ,
		DLRoutingKey: "like.write.failed",
		BindingKeys: []string{
			EventTypeLikeCreated,
			EventTypeLikeDeleted,
		},
	},
	{
		Exchange:     ExchangeCommentEvents,
		ExchangeType: "topic",
		Queue:        QueueCommentWrite,
		DLX:          CommentDLX,
		DLQ:          QueueCommentWriteDLQ,
		DLRoutingKey: "comment.write.failed",
		BindingKeys: []string{
			EventTypeCommentCreated,
			EventTypeCommentDeleted,
		},
	},
	{
		Exchange:     ExchangeSocialEvents,
		ExchangeType: "topic",
		Queue:        QueueSocialWrite,
		DLX:          SocialDLX,
		DLQ:          QueueSocialWriteDLQ,
		DLRoutingKey: "social.write.failed",
		BindingKeys: []string{
			EventTypeSocialFollowed,
			EventTypeSocialUnfollowed,
		},
	},
	{
		Exchange:     ExchangePopularityEvents,
		ExchangeType: "topic",
		Queue:        QueuePopularityUpdate,
		DLX:          PopularityDLX,
		DLQ:          QueuePopularityUpdateDLQ,
		DLRoutingKey: "popularity.write.failed",
		BindingKeys: []string{
			EventTypePopularityChanged,
		},
	},
	{
		Exchange:     ExchangeVideoTimeline,
		ExchangeType: "topic",
		Queue:        QueueTimelineUpdate,
		DLX:          TimelineDLX,
		DLQ:          QueueTimelineUpdateDLQ,
		DLRoutingKey: "timeline.write.failed",
		BindingKeys: []string{
			EventTypeVideoTimelinePush,
		},
	},
}

func DeclareTopology(conn *amqp.Connection) error {
	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open channel: %w", err)
	}
	defer ch.Close()

	for _, spec := range queueSpecs {
		// 业务主 exchange。
		if err := ch.ExchangeDeclare(
			spec.Exchange,
			spec.ExchangeType,
			true,
			false,
			false,
			false,
			nil,
		); err != nil {
			return fmt.Errorf("declare exchange %s: %w", spec.Exchange, err)
		}

		// 每个业务域独立的死信 exchange。
		if err := ch.ExchangeDeclare(
			spec.DLX,
			"direct",
			true,
			false,
			false,
			false,
			nil,
		); err != nil {
			return fmt.Errorf("declare dlx %s: %w", spec.DLX, err)
		}

		mainArgs := amqp.Table{
			"x-dead-letter-exchange":    spec.DLX,
			"x-dead-letter-routing-key": spec.DLRoutingKey,
		}
		// 主队列拒绝的消息会按策略转发到 DLQ。
		if _, err := ch.QueueDeclare(
			spec.Queue,
			true,
			false,
			false,
			false,
			mainArgs,
		); err != nil {
			return fmt.Errorf("declare queue %s: %w", spec.Queue, err)
		}

		for _, key := range spec.BindingKeys {
			// 将业务 routing key 绑定到主队列。
			if err := ch.QueueBind(
				spec.Queue,
				key,
				spec.Exchange,
				false,
				nil,
			); err != nil {
				return fmt.Errorf("bind queue %s to exchange %s by %s: %w", spec.Queue, spec.Exchange, key, err)
			}
		}

		if _, err := ch.QueueDeclare(
			spec.DLQ,
			true,
			false,
			false,
			false,
			nil,
		); err != nil {
			return fmt.Errorf("declare dlq %s: %w", spec.DLQ, err)
		}

		if err := ch.QueueBind(
			spec.DLQ,
			spec.DLRoutingKey,
			spec.DLX,
			false,
			nil,
		); err != nil {
			return fmt.Errorf("bind dlq %s to dlx %s: %w", spec.DLQ, spec.DLX, err)
		}
	}

	return nil
}
