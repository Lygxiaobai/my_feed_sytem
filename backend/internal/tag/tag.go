package tag

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	Max      = 7
	maxRunes = 32
)

var (
	hashTagPattern   = regexp.MustCompile(`#([^\s#]{1,32})`)
	leftoverSplitter = regexp.MustCompile(`[#\s,，.。;；!！?？、/|]+`)
)

// List 是存进 TEXT 的 JSON 字符串数组，API 里按字符串数组输出。
type List []string

func (l List) Value() (driver.Value, error) {
	return Encode(l), nil
}

func (l *List) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		*l = nil
	case []byte:
		*l = List(Parse(string(v)))
	case string:
		*l = List(Parse(v))
	default:
		return fmt.Errorf("unsupported tag list type %T", value)
	}
	return nil
}

func (List) GormDataType() string { return "text" }

func Compact(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	return string([]rune(s)[:maxRunes])
}

func Parse(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		return nil
	}
	return Normalize(tags)
}

func Encode(tags []string) string {
	tags = Normalize(tags)
	if len(tags) == 0 {
		return ""
	}
	raw, err := json.Marshal(tags)
	if err != nil {
		return ""
	}
	return string(raw)
}

func Normalize(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := map[string]struct{}{}
	for _, tag := range tags {
		label := Compact(tag)
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		out = append(out, label)
		if len(out) >= Max {
			break
		}
	}
	return out
}

// Merge 新 tag 追加到末尾；已存在的移到末尾。超过 7 个时丢掉最前面的。
func Merge(existing, incoming []string) []string {
	out := append([]string{}, Normalize(existing)...)
	for _, raw := range incoming {
		label := Compact(raw)
		if label == "" {
			continue
		}
		kept := make([]string, 0, len(out))
		for _, item := range out {
			if item != label {
				kept = append(kept, item)
			}
		}
		out = append(kept, label)
	}
	if len(out) > Max {
		out = append([]string{}, out[len(out)-Max:]...)
	}
	return out
}

const maxPhraseRunes = 16

// Infer 从标题和描述抽出可选标签：先 #话题，再剩下的短词/短标题。
// 整段长句不会拿来当标签。
func Infer(title, description string) []string {
	return Normalize(append(FromHashtags(title, description), leftoverPhrases(title, description)...))
}

// ForPublish：作者选了标签就只存所选；没选时才用 Infer 的结果降级。
func ForPublish(explicit []string, title, description string) List {
	chosen := Normalize(explicit)
	if len(chosen) > 0 {
		return List(chosen)
	}
	return List(Infer(title, description))
}

func leftoverPhrases(title, description string) []string {
	return append(phrasesFrom(title), phrasesFrom(description)...)
}

func phrasesFrom(raw string) []string {
	stripped := hashTagPattern.ReplaceAllString(raw, " ")
	parts := leftoverSplitter.Split(stripped, -1)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		label := Compact(part)
		if label == "" || utf8.RuneCountInString(label) > maxPhraseRunes {
			continue
		}
		out = append(out, label)
	}
	return out
}

func FromHashtags(title, description string) []string {
	combined := strings.TrimSpace(title + " " + description)
	matches := hashTagPattern.FindAllStringSubmatch(combined, Max)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	seen := map[string]struct{}{}
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		label := Compact(m[1])
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		out = append(out, label)
	}
	return out
}

func EqualFold(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}
