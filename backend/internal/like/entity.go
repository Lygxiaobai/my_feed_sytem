package like

import "time"

// VideoLike 表示用户对视频的点赞记录。
type VideoLike struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`
	VideoID   uint64    `gorm:"not null;uniqueIndex:uk_video_likes_video_account" json:"video_id"`
	AccountID uint64    `gorm:"not null;uniqueIndex:uk_video_likes_video_account;index:idx_video_likes_account_created" json:"account_id"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName 指定点赞记录表名。
func (VideoLike) TableName() string {
	return "video_likes"
}

// LikeRequest 统一承载点赞相关接口的入参。
type LikeRequest struct {
	VideoID uint64 `json:"video_id" binding:"required"`
}

type ListLikedVideoIDsRequest struct {
	VideoIDs []uint64 `json:"video_ids" binding:"required"`
}
