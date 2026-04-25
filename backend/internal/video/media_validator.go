package video

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrInvalidPlayURL     = errors.New("play_url must reference an uploaded video")
	ErrInvalidCoverURL    = errors.New("cover_url must reference an uploaded cover")
	ErrInvalidVideoUpload = errors.New("uploaded file is not a supported video")
	ErrInvalidCoverUpload = errors.New("uploaded file is not a supported cover")
)

// MediaValidator keeps media URL checks in one place so write and read paths stay consistent.
type MediaValidator struct {
	uploadDir string
}

func NewMediaValidator(uploadDir string) MediaValidator {
	return MediaValidator{uploadDir: uploadDir}
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

func (v MediaValidator) NormalizePublishURLs(playURL string, coverURL string) (string, string, error) {
	normalizedPlayURL, ok := v.normalizeManagedURL(playURL, "videos")
	if !ok || !v.hasManagedPlayableFile(normalizedPlayURL, "videos") {
		return "", "", ErrInvalidPlayURL
	}

	normalizedCoverURL := ""
	if coverURL != "" {
		normalizedCoverURL, ok = v.normalizeManagedURL(coverURL, "covers")
		if !ok || !v.hasManagedPlayableFile(normalizedCoverURL, "covers") {
			return "", "", ErrInvalidCoverURL
		}
	}

	return normalizedPlayURL, normalizedCoverURL, nil
}

func (v MediaValidator) IsPlayable(item Video) bool {
	if !v.isManagedPlayableFile(item.PlayURL, "videos") {
		return false
	}
	if item.CoverURL != "" && !v.isManagedPlayableFile(item.CoverURL, "covers") {
		return false
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

func (v MediaValidator) isManagedPlayableFile(urlPath string, subDir string) bool {
	normalizedPath, ok := v.normalizeManagedURL(urlPath, subDir)
	if !ok {
		return false
	}

	return v.hasManagedPlayableFile(normalizedPath, subDir)
}

func (v MediaValidator) hasManagedPlayableFile(urlPath string, subDir string) bool {
	relativePrefix := "/static/" + subDir + "/"
	filename := strings.TrimPrefix(urlPath, relativePrefix)
	filePath := filepath.Join(v.uploadDir, subDir, filename)
	file, err := os.Open(filePath)
	if err != nil {
		return false
	}
	defer file.Close()

	ok, err := validateMediaContent(file, subDir)
	return err == nil && ok
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

	filename := strings.TrimPrefix(normalizedPath, relativePrefix)
	if filename == "" || filename != filepath.Base(filename) {
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
