package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"my_feed_system/internal/config"
)

type Publisher struct {
	conn *amqp.Connection
	cfg  *config.RabbitMQConfig
}

func NewPublisher(conn *amqp.Connection) *Publisher {
	return &Publisher{conn: conn}
}

func NewResilientPublisher(cfg config.RabbitMQConfig) *Publisher {
	return &Publisher{cfg: &cfg}
}

func (p *Publisher) Publish(ctx context.Context, event Envelope) error {
	exchange, key, err := exchangeAndKey(event.EventType)
	if err != nil {
		return err
	}

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}

	conn, closeConn, err := p.connection()
	if err != nil {
		return err
	}
	defer closeConn()

	if p.cfg != nil {
		if err := DeclareTopology(conn); err != nil {
			return fmt.Errorf("declare topology: %w", err)
		}
	}

	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open channel: %w", err)
	}
	defer ch.Close()

	pubCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	msg := amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
		MessageId:    event.EventID,
		Type:         event.EventType,
		Timestamp:    event.OccurredAt,
	}
	if err := ch.PublishWithContext(pubCtx, exchange, key, false, false, msg); err != nil {
		return fmt.Errorf("publish event %s: %w", event.EventType, err)
	}

	return nil
}

func (p *Publisher) connection() (*amqp.Connection, func(), error) {
	if p == nil {
		return nil, nil, fmt.Errorf("publisher is nil")
	}
	if p.conn != nil {
		return p.conn, func() {}, nil
	}
	if p.cfg == nil {
		return nil, nil, fmt.Errorf("publisher is not configured")
	}

	conn, err := Dial(*p.cfg)
	if err != nil {
		return nil, nil, err
	}
	return conn, func() {
		_ = conn.Close()
	}, nil
}

func exchangeAndKey(eventType string) (exchange string, routingKey string, err error) {
	switch eventType {
	case EventTypeLikeCreated, EventTypeLikeDeleted:
		return ExchangeLikeEvents, eventType, nil
	case EventTypeCommentCreated, EventTypeCommentDeleted:
		return ExchangeCommentEvents, eventType, nil
	case EventTypeSocialFollowed, EventTypeSocialUnfollowed:
		return ExchangeSocialEvents, eventType, nil
	case EventTypePopularityChanged:
		return ExchangePopularityEvents, eventType, nil
	case EventTypeVideoTimelinePush:
		return ExchangeVideoTimeline, eventType, nil
	default:
		return "", "", fmt.Errorf("unknown event type: %s", eventType)
	}
}
