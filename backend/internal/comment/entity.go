package comment

import "time"

// VideoComment 表示一条持久化到视频评论表的评论记录。
type VideoComment struct {
	ID      uint64 `gorm:"primaryKey;index:idx_video_comments_video_root_created,priority:4;index:idx_video_comments_root_created,priority:3" json:"id"`
	VideoID uint64 `gorm:"not null;index:idx_video_comments_video_root_created,priority:1" json:"video_id"`
	// RootCommentID 为 0 表示当前记录本身是根评论；回复评论则指向所属根评论。
	RootCommentID uint64 `gorm:"not null;default:0;index:idx_video_comments_video_root_created,priority:2;index:idx_video_comments_root_created,priority:1" json:"root_comment_id"`
	// ParentCommentID 表示直接父评论，根评论固定为 0。
	ParentCommentID uint64 `gorm:"not null;default:0;index:idx_video_comments_parent" json:"parent_comment_id"`
	AuthorID        uint64 `gorm:"not null;index:idx_video_comments_author_created,priority:1" json:"author_id"`
	Username        string `gorm:"size:64;not null" json:"username"`
	// ReplyToUserID 仅在回复场景下有值，用于标记当前评论正在回复谁。
	ReplyToUserID   uint64    `gorm:"not null;default:0" json:"reply_to_user_id"`
	ReplyToUsername string    `gorm:"size:64;not null;default:''" json:"reply_to_username"`
	Content         string    `gorm:"size:1000;not null" json:"content"`
	CreatedAt       time.Time `gorm:"index:idx_video_comments_video_root_created,priority:3;index:idx_video_comments_root_created,priority:2;index:idx_video_comments_author_created,priority:2" json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// TableName 指定视频评论表名。
func (VideoComment) TableName() string {
	return "video_comments"
}

// CommentItem 表示评论列表接口返回的树形节点。
type CommentItem struct {
	ID              uint64    `json:"id"`
	VideoID         uint64    `json:"video_id"`
	RootCommentID   uint64    `json:"root_comment_id"`
	ParentCommentID uint64    `json:"parent_comment_id"`
	AuthorID        uint64    `json:"author_id"`
	Username        string    `json:"username"`
	ReplyToUserID   uint64    `json:"reply_to_user_id"`
	ReplyToUsername string    `json:"reply_to_username"`
	Content         string    `json:"content"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	// ReplyCount 统计当前根评论下已挂载的直接回复数量。
	ReplyCount int `json:"reply_count"`
	// Replies 仅承载当前根评论的一层回复列表。
	Replies []CommentItem `json:"replies"`
}

// newCommentItem 把持久化评论转换成接口返回节点。
func newCommentItem(comment VideoComment) CommentItem {
	return CommentItem{
		ID:              comment.ID,
		VideoID:         comment.VideoID,
		RootCommentID:   comment.RootCommentID,
		ParentCommentID: comment.ParentCommentID,
		AuthorID:        comment.AuthorID,
		Username:        comment.Username,
		ReplyToUserID:   comment.ReplyToUserID,
		ReplyToUsername: comment.ReplyToUsername,
		Content:         comment.Content,
		CreatedAt:       comment.CreatedAt,
		UpdatedAt:       comment.UpdatedAt,
		Replies:         []CommentItem{},
	}
}

// buildCommentItems 把根评论和回复组装成两层评论树。
func buildCommentItems(roots []VideoComment, replies []VideoComment) []CommentItem {
	items := make([]CommentItem, 0, len(roots))
	rootIndex := make(map[uint64]int, len(roots))

	for _, root := range roots {
		items = append(items, newCommentItem(root))
		rootIndex[root.ID] = len(items) - 1
	}

	for _, reply := range replies {
		idx, ok := rootIndex[reply.RootCommentID]
		if !ok {
			continue
		}

		items[idx].Replies = append(items[idx].Replies, newCommentItem(reply))
		items[idx].ReplyCount = len(items[idx].Replies)
	}

	return items
}

// ListAllRequest 描述评论列表查询请求。
type ListAllRequest struct {
	VideoID uint64 `json:"video_id" binding:"required"`
}

// PublishRequest 描述评论发布请求。
type PublishRequest struct {
	VideoID         uint64 `json:"video_id" binding:"required"`
	ParentCommentID uint64 `json:"parent_comment_id"`
	Content         string `json:"content" binding:"required"`
}

// DeleteRequest 描述评论删除请求。
type DeleteRequest struct {
	CommentID uint64 `json:"comment_id" binding:"required"`
}
