package recommend

import (
	"sort"
	"strings"

	"my_feed_system/internal/tag"
)

func matchInterestTags(cands []candidate, tags []string) []candidate {
	if len(cands) == 0 || len(tags) == 0 {
		return nil
	}
	weight := make(map[string]int, len(tags))
	for i, label := range tags {
		key := normalizeMatchKey(label)
		if key == "" {
			continue
		}
		// 越靠后的 tag 越新，权重越大。
		weight[key] = i + 1
	}
	if len(weight) == 0 {
		return nil
	}

	type scored struct {
		candidate
		score int
	}
	ranked := make([]scored, 0, len(cands))
	for _, c := range cands {
		score := 0
		for _, videoTag := range c.Video.Tags {
			key := normalizeMatchKey(videoTag)
			if w, ok := weight[key]; ok && w > score {
				score = w
			}
		}
		if score > 0 {
			ranked = append(ranked, scored{candidate: c, score: score})
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		if !ranked[i].Video.CreatedAt.Equal(ranked[j].Video.CreatedAt) {
			return ranked[i].Video.CreatedAt.After(ranked[j].Video.CreatedAt)
		}
		return ranked[i].Video.ID > ranked[j].Video.ID
	})
	out := make([]candidate, 0, len(ranked))
	for _, item := range ranked {
		out = append(out, item.candidate)
	}
	return out
}

func normalizeMatchKey(raw string) string {
	return strings.ToLower(tag.Compact(raw))
}

func leftoverAny(cands []candidate, used map[uint64]struct{}) bool {
	for _, c := range cands {
		if _, seen := used[c.Video.ID]; !seen {
			return true
		}
	}
	return false
}
