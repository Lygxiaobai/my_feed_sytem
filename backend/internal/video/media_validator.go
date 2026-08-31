package video

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/url"
	"path"
	"strings"
)

/*
*
媒体 URL 守门员

上传时验文件内容（封面走后端中转，字节在手上，校验免费）
发布时验 URL 形状，存在性由 Service 结合对象存储另行确认
读取时只验形状

**读路径不访问对象存储**：FilterPlayable / IsPlayable 在每条 feed 的每个视频上都会调用，
改造前它们各开一次本地文件读 32 字节魔数。搬到对象存储后同样的写法等于每页几十次
网络往返，因此内容校验整体前移到写路径（Worker 转码产出时 + 发布时），
读路径只做字符串形状检查。

代价：对象被外部删除或损坏时，feed 不再自动过滤该条，需要靠运维发现。
*/
var (
	ErrInvalidPlayURL     = errors.New("play_url must reference an uploaded video")
	ErrInvalidCoverURL    = errors.New("cover_url must reference an uploaded cover")
	ErrInvalidVideoUpload = errors.New("uploaded file is not a supported video")
	ErrInvalidCoverUpload = errors.New("uploaded file is not a supported cover")
)

// MediaValidator 不持有任何依赖，构造代价为零，可以在读路径上随意使用。
type MediaValidator struct{}

func NewMediaValidator() MediaValidator {
	return MediaValidator{}
}

// ValidateUploadedFile rejects placeholder text files before they reach persistent storage.
func (v MediaValidator) ValidateUploadedFile(file *multipart.FileHeader, subDir string) error {
	src, err := file.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	ok, err := validateMediaContent(src, subDir)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}

	if subDir == "videos" {
		return ErrInvalidVideoUpload
	}
	return ErrInvalidCoverUpload
}

// NormalizePublishURLs 只判定 URL 形状是否合法并归一化。
// 对象是否真的存在由 Service.normalizePublishURLs 补一次 Stat。
func (v MediaValidator) NormalizePublishURLs(playURL string, coverURL string) (string, string, error) {
	normalizedPlayURL, ok := v.normalizeManagedURL(playURL, "videos")
	if !ok {
		return "", "", ErrInvalidPlayURL
	}

	normalizedCoverURL := ""
	if coverURL != "" {
		normalizedCoverURL, ok = v.normalizeManagedURL(coverURL, "covers")
		if !ok {
			return "", "", ErrInvalidCoverURL
		}
	}

	return normalizedPlayURL, normalizedCoverURL, nil
}

func (v MediaValidator) IsPlayable(item Video) bool {
	if _, ok := v.normalizeManagedURL(item.PlayURL, "videos"); !ok {
		return false
	}
	if item.CoverURL != "" {
		if _, ok := v.normalizeManagedURL(item.CoverURL, "covers"); !ok {
			return false
		}
	}
	return true
}

func (v MediaValidator) FilterPlayable(items []Video) []Video {
	filtered := make([]Video, 0, len(items))
	for _, item := range items {
		if v.IsPlayable(item) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

// ObjectKey 把 /static/videos/1.mp4 这样的对外 URL 换算成媒体桶里的对象键 videos/1.mp4。
// 只接受已经过 normalizeManagedURL 的路径。
func ObjectKey(managedURL string) string {
	return strings.TrimPrefix(managedURL, "/static/")
}

func (v MediaValidator) normalizeManagedURL(urlPath string, subDir string) (string, bool) {
	relativePrefix := "/static/" + subDir + "/"
	normalizedPath := strings.TrimSpace(urlPath)
	if normalizedPath == "" {
		return "", false
	}

	if strings.HasPrefix(normalizedPath, "http://") || strings.HasPrefix(normalizedPath, "https://") {
		parsed, err := url.Parse(normalizedPath)
		if err != nil || parsed.Path == "" {
			return "", false
		}
		normalizedPath = parsed.Path
	}

	if !strings.HasPrefix(normalizedPath, relativePrefix) {
		return "", false
	}

	// 文件名必须是单段：挡掉 ../ 逃逸和多层前缀，避免拼出桶内任意键。
	filename := strings.TrimPrefix(normalizedPath, relativePrefix)
	if filename == "" || filename != path.Base(filename) {
		return "", false
	}

	return relativePrefix + filename, true
}

func validateMediaContent(r io.Reader, subDir string) (bool, error) {
	head := make([]byte, 32)
	n, err := io.ReadFull(r, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return false, err
	}
	head = head[:n]

	switch subDir {
	case "videos":
		return isMP4File(head), nil
	case "covers":
		return isImageFile(head), nil
	default:
		return false, nil
	}
}

func isMP4File(head []byte) bool {
	return len(head) >= 12 && bytes.Equal(head[4:8], []byte("ftyp"))
}

func isImageFile(head []byte) bool {
	return isPNGFile(head) || isJPEGFile(head) || isWEBPFile(head)
}

func isPNGFile(head []byte) bool {
	return len(head) >= 8 && bytes.Equal(head[:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
}

func isJPEGFile(head []byte) bool {
	return len(head) >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF
}

func isWEBPFile(head []byte) bool {
	return len(head) >= 12 && bytes.Equal(head[:4], []byte("RIFF")) && bytes.Equal(head[8:12], []byte("WEBP"))
}
