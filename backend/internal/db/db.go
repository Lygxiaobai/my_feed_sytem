package db

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"my_feed_system/internal/account"
	"my_feed_system/internal/comment"
	"my_feed_system/internal/config"
	"my_feed_system/internal/idempotency"
	"my_feed_system/internal/like"
	"my_feed_system/internal/mq"
	"my_feed_system/internal/outbox"
	"my_feed_system/internal/popularity"
	"my_feed_system/internal/social"
	"my_feed_system/internal/video"
)

func NewMySQL(cfg config.DatabaseConfig) (*gorm.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		cfg.User,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.DBName,
	)

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql db: %w", err)
	}

	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("ping mysql: %w", err)
	}

	// 幂等表跟业务表一起自动迁移，保证接口发布新版本后数据库结构能同步补齐。
	if err := db.AutoMigrate(
		&account.Account{},
		&video.Video{},
		&like.VideoLike{},
		&comment.VideoComment{},
		&social.SocialRelation{},
		&mq.ProcessedMessage{},
		&idempotency.Key{},
		&outbox.Message{},
		&popularity.Projection{},
	); err != nil {
		return nil, fmt.Errorf("auto migrate tables: %w", err)
	}
	if err := ensureCommentSchema(db); err != nil {
		return nil, fmt.Errorf("ensure comment schema: %w", err)
	}
	if err := syncVideoCounters(db); err != nil {
		return nil, fmt.Errorf("sync video counters: %w", err)
	}

	return db, nil
}

func ensureCommentSchema(db *gorm.DB) error {
	migrator := db.Migrator()
	model := &comment.VideoComment{}

	columns := []string{
		"RootCommentID",
		"ParentCommentID",
		"ReplyToUserID",
		"ReplyToUsername",
	}
	for _, column := range columns {
		if !migrator.HasColumn(model, column) {
			if err := migrator.AddColumn(model, column); err != nil {
				return err
			}
		}
	}

	indexes := []string{
		"idx_video_comments_video_root_created",
		"idx_video_comments_root_created",
		"idx_video_comments_parent",
	}
	for _, index := range indexes {
		if !migrator.HasIndex(model, index) {
			if err := migrator.CreateIndex(model, index); err != nil {
				return err
			}
		}
	}

	return nil
}

func syncVideoCounters(db *gorm.DB) error {
	if err := db.Exec(`
		UPDATE videos
		LEFT JOIN (
			SELECT video_id, COUNT(*) AS cnt
			FROM video_likes
			GROUP BY video_id
		) liked ON liked.video_id = videos.id
		SET videos.likes_count = COALESCE(liked.cnt, 0)
	`).Error; err != nil {
		return err
	}

	return db.Exec(`
		UPDATE videos
		LEFT JOIN (
			SELECT video_id, COUNT(*) AS cnt
			FROM video_comments
			GROUP BY video_id
		) commented ON commented.video_id = videos.id
		SET videos.comment_count = COALESCE(commented.cnt, 0)
	`).Error
}

func NewRedis(cfg config.RedisConfig) (*redis.Client, error) {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return client, nil
}
