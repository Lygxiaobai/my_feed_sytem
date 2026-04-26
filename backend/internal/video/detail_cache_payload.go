package video

import "time"

const (
	// 不存在的视频只短暂缓存，避免误伤刚写入或刚恢复的内容。
	detailCacheNotFoundTTL = 5 * time.Second
)

// 使用单字节标记区分“缓存里明确不存在”和“缓存未命中”。
var detailNotFoundMarker = []byte{0}

func isDetailNotFoundPayload(payload []byte) bool {
	return len(payload) == len(detailNotFoundMarker) && payload[0] == detailNotFoundMarker[0]
}
