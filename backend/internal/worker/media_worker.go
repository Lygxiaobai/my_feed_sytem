package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"gorm.io/gorm"

	"my_feed_system/internal/media"
	"my_feed_system/internal/mq"
	"my_feed_system/internal/storage"
)

type MediaWorker struct {
	db        *gorm.DB
	repo      *media.Repo
	processor *media.Processor
	store     storage.ObjectStore
}

func NewMediaWorker(db *gorm.DB, store storage.ObjectStore) *MediaWorker {
	return &MediaWorker{
		db:        db,
		repo:      media.NewRepo(db),
		processor: media.NewProcessor(),
		store:     store,
	}
}

func (w *MediaWorker) Handle(ctx context.Context, event mq.Envelope) error {
	if event.EventType != mq.EventTypeMediaTranscodeRequested {
		return fmt.Errorf("media worker unsupported event: %s", event.EventType)
	}

	var payload mq.MediaTranscodePayload
	if err := event.DecodePayload(&payload); err != nil {
		return fmt.Errorf("decode media.transcode.requested payload: %w", err)
	}
	if payload.TaskID == 0 || payload.AccountID == 0 {
		return errors.New("invalid media.transcode.requested payload")
	}

	task, err := w.repo.FindByID(payload.TaskID)
	if err != nil {
		return err
	}
	if task.AccountID != payload.AccountID {
		return errors.New("media task account mismatch")
	}
	if task.Status != media.StatusProcessing {
		return nil
	}

	playURL, posterURL, processErr := w.process(ctx, task)
	if processErr != nil {
		if err := w.markFailed(event, task.ID, userFacingTranscodeError()); err != nil {
			return err
		}
		// 任务已经进入 failed，源文件不再用于重试，避免坏文件占着上传桶
		// 直到生命周期规则过期才被清掉。
		w.removeSource(ctx, task.SourceKey)
		slog.ErrorContext(ctx, "transcode failed", slog.Uint64("task_id", task.ID), slog.String("error", processErr.Error()))
		return nil
	}

	if err := w.db.Transaction(func(tx *gorm.DB) error {
		if err := mq.MarkProcessed(tx, "media-worker", event); err != nil {
			if errors.Is(err, mq.ErrAlreadyProcessed) {
				return nil
			}
			return err
		}
		return w.repo.MarkReady(tx, task.ID, playURL, posterURL)
	}); err != nil {
		return err
	}

	w.removeSource(ctx, task.SourceKey)
	return nil
}

// process 是唯一接触媒体字节的地方：把源对象拉到本地临时文件、转码、
// 再把成品推回媒体桶。ffmpeg 需要可 seek 的输入做 probe，所以不能直接喂 HTTP 流。
func (w *MediaWorker) process(ctx context.Context, task *media.Task) (string, string, error) {
	workDir, err := os.MkdirTemp("", fmt.Sprintf("transcode-%d-", task.ID))
	if err != nil {
		return "", "", fmt.Errorf("create transcode workspace: %w", err)
	}
	defer os.RemoveAll(workDir)

	sourcePath := filepath.Join(workDir, "source")
	if err := w.download(ctx, task.SourceKey, sourcePath); err != nil {
		return "", "", err
	}

	videoPath := filepath.Join(workDir, "out.mp4")
	posterPath := filepath.Join(workDir, "out.jpg")
	if err := w.processor.Transcode(ctx, sourcePath, videoPath, posterPath); err != nil {
		return "", "", err
	}

	// 先传封面再传视频。反过来的话，视频已经可播但封面还没到，
	// 中途失败会在前端留下一个有画面无封面的空档。
	if err := w.upload(ctx, posterPath, media.CoverKey(task.ID), "image/jpeg"); err != nil {
		return "", "", err
	}
	if err := w.upload(ctx, videoPath, media.VideoKey(task.ID), "video/mp4"); err != nil {
		// 封面已经上去了，视频没成功。清掉孤儿封面，避免媒体桶里留下
		// 永远不会被引用的对象。
		if removeErr := w.store.RemoveMedia(ctx, media.CoverKey(task.ID)); removeErr != nil {
			slog.WarnContext(ctx, "remove orphan cover failed",
				slog.Uint64("task_id", task.ID), slog.String("error", removeErr.Error()))
		}
		return "", "", err
	}

	return media.PlayURL(task.ID), media.PosterURL(task.ID), nil
}

func (w *MediaWorker) download(ctx context.Context, key string, targetPath string) error {
	object, err := w.store.GetUpload(ctx, key)
	if err != nil {
		return fmt.Errorf("fetch media source: %w", err)
	}
	defer object.Close()

	file, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create local source: %w", err)
	}
	written, copyErr := io.Copy(file, object)
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("download media source: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close local source: %w", closeErr)
	}
	if written == 0 {
		return errors.New("media source is empty")
	}
	return nil
}

func (w *MediaWorker) upload(ctx context.Context, localPath string, key string, contentType string) error {
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open transcode output: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat transcode output: %w", err)
	}
	return w.store.PutMedia(ctx, key, file, info.Size(), contentType)
}

func (w *MediaWorker) markFailed(event mq.Envelope, taskID uint64, message string) error {
	return w.db.Transaction(func(tx *gorm.DB) error {
		if err := mq.MarkProcessed(tx, "media-worker", event); err != nil {
			if errors.Is(err, mq.ErrAlreadyProcessed) {
				return nil
			}
			return err
		}
		return w.repo.MarkFailed(tx, taskID, message)
	})
}

// userFacingTranscodeError 只给前端看结果，ffmpeg / signal 细节留在日志里。
func userFacingTranscodeError() string {
	return "视频处理失败，请重新上传"
}

func (w *MediaWorker) removeSource(ctx context.Context, key string) {
	if key == "" {
		return
	}
	// 删不掉也不致命：上传桶配了 sources/ 前缀 1 天过期的生命周期规则兜底。
	if err := w.store.RemoveUpload(ctx, key); err != nil && !errors.Is(err, storage.ErrObjectNotFound) {
		slog.Warn("remove media source failed", slog.String("key", key), slog.String("error", err.Error()))
	}
}
