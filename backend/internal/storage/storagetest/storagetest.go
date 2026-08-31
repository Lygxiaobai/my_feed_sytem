// Package storagetest 提供 ObjectStore 的内存替身，只供测试使用。
//
// 上传链路的测试不该依赖一个真实的 Silo 实例——这是 storage 包留出接口的原因。
package storagetest

import (
	"bytes"
	"context"
	"io"
	"sync"
	"time"

	"my_feed_system/internal/storage"
)

// Store 是全内存实现，行为对齐真实实现里被业务依赖的那几条语义：
// 对象不存在时返回 storage.ErrObjectNotFound，拼装分片按 PartNumber 顺序拼接。
type Store struct {
	mu        sync.Mutex
	uploads   map[string][]byte
	media     map[string][]byte
	multipart map[string]map[int][]byte
	nextID    int
}

func New() *Store {
	return &Store{
		uploads:   make(map[string][]byte),
		media:     make(map[string][]byte),
		multipart: make(map[string]map[int][]byte),
	}
}

// SeedMedia 预置一个成品对象，让发布校验能通过。
func (s *Store) SeedMedia(key string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.media[key] = append([]byte(nil), data...)
}

// MediaKeys 返回当前媒体桶里的全部键，供断言使用。
func (s *Store) MediaKeys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.media))
	for key := range s.media {
		keys = append(keys, key)
	}
	return keys
}

func (s *Store) CreateMultipart(_ context.Context, key string, _ string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	uploadID := key + "#" + time.Now().Format("150405.000000") + "#" + itoa(s.nextID)
	s.multipart[uploadID] = make(map[int][]byte)
	return uploadID, nil
}

func (s *Store) PresignPart(_ context.Context, key string, uploadID string, partNumber int, _ time.Duration) (string, error) {
	return "https://storagetest.invalid/" + key + "?uploadId=" + uploadID + "&partNumber=" + itoa(partNumber), nil
}

// PutPart 模拟浏览器直传：真实链路里这一步不经过后端，测试中显式调用。
func (s *Store) PutPart(uploadID string, partNumber int, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if parts, ok := s.multipart[uploadID]; ok {
		parts[partNumber] = append([]byte(nil), data...)
	}
}

func (s *Store) CompleteMultipart(_ context.Context, key string, uploadID string, parts []storage.Part) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	staged, ok := s.multipart[uploadID]
	if !ok {
		return 0, storage.ErrObjectNotFound
	}
	var assembled bytes.Buffer
	for _, part := range parts {
		assembled.Write(staged[part.PartNumber])
	}
	s.uploads[key] = assembled.Bytes()
	delete(s.multipart, uploadID)
	return int64(assembled.Len()), nil
}

func (s *Store) AbortMultipart(_ context.Context, _ string, uploadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.multipart, uploadID)
	return nil
}

func (s *Store) PutUpload(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	return s.write(&s.uploads, key, r)
}

func (s *Store) GetUpload(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.uploads[key]
	if !ok {
		return nil, storage.ErrObjectNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *Store) StatUpload(_ context.Context, key string) (int64, error) {
	return s.stat(&s.uploads, key)
}

func (s *Store) RemoveUpload(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.uploads, key)
	return nil
}

func (s *Store) PutMedia(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	return s.write(&s.media, key, r)
}

func (s *Store) StatMedia(_ context.Context, key string) (int64, error) {
	return s.stat(&s.media, key)
}

func (s *Store) RemoveMedia(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.media, key)
	return nil
}

func (s *Store) write(target *map[string][]byte, key string, r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	(*target)[key] = data
	return nil
}

func (s *Store) stat(target *map[string][]byte, key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := (*target)[key]
	if !ok {
		return 0, storage.ErrObjectNotFound
	}
	return int64(len(data)), nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
