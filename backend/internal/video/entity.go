package video

import (
	"time"

	"my_feed_system/internal/audit"
	"my_feed_system/internal/tag"
)

// Lifecycle 是作者侧的内容生命周期，与 AuditStatus 正交。
//
// 审核回答「平台允不允许公开」，生命周期回答「作者想不想公开」。
// 两者不能挤在同一个字段里：作者下架不是审核拒绝，草稿也从未进入过审流程。
type Lifecycle string

const (
	LifecycleDraft       Lifecycle = "draft"
	LifecyclePublished   Lifecycle = "published"
	LifecycleUnpublished Lifecycle = "unpublished"
)

func (l Lifecycle) Valid() bool {
	switch l {
	case LifecycleDraft, LifecyclePublished, LifecycleUnpublished:
		return true
	}
	return false
}

// Video is the persisted video model.
type Video struct {
	ID          uint64 `gorm:"primaryKey" json:"id"`
	AuthorID    uint64 `gorm:"not null;index:idx_videos_author_created;index:idx_videos_author_life,priority:1" json:"author_id"`
	Username    string `gorm:"size:64;not null" json:"username"`
	Title       string `gorm:"size:128;not null" json:"title"`
	Description string `gorm:"size:1000" json:"description"`
	// Tags 与账号兴趣同一形状：TEXT 里存最多 7 个字符串。
	Tags         tag.List `gorm:"type:text" json:"tags,omitempty"`
	PlayURL      string   `gorm:"size:255;not null" json:"play_url"`
	CoverURL     string   `gorm:"size:255" json:"cover_url"`
	LikesCount   int64    `gorm:"not null;default:0;index:idx_videos_likes_count" json:"likes_count"`
	CommentCount int64    `gorm:"not null;default:0" json:"comment_count"`
	Popularity   int64    `gorm:"not null;default:0;index:idx_videos_popularity" json:"popularity"`
	// AuditStatus 决定内容能否进入公开信息流。
	// 列默认 pending，业务层按 audit.enabled 写入：关闭时直接 approved。
	// 与 created_at 组成联合索引，因为所有信息流查询都会带上这个过滤条件。
	AuditStatus audit.Status `gorm:"size:20;not null;default:'pending';index:idx_videos_audit_created,priority:1" json:"audit_status"`
	// Lifecycle 默认 published，保证存量行在列上线后仍按「已发布」解释。
	Lifecycle Lifecycle `gorm:"size:20;not null;default:'published';index:idx_videos_author_life,priority:2" json:"lifecycle"`
	// DeletedAt 是软删除。不用 GORM 的 DeletedAt 类型，避免所有查询被悄悄过滤，
	// 管理面和流水回溯必须还能读到已删行。
	DeletedAt *time.Time `gorm:"index" json:"deleted_at,omitempty"`
	CreatedAt time.Time  `gorm:"index:idx_videos_audit_created,priority:2" json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func (Video) TableName() string {
	return "videos"
}

type PublishRequest struct {
	// DraftID 若有值，发布的是这条草稿而不是新建一行。
	DraftID         uint64   `json:"draft_id"`
	Title           string   `json:"title" binding:"required"`
	Description     string   `json:"description"`
	Tags            []string `json:"tags"`
	PlayURL         string   `json:"play_url" binding:"required"`
	CoverURL        string   `json:"cover_url"`
	ClientRequestID string   `json:"client_request_id"`
}

type SaveDraftRequest struct {
	ID          uint64   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	PlayURL     string   `json:"play_url" binding:"required"`
	CoverURL    string   `json:"cover_url"`
}

type AuthorVideoRequest struct {
	ID uint64 `json:"id" binding:"required"`
}

type ListByAuthorIDRequest struct {
	AuthorID uint64 `json:"author_id" binding:"required"`
}

type ListLikedRequest struct{}

type GetDetailRequest struct {
	ID uint64 `json:"id" binding:"required"`
}

type ShareRequest struct {
	ID uint64 `json:"id" binding:"required"`
}

// ResolveShareRequest 携带用户粘贴的原始文本。
// 不要求调用方先自行提取口令：解析规则由服务端独占，
// 前端各写一份解析逻辑必然与后端漂移。
type ResolveShareRequest struct {
	Text string `json:"text" binding:"required"`
}

// ShareInfo 是拼装分享文案所需的最小字段集。
//
// 只回 code 与展示用字段，不回完整链接：站点同时存在 HTTPS 域名和
// 明文 IP 两个入口，服务端拼 URL 就得去信任 X-Forwarded-* 头，
// 而前端用 location.origin 天然与用户当前入口一致。
type ShareInfo struct {
	VideoID  uint64 `json:"video_id"`
	Code     string `json:"code"`
	Title    string `json:"title"`
	Username string `json:"username"`
	CoverURL string `json:"cover_url"`
}
