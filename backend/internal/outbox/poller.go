package outbox

import (
	"context"
	"log"
	"time"

	"my_feed_system/internal/mq"
)

const (
	defaultBatchSize      = 20
	defaultPollInterval   = time.Second
	defaultLockTimeout    = 30 * time.Second
	defaultPublishTimeout = 3 * time.Second
	maxRetryDelay         = time.Minute
)

type Publisher interface {
	Publish(ctx context.Context, event mq.Envelope) error
}

type Poller struct {
	repo           *Repo
	publisher      Publisher
	batchSize      int
	pollInterval   time.Duration
	lockTimeout    time.Duration
	publishTimeout time.Duration
}

func NewPoller(repo *Repo, publisher Publisher) *Poller {
	return &Poller{
		repo:           repo,
		publisher:      publisher,
		batchSize:      defaultBatchSize,
		pollInterval:   defaultPollInterval,
		lockTimeout:    defaultLockTimeout,
		publishTimeout: defaultPublishTimeout,
	}
}

func (p *Poller) Run(ctx context.Context) {
	if p == nil || p.repo == nil || p.publisher == nil {
		return
	}

	// Poller 常驻后台，把事务内落下来的待投递事件持续补发到 MQ。
	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	for {
		if err := p.runOnce(ctx); err != nil && ctx.Err() == nil {
			log.Printf("outbox poller: process batch failed: %v", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (p *Poller) RunOnce(ctx context.Context) error {
	if p == nil || p.repo == nil || p.publisher == nil {
		return nil
	}
	return p.runOnce(ctx)
}

func (p *Poller) runOnce(ctx context.Context) error {
	// 先 claim 一批可处理消息，避免边扫边发时被其他实例重复拿到。
	rows, err := p.repo.ClaimBatch(p.batchSize, time.Now().UTC(), p.lockTimeout)
	if err != nil {
		return err
	}

	for _, row := range rows {
		publishCtx, cancel := context.WithTimeout(ctx, p.publishTimeout)
		publishErr := p.publisher.Publish(publishCtx, row.Envelope())
		cancel()
		if publishErr != nil {
			// 失败后回写 pending，等退避时间到达后再由后续轮询接着补投。
			retryAt := time.Now().UTC().Add(nextRetryDelay(row.AttemptCount))
			if err := p.repo.MarkPending(row.ID, retryAt, publishErr); err != nil {
				log.Printf("outbox poller: requeue failed, id=%d err=%v", row.ID, err)
			}
			continue
		}

		if err := p.repo.Delete(row.ID); err != nil {
			// 已经成功投递到 MQ 的消息删除失败时，最多造成重复投递，不会造成漏投。
			log.Printf("outbox poller: delete published row failed, id=%d err=%v", row.ID, err)
		}
	}

	return nil
}

func nextRetryDelay(attempt int) time.Duration {
	if attempt <= 1 {
		return 2 * time.Second
	}

	// 用简单指数退避控制重试频率，避免 MQ 故障时高频空转。
	delay := 2 * time.Second
	for i := 1; i < attempt && delay < maxRetryDelay; i++ {
		delay *= 2
	}
	if delay > maxRetryDelay {
		return maxRetryDelay
	}
	return delay
}
