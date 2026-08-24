package recommend

import (
	"math"
	"sort"
)

type mixConfig struct {
	SmallMaxFollowers int64
	SlotSmallRatio    float64
	Lambda            float64
	AuthorWindow      int
	NearDup           float64
}

func defaultMixConfig() mixConfig {
	return mixConfig{
		SmallMaxFollowers: 50,
		SlotSmallRatio:    0.2,
		Lambda:            0.7,
		AuthorWindow:      authorWindow,
		NearDup:           nearDupCosine,
	}
}

func slotPlan(limit int, smallRatio float64) []queueKind {
	if limit <= 0 {
		return nil
	}
	smallN := int(math.Round(float64(limit) * smallRatio))
	if smallN < 1 && smallRatio > 0 && limit >= 5 {
		smallN = 1
	}
	if smallN > limit {
		smallN = limit
	}
	hotN := 0
	if limit >= 5 {
		hotN = 1
	}
	if limit >= 10 {
		hotN = 2
	}
	if smallN+hotN >= limit {
		hotN = max(0, limit-smallN-1)
	}

	plan := make([]queueKind, limit)
	for i := range plan {
		plan[i] = queueInterest
	}
	// 每 5 个位置放 1 个普通人、1 个热度：I I S I H，10 条即 2/10 普通人。
	placedSmall := 0
	placedHot := 0
	for i := 0; i < limit; i++ {
		mod := (i + 1) % 5
		if mod == 3 && placedSmall < smallN {
			plan[i] = queueSmall
			placedSmall++
			continue
		}
		if mod == 0 && placedHot < hotN {
			plan[i] = queueHot
			placedHot++
		}
	}
	for i := 0; i < limit && placedSmall < smallN; i++ {
		if plan[i] == queueInterest {
			plan[i] = queueSmall
			placedSmall++
		}
	}
	return plan
}

func splitQueues(cands []candidate, userVec []float32, cfg mixConfig) (interest, small, hot []candidate) {
	interest = make([]candidate, 0, len(cands))
	small = make([]candidate, 0, len(cands))
	hot = make([]candidate, 0, len(cands))

	for _, c := range cands {
		item := c
		if len(userVec) > 0 && len(item.Vector) > 0 {
			item.Cosine = cosine(userVec, item.Vector)
			item.Rel = math.Max(0, item.Cosine)
			interest = append(interest, item)
		}
		if item.FollowerCount < cfg.SmallMaxFollowers {
			small = append(small, item)
		}
		hot = append(hot, item)
	}

	// 没有兴趣向量时，兴趣队列退化为按发布时间，避免匿名页空窗。
	if len(userVec) == 0 {
		interest = append(interest[:0], cands...)
		sort.SliceStable(interest, func(i, j int) bool {
			if interest[i].Video.CreatedAt.Equal(interest[j].Video.CreatedAt) {
				return interest[i].Video.ID > interest[j].Video.ID
			}
			return interest[i].Video.CreatedAt.After(interest[j].Video.CreatedAt)
		})
		assignRankRel(interest)
	} else {
		sort.SliceStable(interest, func(i, j int) bool {
			if interest[i].Rel == interest[j].Rel {
				return interest[i].Video.ID > interest[j].Video.ID
			}
			return interest[i].Rel > interest[j].Rel
		})
	}

	sort.SliceStable(small, func(i, j int) bool {
		if small[i].Video.CreatedAt.Equal(small[j].Video.CreatedAt) {
			return small[i].Video.ID > small[j].Video.ID
		}
		return small[i].Video.CreatedAt.After(small[j].Video.CreatedAt)
	})
	assignRankRel(small)

	sort.SliceStable(hot, func(i, j int) bool {
		if hot[i].Hot == hot[j].Hot {
			return hot[i].Video.ID > hot[j].Video.ID
		}
		return hot[i].Hot > hot[j].Hot
	})
	assignHotRel(hot)
	return interest, small, hot
}

func assignRankRel(cands []candidate) {
	n := float64(len(cands))
	if n == 0 {
		return
	}
	for i := range cands {
		cands[i].Rel = 1 - float64(i)/n
	}
}

