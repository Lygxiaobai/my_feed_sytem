package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const ffmpegBinary = "ffmpeg"

// 成品在媒体桶里的键，与对外 URL 一一对应：
// videos/{taskID}.mp4  <->  /static/videos/{taskID}.mp4
func VideoKey(taskID uint64) string  { return fmt.Sprintf("videos/%d.mp4", taskID) }
func CoverKey(taskID uint64) string  { return fmt.Sprintf("covers/%d.jpg", taskID) }
func PlayURL(taskID uint64) string   { return "/static/" + VideoKey(taskID) }
func PosterURL(taskID uint64) string { return "/static/" + CoverKey(taskID) }

// Processor 只负责 ffmpeg 这一段：本地文件进，本地文件出。
// 对象存储的收发由调用方（Worker）处理，转码逻辑不必知道字节从哪来、往哪去。
type Processor struct{}

func NewProcessor() *Processor { return &Processor{} }

// Transcode 把任意 ffmpeg 可读的视频统一转成浏览器兼容的 MP4，并抽首帧做封面。
func (p *Processor) Transcode(ctx context.Context, sourcePath string, videoPath string, posterPath string) error {
	if strings.TrimSpace(sourcePath) == "" {
		return errors.New("empty media source path")
	}

	if err := normalizeVideo(ctx, sourcePath, videoPath); err != nil {
		return fmt.Errorf("transcode video: %w", err)
	}
	if err := ensureNonEmptyFile(videoPath); err != nil {
		return err
	}

	// 封面从转码产物取，而不是从源文件取：这样封面与实际播放的画面一致。
	if err := runFFmpeg(ctx, videoPath, posterPath,
		"-frames:v", "1",
		"-vf", "scale=720:-2",
		"-q:v", "3",
	); err != nil {
		return fmt.Errorf("generate video poster: %w", err)
	}
	return ensureNonEmptyFile(posterPath)
}

func normalizeVideo(ctx context.Context, input string, output string) error {
	probe, err := probeMedia(ctx, input)
	if err == nil && canRemux(probe) {
		// 已经是 H.264/AAC/yuv420p 时只重封装。整段重编码在这台 4G 机器上要数分钟。
		remuxErr := runFFmpeg(ctx, input, output,
			"-map", "0:v:0",
			"-map", "0:a?",
			"-c", "copy",
			"-movflags", "+faststart",
		)
		if remuxErr == nil {
			return nil
		}
	}
	return runFFmpeg(ctx, input, output,
		"-map", "0:v:0",
		"-map", "0:a?",
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-crf", "23",
		"-pix_fmt", "yuv420p",
		"-threads", "2",
		"-c:a", "aac",
		"-b:a", "128k",
		"-movflags", "+faststart",
	)
}

func runFFmpeg(ctx context.Context, input string, output string, options ...string) error {
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-i", input}
	args = append(args, options...)
	args = append(args, output)
	command := exec.CommandContext(ctx, ffmpegBinary, args...)
	outputBytes, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(outputBytes))
		if len(message) > 1200 {
			message = message[len(message)-1200:]
		}
		if message == "" {
			message = err.Error()
		}
		return errors.New(message)
	}
	return nil
}

func ensureNonEmptyFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("check media output: %w", err)
	}
	if info.Size() == 0 {
		return errors.New("media output is empty")
	}
	return nil
}
