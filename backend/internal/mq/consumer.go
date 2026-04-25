package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type HandlerFunc func(ctx context.Context, event Envelope) error

type Consumer struct {
	conn        *amqp.Connection
	queue       string
	consumerTag string
	prefetch    int
	handle      HandlerFunc
}

func NewConsumer(conn *amqp.Connection, queue string, consumerTag string, prefetchCount int, handle HandlerFunc) *Consumer {
	if prefetchCount <= 0 {
		prefetchCount = 10
	}
	return &Consumer{
		conn:        conn,
		queue:       queue,
		consumerTag: consumerTag,
		prefetch:    prefetchCount,
		handle:      handle,
	}
}

func (c *Consumer) Run(ctx context.Context) error {
	ch, err := c.conn.Channel()
	if err != nil {
		return fmt.Errorf("open consumer channel: %w", err)
	}
	defer ch.Close()

	if err := ch.Qos(c.prefetch, 0, false); err != nil {
		return fmt.Errorf("set qos: %w", err)
	}

	msgs, err := ch.Consume(
		c.queue,
		c.consumerTag,
		false, // 关闭自动 ACK，只有业务处理成功后才确认。
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf("consume queue %s: %w", c.queue, err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case d, ok := <-msgs:
			if !ok {
				return fmt.Errorf("consumer channel closed: %s", c.queue)
			}

			if err := c.handleDelivery(ctx, d); err != nil {
				// nack(requeue=false) 会按队列策略进入死信队列。
				log.Printf("consumer[%s]: handle message failed, send to dlq: %v", c.queue, err)
				if nackErr := d.Nack(false, false); nackErr != nil {
					log.Printf("consumer[%s]: nack failed: %v", c.queue, nackErr)
				}
				continue
			}

			// 业务处理成功后再 ACK。
			if err := d.Ack(false); err != nil {
				log.Printf("consumer[%s]: ack failed: %v", c.queue, err)
			}
		}
	}
}

func (c *Consumer) handleDelivery(parent context.Context, d amqp.Delivery) error {
	var env Envelope
	if err := json.Unmarshal(d.Body, &env); err != nil {
		return fmt.Errorf("unmarshal envelope: %w", err)
	}

	handleCtx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()

	return c.handle(handleCtx, env)
}
