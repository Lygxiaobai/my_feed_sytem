package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"my_feed_system/internal/mq"
	"my_feed_system/internal/recommend"
)

const embedConsumerGroup = "embed-worker"

// EmbedWorker 在内容公开后异步计算标题/描述向量。
// 失败不得影响视频已经公开这一事实。
type EmbedWorker struct {
	db       *gorm.DB
	repo     *recommend.Repo
	embedder recommend.Embedder
}

func NewEmbedWorker(db *gorm.DB, embedder recommend.Embedder) *EmbedWorker {
	return &EmbedWorker{
		db:       db,
		repo:     recommend.NewRepo(db),
		embedder: embedder,
	}
}

func (w *EmbedWorker) Handle(ctx context.Context, event mq.Envelope) error {
	if event.EventType != mq.EventTypeVideoEmbedRequested {
		return fmt.Errorf("embed worker unsupported event: %s", event.EventType)
	}
	var payload mq.VideoEmbedPayload
	if err := event.DecodePayload(&payload); err != nil {
		return fmt.Errorf("decode video.embed.requested: %w", err)
	}
	if payload.VideoID == 0 {
		return errors.New("invalid video.embed.requested payload")
	}

	if w.embedder == nil || !w.embedder.Enabled() {
		// 未配置时确认消息，避免每条发布都进死信。
		slog.InfoContext(ctx, "embedding disabled, skip", slog.Uint64("video_id", payload.VideoID))
		return w.markDone(event)
	}

	if err := w.embedVideo(ctx, payload.VideoID); err != nil {
		return err
	}
	return w.markDone(event)
}

func (w *EmbedWorker) markDone(event mq.Envelope) error {
	return w.db.Transaction(func(tx *gorm.DB) error {
		if err := mq.MarkProcessed(tx, embedConsumerGroup, event); err != nil {
			if errors.Is(err, mq.ErrAlreadyProcessed) {
				return nil
			}
			return err
		}
		return nil
	})
}

func (w *EmbedWorker) embedVideo(ctx context.Context, videoID uint64) error {
	item, err := w.repo.LoadVideo(videoID)
	if err != nil {
		return err
	}
	if item == nil || !item.IsPubliclyListed() {
		return nil
	}
	text := recommend.EmbedText(item.Title, item.Description)
	if text == "" {
		return nil
	}
	vec, err := w.embedder.Embed(ctx, text)
	if err != nil {
		return fmt.Errorf("embed video %d: %w", videoID, err)
	}
	now := time.Now().UTC()
	return w.repo.UpsertEmbedding(recommend.VideoEmbedding{
		VideoID:   videoID,
		Model:     w.embedder.Model(),
		Dim:       len(vec),
		Vector:    recommend.EncodeVector(vec),
		UpdatedAt: now,
	})
}

// Backfill 给存量已过审、尚无当前模型向量的视频补算。接口未配置时直接返回。
func (w *EmbedWorker) Backfill(ctx context.Context) {
	if w.embedder == nil || !w.embedder.Enabled() {
		return
	}
	ids, err := w.repo.ListMissingVideoIDs(w.embedder.Model(), 500)
	if err != nil {
		slog.WarnContext(ctx, "list videos missing embeddings failed", slog.String("error", err.Error()))
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		if err := w.embedVideo(ctx, id); err != nil {
			slog.WarnContext(ctx, "backfill embedding failed",
				slog.Uint64("video_id", id), slog.String("error", err.Error()))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
	if len(ids) > 0 {
		slog.InfoContext(ctx, "embedding backfill finished", slog.Int("count", len(ids)))
	}
}
