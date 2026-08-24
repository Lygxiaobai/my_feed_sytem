package feed

import (
	"time"

	"gorm.io/gorm"

	"my_feed_system/internal/video"
)

// Repo 负责信息流查询所需的数据库访问。
//
// 本文件内所有查询都面向公众，因此**每一个**都必须带上公开过滤。
// 漏掉任意一处，未过审、作者下架或已删除的内容就会从那条路径泄漏出去——
// 新增查询方法时务必套用 onlyApproved（内部走 video.ScopePublic）。
type Repo struct {
	db *gorm.DB
}

// NewRepo 创建信息流仓储。
func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

// onlyApproved 给查询附加审核过滤。
// 统一走这个函数而不是各处手写字符串，避免拼错列名或写错状态值。
func onlyApproved(query *gorm.DB) *gorm.DB {
	return video.ScopePublic(query)
}

// ListLatest 使用 created_at + id 作为游标，按最新发布时间分页。
func (r *Repo) ListLatest(limit int64, latestTime int64, lastID uint64) ([]video.Video, error) {
	var videos []video.Video

	query := onlyApproved(r.db.Model(&video.Video{}))
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

	query := onlyApproved(r.db.Model(&video.Video{}))
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

	query := onlyApproved(r.db.Model(&video.Video{}).
		Joins("JOIN social_relations sr ON sr.vlogger_id = videos.author_id").
		Where("sr.follower_id = ?", followerID))

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

// FindByIDs 按主键批量查询视频，用于把热度快照或全局时间线的结果回填成完整视频信息。
//
// 这里同样要过滤：Redis 里的时间线与热度榜是异步写入的，
// 内容被下架后残留的 ID 只能靠这一层挡住。
func (r *Repo) FindByIDs(ids []uint64) ([]video.Video, error) {
	if len(ids) == 0 {
		return []video.Video{}, nil
	}

	var videos []video.Video
	if err := onlyApproved(r.db.Model(&video.Video{}).Where("id IN ?", ids)).
		Find(&videos).Error; err != nil {
		return nil, err
	}

	return videos, nil
}

// ListFollowedAuthors 查询某个用户关注的作者及其粉丝数。
//
// 这里查的是关注关系而不是视频，因此不适用本文件的审核过滤约定；
// 由它挑出的作者最终仍要经过 FindByIDs 才能拿到视频。
func (r *Repo) ListFollowedAuthors(followerID uint64) ([]FollowedAuthor, error) {
	authors := make([]FollowedAuthor, 0)
	if err := r.db.
		Table("social_relations").
		Select("social_relations.vlogger_id AS vlogger_id, COALESCE(accounts.follower_count, 0) AS follower_count").
		Joins("LEFT JOIN accounts ON accounts.id = social_relations.vlogger_id").
		Where("social_relations.follower_id = ?", followerID).
		Scan(&authors).Error; err != nil {
		return nil, err
	}

	return authors, nil
}

// ListByPopularity 在无 Redis 热度服务时，退化为按表中 popularity 字段排序。
func (r *Repo) ListByPopularity(limit int64, offset int64) ([]video.Video, error) {
	var videos []video.Video

	query := onlyApproved(r.db.Model(&video.Video{})).
		Order("popularity DESC, id DESC").
		Limit(int(limit))
	if offset > 0 {
		query = query.Offset(int(offset))
	}

	if err := query.Find(&videos).Error; err != nil {
		return nil, err
	}

	return videos, nil
}
