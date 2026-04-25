package video

import "time"

// Video is the persisted video model.
type Video struct {
	ID           uint64    `gorm:"primaryKey" json:"id"`
	AuthorID     uint64    `gorm:"not null;index:idx_videos_author_created" json:"author_id"`
	Username     string    `gorm:"size:64;not null" json:"username"`
	Title        string    `gorm:"size:128;not null" json:"title"`
	Description  string    `gorm:"size:1000" json:"description"`
	PlayURL      string    `gorm:"size:255;not null" json:"play_url"`
	CoverURL     string    `gorm:"size:255" json:"cover_url"`
	LikesCount   int64     `gorm:"not null;default:0;index:idx_videos_likes_count" json:"likes_count"`
	CommentCount int64     `gorm:"not null;default:0" json:"comment_count"`
	Popularity   int64     `gorm:"not null;default:0;index:idx_videos_popularity" json:"popularity"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (Video) TableName() string {
	return "videos"
}

type PublishRequest struct {
	Title           string `json:"title" binding:"required"`
	Description     string `json:"description"`
	PlayURL         string `json:"play_url" binding:"required"`
	CoverURL        string `json:"cover_url"`
	ClientRequestID string `json:"client_request_id"`
}

type ListByAuthorIDRequest struct {
	AuthorID uint64 `json:"author_id" binding:"required"`
}

type ListLikedRequest struct{}

type GetDetailRequest struct {
	ID uint64 `json:"id" binding:"required"`
}
