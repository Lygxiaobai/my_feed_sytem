// Package storage 把对象存储（Silo / S3 兼容）收在一个接口后面。
//
// 只有一个实现。这里存在接口不是为了将来换实现，而是因为上传链路的单元测试
// 不能依赖真实的 Silo 实例——业务代码需要一个可替换的缝。
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ErrObjectNotFound 让调用方不必关心底层返回的是哪种 S3 错误码。
var ErrObjectNotFound = errors.New("object not found")

// Part 是一个已上传分片的确认信息。ETag 由对象存储在分片 PUT 的响应头里给出，
// 浏览器读到后回传，服务端凭它拼装完整对象。
type Part struct {
	PartNumber int    `json:"part_number"`
	ETag       string `json:"etag"`
}

// ObjectStore 是业务侧看到的全部存储能力。
//
// 分片相关的方法都只作用于「上传桶」，成品读写都只作用于「媒体桶」，
// 桶名不出现在签名里，避免调用方拼错桶。
type ObjectStore interface {
	// CreateMultipart 开一个分片上传，返回 uploadID。
	CreateMultipart(ctx context.Context, key string, contentType string) (string, error)
	// PresignPart 给单个分片签一个 PUT URL，浏览器直接往这个地址传，不经过后端。
	PresignPart(ctx context.Context, key string, uploadID string, partNumber int, expires time.Duration) (string, error)
	// CompleteMultipart 拼装分片并返回最终对象大小。
	CompleteMultipart(ctx context.Context, key string, uploadID string, parts []Part) (int64, error)
	// AbortMultipart 放弃一次分片上传。不调用也不会泄漏：
	// Silo 的 stale_uploads_expiry（默认 24h）会回收未完成的分片。
	AbortMultipart(ctx context.Context, key string, uploadID string) error

	// PutUpload 整文件写入上传桶，供小文件兜底入口使用。
	PutUpload(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	// GetUpload 读回源文件，供 Worker 转码使用。
	GetUpload(ctx context.Context, key string) (io.ReadCloser, error)
	// StatUpload 返回源文件大小，用于在建任务前校验实际落盘大小。
	StatUpload(ctx context.Context, key string) (int64, error)
	// RemoveUpload 删除源文件。转码成功或失败后都要调用。
	RemoveUpload(ctx context.Context, key string) error

	// PutMedia 写入成品（转码后的 mp4 与封面 jpg）。
	PutMedia(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	// StatMedia 确认成品存在，发布时校验用。
	StatMedia(ctx context.Context, key string) (int64, error)
	// RemoveMedia 删除成品。
	RemoveMedia(ctx context.Context, key string) error
}

// Config 与 config.StorageConfig 一一对应，独立声明避免 storage 反向依赖 config。
type Config struct {
	Endpoint       string
	PublicEndpoint string
	AccessKey      string
	SecretKey      string
	UseSSL         bool
	Region         string
	BucketUploads  string
	BucketMedia    string
}

// siloStore 持有两个客户端：
//   - internal 走 compose 网络，负责所有真实读写
//   - presign 指向浏览器实际连接的地址，只用来签 URL，从不发请求
//
// 两者共用同一组凭据。分成两个是因为 SigV4 把 Host 算进签名，
// 内网地址签出来的 URL 浏览器用不了。
type siloStore struct {
	internal *minio.Client
	presign  *minio.Client
	core     *minio.Core

	bucketUploads string
	bucketMedia   string
}

// New 建立到对象存储的连接。PublicEndpoint 为空时退化为用内网地址签名，
// 只适合同源直传（浏览器与后端经同一反代）的部署形态。
func New(cfg Config) (ObjectStore, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, errors.New("storage endpoint is required")
	}
	if strings.TrimSpace(cfg.BucketUploads) == "" || strings.TrimSpace(cfg.BucketMedia) == "" {
		return nil, errors.New("storage bucket names are required")
	}

	region := cfg.Region
	if region == "" {
		// 显式给 region 可以省掉 SDK 的 GetBucketLocation 探测，
		// 应用策略里也就不必放开那个动作。
		region = "us-east-1"
	}

	creds := credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, "")
	internal, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  creds,
		Secure: cfg.UseSSL,
		Region: region,
		// Silo 未配 MINIO_DOMAIN，只认路径式寻址（桶名在 path 里）。
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		return nil, fmt.Errorf("connect object storage: %w", err)
	}

	presign := internal
	if publicHost, publicSSL, ok := parsePublicEndpoint(cfg.PublicEndpoint); ok {
		presign, err = minio.New(publicHost, &minio.Options{
			Creds:        creds,
			Secure:       publicSSL,
			Region:       region,
			BucketLookup: minio.BucketLookupPath,
		})
		if err != nil {
			return nil, fmt.Errorf("build presign client: %w", err)
		}
	}

	return &siloStore{
		internal:      internal,
		presign:       presign,
		core:          &minio.Core{Client: internal},
		bucketUploads: cfg.BucketUploads,
		bucketMedia:   cfg.BucketMedia,
	}, nil
}

