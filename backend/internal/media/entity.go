package media

import "time"

const (
	StatusProcessing = "processing"
	StatusReady      = "ready"
	StatusFailed     = "failed"
)

// Task 记录一次视频源文件的异步转码任务。
// SourceKey 是源文件在上传桶里的对象键，只供 Worker 使用，不通过 API 返回。
type Task struct {
	ID           uint64    `gorm:"primaryKey" json:"id"`
	AccountID    uint64    `gorm:"not null;index:idx_media_tasks_account_created" json:"-"`
	SourceKey    string    `gorm:"column:source_path;size:512;not null" json:"-"`
	PlayURL      string    `gorm:"size:255" json:"play_url,omitempty"`
	PosterURL    string    `gorm:"size:255" json:"cover_url,omitempty"`
	ContentType  string    `gorm:"size:64;not null;default:'video/mp4'" json:"content_type"`
	Status       string    `gorm:"size:20;not null;index:idx_media_tasks_status" json:"status"`
	ErrorMessage string    `gorm:"size:2000" json:"error_message,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (Task) TableName() string {
	return "media_tasks"
}
