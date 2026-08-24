package video

import (
	"gorm.io/gorm"

	"my_feed_system/internal/audit"
)

// EffectiveLifecycle 把空值当成 published。
// 列上线前写入的行、以及缓存里尚未带回该字段的旧 payload，都必须按已发布解释，
// 否则存量内容会从信息流里消失。
func (v Video) EffectiveLifecycle() Lifecycle {
	if v.Lifecycle == "" {
		return LifecyclePublished
	}
	return v.Lifecycle
}

func (v *Video) IsDeleted() bool {
	return v != nil && v.DeletedAt != nil && !v.DeletedAt.IsZero()
}

// IsPubliclyListed 是公开表面的唯一判定：作者仍想公开、平台已过审、且未被删除。
func (v *Video) IsPubliclyListed() bool {
	if v == nil || v.IsDeleted() {
		return false
	}
	return v.EffectiveLifecycle() == LifecyclePublished && v.AuditStatus.IsPublic()
}

func (v *Video) IsDraft() bool {
	return v != nil && !v.IsDeleted() && v.EffectiveLifecycle() == LifecycleDraft
}

func (v *Video) IsUnpublished() bool {
	return v != nil && !v.IsDeleted() && v.EffectiveLifecycle() == LifecycleUnpublished
}

// ScopePublic 给面向公众的 videos 查询加上统一过滤。
// 信息流、推荐、点赞列表都必须走这里，漏掉任意一处就会把草稿或已下架内容泄漏出去。
func ScopePublic(query *gorm.DB) *gorm.DB {
	return query.
		Where("videos.audit_status = ?", audit.StatusApproved).
		Where("(videos.lifecycle = ? OR videos.lifecycle = '' OR videos.lifecycle IS NULL)", LifecyclePublished).
		Where("videos.deleted_at IS NULL")
}

// scopeAuthorOwned 作者工作室可见：未删除，且不是草稿（草稿走独立列表）。
func scopeAuthorOwned(query *gorm.DB) *gorm.DB {
	return query.
		Where("deleted_at IS NULL").
		Where("(lifecycle IS NULL OR lifecycle = '' OR lifecycle <> ?)", LifecycleDraft)
}

func scopeDrafts(query *gorm.DB) *gorm.DB {
	return query.Where("lifecycle = ?", LifecycleDraft).Where("deleted_at IS NULL")
}

// scopeReviewable 复审队列只看仍打算公开的内容。
// 草稿从未送审；已删除和已下架不应继续占着人工队列。
func scopeReviewable(query *gorm.DB) *gorm.DB {
	return query.
		Where("deleted_at IS NULL").
		Where("(lifecycle = ? OR lifecycle = '' OR lifecycle IS NULL)", LifecyclePublished)
}