func assignHotRel(cands []candidate) {
	if len(cands) == 0 {
		return
	}
	minV, maxV := cands[0].Hot, cands[0].Hot
	for _, c := range cands[1:] {
		if c.Hot < minV {
			minV = c.Hot
		}
		if c.Hot > maxV {
			maxV = c.Hot
		}
	}
	span := float64(maxV - minV)
	for i := range cands {
		if span <= 0 {
			cands[i].Rel = 1
			continue
		}
		cands[i].Rel = float64(cands[i].Hot-minV) / span
	}
}

func mix(interest, small, hot []candidate, limit int, cfg mixConfig) []candidate {
	return mixExcluding(interest, small, hot, limit, cfg, nil)
}

func mixExcluding(interest, small, hot []candidate, limit int, cfg mixConfig, usedSeed map[uint64]struct{}) []candidate {
	plan := slotPlan(limit, cfg.SlotSmallRatio)
	picked := make([]candidate, 0, limit)
	used := make(map[uint64]struct{}, limit+len(usedSeed))
	for id := range usedSeed {
		used[id] = struct{}{}
	}

	take := func(pool []candidate) (candidate, []candidate, bool) {
		return pickMMR(pool, picked, used, cfg)
	}

	for _, kind := range plan {
		var item candidate
		var ok bool
		switch kind {
		case queueSmall:
			item, small, ok = take(small)
			if !ok {
				item, interest, ok = take(interest)
			}
		case queueHot:
			item, hot, ok = take(hot)
			if !ok {
				item, interest, ok = take(interest)
			}
		default:
			item, interest, ok = take(interest)
			if !ok {
				item, small, ok = take(small)
			}
		}
		if !ok {
			continue
		}
		picked = append(picked, item)
		used[item.Video.ID] = struct{}{}
	}
	return picked
}

func pickMMR(pool []candidate, picked []candidate, used map[uint64]struct{}, cfg mixConfig) (candidate, []candidate, bool) {
	idx := bestMMRIndex(pool, picked, used, cfg, true)
	if idx < 0 {
		idx = bestMMRIndex(pool, picked, used, cfg, false)
	}
	if idx < 0 {
		for i, c := range pool {
			if _, seen := used[c.Video.ID]; !seen {
				idx = i
				break
			}
		}
	}
	if idx < 0 {
		return candidate{}, pool, false
	}
	item := pool[idx]
	next := append(append([]candidate{}, pool[:idx]...), pool[idx+1:]...)
	return item, next, true
}

func bestMMRIndex(pool []candidate, picked []candidate, used map[uint64]struct{}, cfg mixConfig, strict bool) int {
	bestIdx := -1
	bestScore := math.Inf(-1)
	for i, c := range pool {
		if _, seen := used[c.Video.ID]; seen {
			continue
		}
		if strict && authorInWindow(picked, c.Video.AuthorID, cfg.AuthorWindow) {
			continue
		}
		if strict && tooSimilar(picked, c, cfg.NearDup) {
			continue
		}
		score := cfg.Lambda * c.Rel
		if len(picked) > 0 && len(c.Vector) > 0 {
			maxSim := 0.0
			for _, p := range picked {
				if len(p.Vector) == 0 {
					continue
				}
				sim := cosine(c.Vector, p.Vector)
				if sim > maxSim {
					maxSim = sim
				}
			}
			score -= (1 - cfg.Lambda) * maxSim
		}
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	return bestIdx
}

func authorInWindow(picked []candidate, authorID uint64, window int) bool {
	if window <= 0 || authorID == 0 {
		return false
	}
	start := len(picked) - window
	if start < 0 {
		start = 0
	}
	for _, item := range picked[start:] {
		if item.Video.AuthorID == authorID {
			return true
		}
	}
	return false
}

func tooSimilar(picked []candidate, c candidate, threshold float64) bool {
	if threshold <= 0 || len(c.Vector) == 0 {
		return false
	}
	for _, p := range picked {
		if len(p.Vector) == 0 {
			continue
		}
		if cosine(c.Vector, p.Vector) > threshold {
			return true
		}
	}
	return false
}

func leftoverExists(interest, small, hot []candidate, used map[uint64]struct{}) bool {
	for _, pool := range [][]candidate{interest, small, hot} {
		for _, c := range pool {
			if _, seen := used[c.Video.ID]; !seen {
				return true
			}
		}
	}
	return false
}
