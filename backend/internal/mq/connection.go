package mq

import (
	"fmt"
	"net/url"
	"strings"

	amqp "github.com/rabbitmq/amqp091-go"

	"my_feed_system/internal/config"
)

func Dial(cfg config.RabbitMQConfig) (*amqp.Connection, error) {
	dsn, err := buildDSN(cfg)
	if err != nil {
		return nil, err
	}

	conn, err := amqp.Dial(dsn)
	if err != nil {
		return nil, fmt.Errorf("dial rabbitmq: %w", err)
	}
	return conn, nil
}

func buildDSN(cfg config.RabbitMQConfig) (string, error) {
	// RabbitMQ 的 "/" vhost 在连接串里需要编码成 "%2F"。
	vhost := cfg.VHost
	if strings.TrimSpace(vhost) == "" {
		vhost = "/"
	}
	encodedVHost := "%2F"
	if vhost != "/" {
		encodedVHost = url.PathEscape(strings.TrimPrefix(vhost, "/"))
	}

	return fmt.Sprintf(
		"amqp://%s:%s@%s:%d/%s",
		url.QueryEscape(cfg.Username),
		url.QueryEscape(cfg.Password),
		cfg.Host,
		cfg.Port,
		encodedVHost,
	), nil
}
