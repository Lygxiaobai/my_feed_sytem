package db

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"my_feed_system/internal/account"
	"my_feed_system/internal/audit"
	"my_feed_system/internal/comment"
	"my_feed_system/internal/config"
	"my_feed_system/internal/danmaku"
	"my_feed_system/internal/dm"
	"my_feed_system/internal/history"
	"my_feed_system/internal/idempotency"
	"my_feed_system/internal/invoice"
	"my_feed_system/internal/like"
	"my_feed_system/internal/media"
	"my_feed_system/internal/mq"
	"my_feed_system/internal/notification"
	"my_feed_system/internal/outbox"
	"my_feed_system/internal/popularity"
	"my_feed_system/internal/recommend"
	"my_feed_system/internal/report"
	"my_feed_system/internal/social"
	"my_feed_system/internal/video"
	"my_feed_system/internal/wallet"
)

func NewMySQL(cfg config.DatabaseConfig) (*gorm.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		cfg.User,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.DBName,
	)

	// 用 slog 适配器接管 GORM 日志，统一输出格式并避免 SQL 参数值进日志。
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: newSlogGormLogger(200 * time.Millisecond),
	})
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
		&account.Identity{},
		&account.PasskeyCredential{},
		&video.Video{},
		&like.VideoLike{},
		&media.Task{},
		&comment.VideoComment{},
		&danmaku.VideoDanmaku{},
		&history.WatchHistory{},
		&social.SocialRelation{},
		&mq.ProcessedMessage{},
		&mq.DeadLetterMessage{},
		&idempotency.Key{},
		&outbox.Message{},
		&popularity.Projection{},
		&audit.Record{},
		&report.Report{},
		&notification.Notification{},
		&dm.Conversation{},
		&dm.Message{},
		&wallet.Lot{},
		&wallet.Ledger{},
		&wallet.DailyAction{},
		&wallet.RechargeOrder{},
		&wallet.TipRecord{},
		&wallet.PlatformEntry{},
		&invoice.Profile{},
		&invoice.Invoice{},
		&recommend.VideoEmbedding{},
		&recommend.UserEmbedding{},
	); err != nil {
		return nil, fmt.Errorf("auto migrate tables: %w", err)
	}
	if err := ensureAccountSchema(db); err != nil {
		return nil, fmt.Errorf("ensure account schema: %w", err)
	}
	if err := ensureCommentSchema(db); err != nil {
		return nil, fmt.Errorf("ensure comment schema: %w", err)
	}
	if err := backfillAuditStatus(db); err != nil {
		return nil, fmt.Errorf("backfill audit status: %w", err)
	}
	if err := backfillVideoLifecycle(db); err != nil {
		return nil, fmt.Errorf("backfill video lifecycle: %w", err)
	}
	if err := syncVideoCounters(db); err != nil {
		return nil, fmt.Errorf("sync video counters: %w", err)
	}
	if err := syncFollowerCounts(db); err != nil {
		return nil, fmt.Errorf("sync follower counts: %w", err)
	}

	return db, nil
}

func ensureAccountSchema(db *gorm.DB) error {
	if !db.Migrator().HasTable(&account.Account{}) {
		return nil
	}
	// 邮箱账号没有密码。GORM AutoMigrate 不会把已有 NOT NULL 改成可空。
	return db.Exec("ALTER TABLE accounts MODIFY password VARCHAR(255) NULL").Error
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

// backfillAuditStatus 把审核功能上线前就已存在的视频置为已通过。
//
// 新增列的默认值是 pending，如果不做这一步，上线瞬间全部历史内容
// 都会从信息流中消失——那是数据事故而不是审核生效。
// 只针对空值与 pending 且创建时间早于本次迁移的行，用 audit_records
// 里是否已有记录来区分「历史遗留」与「新发布待审」。
func backfillAuditStatus(db *gorm.DB) error {
	return db.Exec(`
		UPDATE videos
		SET audit_status = 'approved'
		WHERE (audit_status IS NULL OR audit_status = '' OR audit_status = 'pending')
		  AND NOT EXISTS (
		      SELECT 1 FROM audit_records
		      WHERE audit_records.target_type = 'video'
		        AND audit_records.target_id = videos.id
		  )
		  AND videos.created_at < ?
	`, auditFeatureLaunchedAt).Error
}

// auditFeatureLaunchedAt 是审核功能上线时间。
// 早于此时间发布且没有任何审核流水的内容视为历史存量，直接放行。
var auditFeatureLaunchedAt = time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)

// backfillVideoLifecycle 把生命周期列上线前的存量行标成已发布。
// 新增列默认就是 published，这一步兜住空串或旧驱动没把默认值写进去的情况。
func backfillVideoLifecycle(db *gorm.DB) error {
	return db.Exec(`
		UPDATE videos
		SET lifecycle = 'published'
		WHERE lifecycle IS NULL OR lifecycle = ''
	`).Error
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

// syncFollowerCounts 把 accounts.follower_count 重算成 social_relations 的真值。
//
// 新增列默认是 0，不重算的话历史大V 全部会被判成普通用户而走全量写扩散。
// 同时这也是计数漂移的兜底：增减都在异步 Worker 里做，重启时对齐一次成本很低。
func syncFollowerCounts(db *gorm.DB) error {
	return db.Exec(`
		UPDATE accounts
		LEFT JOIN (
			SELECT vlogger_id, COUNT(*) AS cnt
			FROM social_relations
			GROUP BY vlogger_id
		) followed ON followed.vlogger_id = accounts.id
		SET accounts.follower_count = COALESCE(followed.cnt, 0)
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
