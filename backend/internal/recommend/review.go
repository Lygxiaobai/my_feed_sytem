package recommend

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"my_feed_system/internal/like"
	"my_feed_system/internal/video"
	"my_feed_system/internal/wallet"
)

const (
	MaxInterestTags     = 8
	maxInterestTagRunes = 32
	sourceLike          = "like"
	sourceTip           = "tip"
)

// InterestTag 是管理面展示用的兴趣标签：来自点赞/打赏作品的标题或话题，不是独立标签表。
type InterestTag struct {
	Label   string `json:"label"`
	VideoID uint64 `json:"video_id"`
	Source  string `json:"source"`
}

var hashTagPattern = regexp.MustCompile(`#([^\s#]{1,32})`)

type taggedSignal struct {
	interestSignal
	Source string
}

// InterestTagsForReview 按账号投影兴趣标签。只读 likes/tips/titles，不改向量。
func (s *Service) InterestTagsForReview(accountIDs []uint64, perUser int) (map[uint64][]InterestTag, error) {
	out := make(map[uint64][]InterestTag, len(accountIDs))
	if len(accountIDs) == 0 {
		return out, nil
	}
	if perUser <= 0 || perUser > MaxInterestTags {
		perUser = MaxInterestTags
	}

	signals, err := s.loadSignals(accountIDs)
	if err != nil {
		return nil, err
	}
	videoIDs := make([]uint64, 0, len(signals))
	seenVideo := map[uint64]struct{}{}
	for _, list := range signals {
		for _, sig := range list {
			if _, ok := seenVideo[sig.VideoID]; ok {
				continue
			}
			seenVideo[sig.VideoID] = struct{}{}
			videoIDs = append(videoIDs, sig.VideoID)
		}
	}
	labels, err := s.loadVideoLabels(videoIDs)
	if err != nil {
		return nil, err
	}
	for _, accountID := range accountIDs {
		out[accountID] = tagsFromSignals(signals[accountID], labels, perUser)
	}
	return out, nil
}

func (s *Service) loadSignals(accountIDs []uint64) (map[uint64][]taggedSignal, error) {
	var likes []like.VideoLike
	if err := s.repo.db.Where("account_id IN ?", accountIDs).
		Order("created_at DESC").
		Limit(len(accountIDs) * maxInterestItems).
		Find(&likes).Error; err != nil {
		return nil, err
	}
	var tips []wallet.TipRecord
	if err := s.repo.db.Where("from_account_id IN ?", accountIDs).
		Order("created_at DESC").
		Limit(len(accountIDs) * maxInterestItems).
		Find(&tips).Error; err != nil {
		return nil, err
	}

	merged := make(map[uint64]map[uint64]taggedSignal, len(accountIDs))

	for _, row := range likes {
		byVideo := merged[row.AccountID]
		if byVideo == nil {
			byVideo = map[uint64]taggedSignal{}
			merged[row.AccountID] = byVideo
		}
		byVideo[row.VideoID] = taggedSignal{
			interestSignal: interestSignal{VideoID: row.VideoID, Weight: likeWeight, At: row.CreatedAt},
			Source:         sourceLike,
		}
	}
	for _, row := range tips {
		byVideo := merged[row.FromAccountID]
		if byVideo == nil {
			byVideo = map[uint64]taggedSignal{}
			merged[row.FromAccountID] = byVideo
		}
		cur, ok := byVideo[row.VideoID]
		if !ok {
			byVideo[row.VideoID] = taggedSignal{
				interestSignal: interestSignal{VideoID: row.VideoID, Weight: tipWeight, At: row.CreatedAt},
				Source:         sourceTip,
			}
			continue
		}
		if row.CreatedAt.After(cur.At) {
			cur.At = row.CreatedAt
		}
		cur.Weight += tipWeight
		cur.Source = sourceTip
		byVideo[row.VideoID] = cur
	}

	out := make(map[uint64][]taggedSignal, len(merged))
	for accountID, byVideo := range merged {
		list := make([]taggedSignal, 0, len(byVideo))
		for _, sig := range byVideo {
			list = append(list, sig)
		}
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].Weight == list[j].Weight {
				return list[i].At.After(list[j].At)
			}
			return list[i].Weight > list[j].Weight
		})
		if len(list) > maxInterestItems {
			list = list[:maxInterestItems]
		}
		out[accountID] = list
	}
	return out, nil
}

type videoLabel struct {
	Title       string
	Description string
}

func (s *Service) loadVideoLabels(ids []uint64) (map[uint64]videoLabel, error) {
	out := make(map[uint64]videoLabel, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []video.Video
	if err := s.repo.db.Select("id, title, description").Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		out[rows[i].ID] = videoLabel{Title: rows[i].Title, Description: rows[i].Description}
	}
	return out, nil
}

func tagsFromSignals(signals []taggedSignal, labels map[uint64]videoLabel, limit int) []InterestTag {
	if len(signals) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]InterestTag, 0, limit)
	for _, sig := range signals {
		label, ok := labels[sig.VideoID]
		if !ok {
			continue
		}
		for _, text := range interestTexts(label.Title, label.Description) {
			if _, dup := seen[text]; dup {
				continue
			}
			seen[text] = struct{}{}
			out = append(out, InterestTag{Label: text, VideoID: sig.VideoID, Source: sig.Source})
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

func interestTexts(title, description string) []string {
	combined := strings.TrimSpace(title + " " + description)
	matches := hashTagPattern.FindAllStringSubmatch(combined, MaxInterestTags)
	if len(matches) > 0 {
		out := make([]string, 0, len(matches))
		seen := map[string]struct{}{}
		for _, m := range matches {
			if len(m) < 2 {
				continue
			}
			label := compactTag(m[1])
			if label == "" {
				continue
			}
			if _, ok := seen[label]; ok {
				continue
			}
			seen[label] = struct{}{}
			out = append(out, label)
		}
		if len(out) > 0 {
			return out
		}
	}
	if title = compactTag(title); title != "" {
		return []string{title}
	}
	return nil
}

func compactTag(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxInterestTagRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxInterestTagRunes])
}
