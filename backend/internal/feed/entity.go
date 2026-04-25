package feed

import "time"

// ListLatestRequest 描述最新流查询请求。
type ListLatestRequest struct {
	Limit int64 `json:"limit"`
	// LatestTime 与 LastID 共同组成时间倒序分页游标。
	LatestTime int64  `json:"latest_time"`
	LastID     uint64 `json:"last_id"`
}

// ListLikesCountRequest 描述按点赞数排序的排行榜查询请求。
type ListLikesCountRequest struct {
	Limit            int64  `json:"limit"`
	LikesCountBefore *int64 `json:"likes_count_before"`
	IDBefore         uint64 `json:"id_before"`
}

// ListByFollowingRequest 描述关注流查询请求。
type ListByFollowingRequest struct {
	Limit      int64  `json:"limit"`
	LatestTime int64  `json:"latest_time"`
	LastID     uint64 `json:"last_id"`
}

// ListByPopularityRequest 描述热榜查询请求。
type ListByPopularityRequest struct {
	Limit  int64 `json:"limit"`
	AsOf   int64 `json:"as_of"`
	Offset int64 `json:"offset"`
}

// FeedVideo 表示信息流返回给前端的视频卡片数据。
type FeedVideo struct {
	ID           uint64    `json:"id"`
	AuthorID     uint64    `json:"author_id"`
	Username     string    `json:"username"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	PlayURL      string    `json:"play_url"`
	CoverURL     string    `json:"cover_url"`
	LikesCount   int64     `json:"likes_count"`
	CommentCount int64     `json:"comment_count"`
	Popularity   int64     `json:"popularity"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ListLatestResult 表示最新流分页结果。
type ListLatestResult struct {
	Videos []FeedVideo `json:"videos"`
	// NextTime 与 NextID 用于请求下一页最新流。
	NextTime int64  `json:"next_time"`
	NextID   uint64 `json:"next_id"`
	HasMore  bool   `json:"has_more"`
}

// ListLikesCountResult 表示点赞榜分页结果。
type ListLikesCountResult struct {
	Videos               []FeedVideo `json:"videos"`
	NextLikesCountBefore *int64      `json:"next_likes_count_before,omitempty"`
	NextIDBefore         uint64      `json:"next_id_before"`
	HasMore              bool        `json:"has_more"`
}

// ListByFollowingResult 表示关注流分页结果。
type ListByFollowingResult struct {
	Videos   []FeedVideo `json:"videos"`
	NextTime int64       `json:"next_time"`
	NextID   uint64      `json:"next_id"`
	HasMore  bool        `json:"has_more"`
}

// ListByPopularityResult 表示热榜分页结果。
type ListByPopularityResult struct {
	Videos []FeedVideo `json:"videos"`
	// AsOf 固定本次分页所依据的热度快照时间。
	AsOf       int64 `json:"as_of"`
	NextOffset int64 `json:"next_offset"`
	HasMore    bool  `json:"has_more"`
}
