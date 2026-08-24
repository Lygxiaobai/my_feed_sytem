package recommend

import (
	"errors"

	"my_feed_system/internal/account"
	"my_feed_system/internal/like"
	"my_feed_system/internal/video"
	"my_feed_system/internal/wallet"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repo struct {
	db *gorm.DB
}

func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

func (r *Repo) UpsertEmbedding(row VideoEmbedding) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "video_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"model", "dim", "vector", "updated_at"}),
	}).Create(&row).Error
}

func (r *Repo) FindEmbedding(videoID uint64) (*VideoEmbedding, error) {
	var row VideoEmbedding
	err := r.db.Where("video_id = ?", videoID).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func (r *Repo) ListMissingVideoIDs(model string, limit int) ([]uint64, error) {
	if limit <= 0 {
		limit = 200
	}
	var ids []uint64
	err := video.ScopePublic(r.db.Model(&video.Video{})).
		Select("videos.id").
		Where(`NOT EXISTS (
			SELECT 1 FROM video_embeddings e
			WHERE e.video_id = videos.id AND e.model = ?
		)`, model).
		Order("videos.id ASC").
		Limit(limit).
		Pluck("videos.id", &ids).Error
	return ids, err
}

func (r *Repo) LoadVideo(videoID uint64) (*video.Video, error) {
	var item video.Video
	err := r.db.Where("id = ?", videoID).First(&item).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func (r *Repo) ListApprovedCandidates(exclude map[uint64]struct{}, viewerID uint64) ([]candidate, error) {
	query := video.ScopePublic(r.db.Model(&video.Video{}))
	if viewerID > 0 {
		query = query.Where("author_id <> ?", viewerID)
	}
	if len(exclude) > 0 {
		ids := make([]uint64, 0, len(exclude))
		for id := range exclude {
			ids = append(ids, id)
		}
		query = query.Where("id NOT IN ?", ids)
	}

	var videos []video.Video
	if err := query.Order("created_at DESC, id DESC").Limit(maxCandidates).Find(&videos).Error; err != nil {
		return nil, err
	}
	if len(videos) == 0 {
		return nil, nil
	}

	authorIDs := uniqueIDs(videos, func(v video.Video) uint64 { return v.AuthorID })
	videoIDs := uniqueIDs(videos, func(v video.Video) uint64 { return v.ID })

	followers := map[uint64]int64{}
	type followerRow struct {
		ID            uint64 `gorm:"column:id"`
		FollowerCount int64  `gorm:"column:follower_count"`
	}
	var followerRows []followerRow
	if err := r.db.Table("accounts").
		Select("id, follower_count").
		Where("id IN ?", authorIDs).
		Scan(&followerRows).Error; err != nil {
		return nil, err
	}
	for _, row := range followerRows {
		followers[row.ID] = row.FollowerCount
	}

	var embeddings []VideoEmbedding
	if err := r.db.Where("video_id IN ?", videoIDs).Find(&embeddings).Error; err != nil {
		return nil, err
	}
	byVideo := make(map[uint64]VideoEmbedding, len(embeddings))
	for _, row := range embeddings {
		byVideo[row.VideoID] = row
	}

	out := make([]candidate, 0, len(videos))
	for _, item := range videos {
		c := candidate{
			Video:         item,
			FollowerCount: followers[item.AuthorID],
			Hot:           item.Popularity,
		}
		if row, ok := byVideo[item.ID]; ok {
			if vec, ok := decodeVector(row.Vector, row.Dim); ok {
				c.Vector = vec
			}
		}
		out = append(out, c)
	}
	return out, nil
}

func (r *Repo) LoadAccountInterests(accountID uint64) ([]string, error) {
	if accountID == 0 {
		return nil, nil
	}
	var raw account.Account
	err := r.db.Select("interests").Where("id = ?", accountID).Take(&raw).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return raw.Interests, nil
}

func (r *Repo) ListInterestSignals(accountID uint64, limit int) ([]interestSignal, error) {
	if accountID == 0 || limit <= 0 {
		return nil, nil
	}

	var likes []like.VideoLike
	if err := r.db.Where("account_id = ?", accountID).
		Order("created_at DESC").
		Limit(limit).
		Find(&likes).Error; err != nil {
		return nil, err
	}

	var tips []wallet.TipRecord
	if err := r.db.Where("from_account_id = ?", accountID).
		Order("created_at DESC").
		Limit(limit).
		Find(&tips).Error; err != nil {
		return nil, err
	}

	merged := make(map[uint64]interestSignal, len(likes)+len(tips))
	for _, row := range likes {
		merged[row.VideoID] = interestSignal{VideoID: row.VideoID, Weight: likeWeight, At: row.CreatedAt}
	}
	for _, row := range tips {
		cur, ok := merged[row.VideoID]
		if !ok {
			merged[row.VideoID] = interestSignal{VideoID: row.VideoID, Weight: tipWeight, At: row.CreatedAt}
			continue
		}
		if row.CreatedAt.After(cur.At) {
			cur.At = row.CreatedAt
		}
		cur.Weight += tipWeight
		merged[row.VideoID] = cur
	}

	out := make([]interestSignal, 0, len(merged))
	for _, sig := range merged {
		out = append(out, sig)
	}
	return out, nil
}

func (r *Repo) UpsertUserEmbedding(row UserEmbedding) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "account_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"model", "dim", "vector", "updated_at"}),
	}).Create(&row).Error
}

func (r *Repo) FindUserEmbedding(accountID uint64) (*UserEmbedding, error) {
	var row UserEmbedding
	err := r.db.Where("account_id = ?", accountID).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func (r *Repo) DeleteUserEmbedding(accountID uint64) error {
	return r.db.Where("account_id = ?", accountID).Delete(&UserEmbedding{}).Error
}

func (r *Repo) LoadEmbeddings(videoIDs []uint64, model string) (map[uint64][]float32, error) {
	out := make(map[uint64][]float32, len(videoIDs))
	if len(videoIDs) == 0 {
		return out, nil
	}
	query := r.db.Where("video_id IN ?", videoIDs)
	if model != "" {
		query = query.Where("model = ?", model)
	}
	var rows []VideoEmbedding
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		vec, ok := decodeVector(row.Vector, row.Dim)
		if !ok {
			continue
		}
		out[row.VideoID] = vec
	}
	return out, nil
}

func uniqueIDs(videos []video.Video, pick func(video.Video) uint64) []uint64 {
	seen := make(map[uint64]struct{}, len(videos))
	ids := make([]uint64, 0, len(videos))
	for _, item := range videos {
		id := pick(item)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}
