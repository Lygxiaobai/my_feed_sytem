package feed

import (
	"time"

	"gorm.io/gorm"

	"my_feed_system/internal/video"
)

// Repo 负责信息流查询所需的数据库访问。
type Repo struct {
	db *gorm.DB
}

// NewRepo 创建信息流仓储。
func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

// ListLatest 使用 created_at + id 作为游标，按最新发布时间分页。
func (r *Repo) ListLatest(limit int64, latestTime int64, lastID uint64) ([]video.Video, error) {
	var videos []video.Video

	query := r.db.Model(&video.Video{})
	if latestTime > 0 {
		cursorTime := time.UnixMilli(latestTime)
		query = query.Where(
			"created_at < ? OR (created_at = ? AND id < ?)",
			cursorTime,
			cursorTime,
			lastID,
		)
	}

	if err := query.Order("created_at DESC, id DESC").Limit(int(limit)).Find(&videos).Error; err != nil {
		return nil, err
	}

	return videos, nil
}

// ListLikesCount 使用 likes_count + id 作为游标，按点赞数分页。
func (r *Repo) ListLikesCount(limit int64, likesCountBefore *int64, idBefore uint64) ([]video.Video, error) {
	var videos []video.Video

	query := r.db.Model(&video.Video{})
	if likesCountBefore != nil {
		query = query.Where(
			"likes_count < ? OR (likes_count = ? AND id < ?)",
			*likesCountBefore,
			*likesCountBefore,
			idBefore,
		)
	}

	if err := query.Order("likes_count DESC, id DESC").Limit(int(limit)).Find(&videos).Error; err != nil {
		return nil, err
	}

	return videos, nil
}

// ListByFollowing 查询某个用户关注作者发布的视频，并按时间倒序分页。
func (r *Repo) ListByFollowing(followerID uint64, limit int64, latestTime int64, lastID uint64) ([]video.Video, error) {
	var videos []video.Video

	query := r.db.Model(&video.Video{}).
		Joins("JOIN social_relations sr ON sr.vlogger_id = videos.author_id").
		Where("sr.follower_id = ?", followerID)

	if latestTime > 0 {
		cursorTime := time.UnixMilli(latestTime)
		query = query.Where(
			"videos.created_at < ? OR (videos.created_at = ? AND videos.id < ?)",
			cursorTime,
			cursorTime,
			lastID,
		)
	}

	if err := query.Order("videos.created_at DESC, videos.id DESC").Limit(int(limit)).Find(&videos).Error; err != nil {
		return nil, err
	}

	return videos, nil
}

// FindByIDs 按主键批量查询视频，用于把热度快照结果回填成完整视频信息。
func (r *Repo) FindByIDs(ids []uint64) ([]video.Video, error) {
	if len(ids) == 0 {
		return []video.Video{}, nil
	}

	var videos []video.Video
	if err := r.db.Where("id IN ?", ids).Find(&videos).Error; err != nil {
		return nil, err
	}

	return videos, nil
}

// ListByPopularity 在无 Redis 热度服务时，退化为按表中 popularity 字段排序。
func (r *Repo) ListByPopularity(limit int64, offset int64) ([]video.Video, error) {
	var videos []video.Video

	query := r.db.Model(&video.Video{}).Order("popularity DESC, id DESC").Limit(int(limit))
	if offset > 0 {
		query = query.Offset(int(offset))
	}

	if err := query.Find(&videos).Error; err != nil {
		return nil, err
	}

	return videos, nil
}
