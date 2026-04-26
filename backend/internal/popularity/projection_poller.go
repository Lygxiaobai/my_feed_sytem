package popularity

import (
	"context"
	"log"
	"time"
)

const (
	defaultProjectionBatchSize    = 20
	defaultProjectionPollInterval = time.Second
	defaultProjectionLockTimeout  = 30 * time.Second
	defaultProjectionApplyTimeout = 3 * time.Second
	maxProjectionRetryDelay       = time.Minute
)

type ProjectionPoller struct {
	repo         *ProjectionRepo
	service      *Service
	batchSize    int
	pollInterval time.Duration
	lockTimeout  time.Duration
	applyTimeout time.Duration
}

func NewProjectionPoller(repo *ProjectionRepo, service *Service) *ProjectionPoller {
	return &ProjectionPoller{
		repo:         repo,
		service:      service,
		batchSize:    defaultProjectionBatchSize,
		pollInterval: defaultProjectionPollInterval,
		lockTimeout:  defaultProjectionLockTimeout,
		applyTimeout: defaultProjectionApplyTimeout,
	}
}

func (p *ProjectionPoller) Run(ctx context.Context) {
	if p == nil || p.repo == nil || p.service == nil {
		return
	}

	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	for {
		if err := p.runOnce(ctx); err != nil && ctx.Err() == nil {
			log.Printf("popularity projection poller: process batch failed: %v", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (p *ProjectionPoller) runOnce(ctx context.Context) error {
	rows, err := p.repo.ClaimBatch(p.batchSize, time.Now().UTC(), p.lockTimeout)
	if err != nil {
		return err
	}

	for _, row := range rows {
		applyCtx, cancel := context.WithTimeout(ctx, p.applyTimeout)
		applyErr := p.service.RecordEvent(applyCtx, row.EventID, row.VideoID, float64(row.Delta), row.OccurredAt)
		cancel()
		if applyErr != nil {
			retryAt := time.Now().UTC().Add(nextProjectionRetryDelay(row.AttemptCount))
			if err := p.repo.MarkPending(row.ID, retryAt, applyErr); err != nil {
				log.Printf("popularity projection poller: requeue failed, id=%d err=%v", row.ID, err)
			}
			continue
		}

		if err := p.repo.Delete(row.ID); err != nil {
			log.Printf("popularity projection poller: delete applied row failed, id=%d err=%v", row.ID, err)
		}
	}

	return nil
}

func nextProjectionRetryDelay(attempt int) time.Duration {
	if attempt <= 1 {
		return 2 * time.Second
	}

	delay := 2 * time.Second
	for i := 1; i < attempt && delay < maxProjectionRetryDelay; i++ {
		delay *= 2
	}
	if delay > maxProjectionRetryDelay {
		return maxProjectionRetryDelay
	}
	return delay
}