// parsePublicEndpoint 把 https://host:port 形式拆成 minio-go 需要的 host:port + 是否 TLS。
// 允许不带 scheme，此时按 https 处理——对外地址不该是明文。
func parsePublicEndpoint(raw string) (host string, secure bool, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false, false
	}
	if !strings.Contains(trimmed, "://") {
		return strings.TrimSuffix(trimmed, "/"), true, true
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return "", false, false
	}
	return parsed.Host, parsed.Scheme != "http", true
}

func (s *siloStore) CreateMultipart(ctx context.Context, key string, contentType string) (string, error) {
	uploadID, err := s.core.NewMultipartUpload(ctx, s.bucketUploads, key, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", fmt.Errorf("create multipart upload: %w", err)
	}
	return uploadID, nil
}

func (s *siloStore) PresignPart(ctx context.Context, key string, uploadID string, partNumber int, expires time.Duration) (string, error) {
	if partNumber < 1 {
		return "", fmt.Errorf("invalid part number %d", partNumber)
	}
	params := url.Values{}
	params.Set("uploadId", uploadID)
	params.Set("partNumber", strconv.Itoa(partNumber))

	signed, err := s.presign.Presign(ctx, "PUT", s.bucketUploads, key, expires, params)
	if err != nil {
		return "", fmt.Errorf("presign upload part: %w", err)
	}
	return signed.String(), nil
}

func (s *siloStore) CompleteMultipart(ctx context.Context, key string, uploadID string, parts []Part) (int64, error) {
	complete := make([]minio.CompletePart, 0, len(parts))
	for _, part := range parts {
		complete = append(complete, minio.CompletePart{
			PartNumber: part.PartNumber,
			// 浏览器读到的 ETag 带引号，S3 拼装时不接受，这里统一剥掉。
			ETag: strings.Trim(part.ETag, "\""),
		})
	}

	info, err := s.core.CompleteMultipartUpload(ctx, s.bucketUploads, key, uploadID, complete, minio.PutObjectOptions{})
	if err != nil {
		return 0, fmt.Errorf("complete multipart upload: %w", err)
	}
	if info.Size > 0 {
		return info.Size, nil
	}
	// 部分版本的 CompleteMultipartUpload 不回填 Size，回查一次。
	return s.StatUpload(ctx, key)
}

func (s *siloStore) AbortMultipart(ctx context.Context, key string, uploadID string) error {
	return s.core.AbortMultipartUpload(ctx, s.bucketUploads, key, uploadID)
}

func (s *siloStore) PutUpload(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	return s.put(ctx, s.bucketUploads, key, r, size, contentType)
}

func (s *siloStore) GetUpload(ctx context.Context, key string) (io.ReadCloser, error) {
	object, err := s.internal.GetObject(ctx, s.bucketUploads, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, translateNotFound(err)
	}
	// GetObject 是惰性的，错误要到第一次读才暴露。这里先 Stat 一次，
	// 让「对象不存在」在返回前就变成明确的错误，而不是留到 ffmpeg 读到一半。
	if _, err := object.Stat(); err != nil {
		_ = object.Close()
		return nil, translateNotFound(err)
	}
	return object, nil
}

func (s *siloStore) StatUpload(ctx context.Context, key string) (int64, error) {
	return s.stat(ctx, s.bucketUploads, key)
}

func (s *siloStore) RemoveUpload(ctx context.Context, key string) error {
	return s.internal.RemoveObject(ctx, s.bucketUploads, key, minio.RemoveObjectOptions{})
}

func (s *siloStore) PutMedia(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	return s.put(ctx, s.bucketMedia, key, r, size, contentType)
}

func (s *siloStore) StatMedia(ctx context.Context, key string) (int64, error) {
	return s.stat(ctx, s.bucketMedia, key)
}

func (s *siloStore) RemoveMedia(ctx context.Context, key string) error {
	return s.internal.RemoveObject(ctx, s.bucketMedia, key, minio.RemoveObjectOptions{})
}

func (s *siloStore) put(ctx context.Context, bucket string, key string, r io.Reader, size int64, contentType string) error {
	if _, err := s.internal.PutObject(ctx, bucket, key, r, size, minio.PutObjectOptions{
		ContentType: contentType,
	}); err != nil {
		return fmt.Errorf("put object %s/%s: %w", bucket, key, err)
	}
	return nil
}

func (s *siloStore) stat(ctx context.Context, bucket string, key string) (int64, error) {
	info, err := s.internal.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return 0, translateNotFound(err)
	}
	return info.Size, nil
}

func translateNotFound(err error) error {
	if err == nil {
		return nil
	}
	if minio.ToErrorResponse(err).Code == "NoSuchKey" {
		return ErrObjectNotFound
	}
	return err
}
