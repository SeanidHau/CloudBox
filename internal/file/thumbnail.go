package file

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	ThumbnailMaxDimension          = 320
	maxThumbnailSourcePixels int64 = 40_000_000
)

var (
	ErrThumbUnsupportedContentType = errors.New("file type does not support thumbnail")
	ErrThumbnailSourceTooLarge     = errors.New("image is too large to create a thumbnail")
	ErrVideoThumbnailUnavailable   = errors.New("video thumbnail generator is unavailable")
)

// VideoThumbnailExtractor extracts a PNG image from a video stream. Keeping
// the external command behind this interface lets local development work
// without ffmpeg and keeps the thumbnail service straightforward to test.
type VideoThumbnailExtractor interface {
	ExtractFirstFrame(ctx context.Context, source io.Reader) ([]byte, error)
}

// FFmpegVideoThumbnailExtractor uses ffmpeg to read the first decodable video
// frame. The command is intentionally not required for image-only installs.
type FFmpegVideoThumbnailExtractor struct {
	command string
}

func NewFFmpegVideoThumbnailExtractor() *FFmpegVideoThumbnailExtractor {
	return &FFmpegVideoThumbnailExtractor{command: "ffmpeg"}
}

func (e *FFmpegVideoThumbnailExtractor) ExtractFirstFrame(
	ctx context.Context,
	source io.Reader,
) ([]byte, error) {
	if _, err := exec.LookPath(e.command); err != nil {
		return nil, ErrVideoThumbnailUnavailable
	}

	input, err := os.CreateTemp("", "cloudbox-video-*")
	if err != nil {
		return nil, err
	}
	inputPath := input.Name()
	defer os.Remove(inputPath)

	if _, err := io.Copy(input, source); err != nil {
		_ = input.Close()
		return nil, err
	}
	if err := input.Close(); err != nil {
		return nil, err
	}

	output, err := os.CreateTemp("", "cloudbox-thumbnail-*.png")
	if err != nil {
		return nil, err
	}
	outputPath := output.Name()
	if err := output.Close(); err != nil {
		_ = os.Remove(outputPath)
		return nil, err
	}
	defer os.Remove(outputPath)

	// ffmpeg performs the resize before writing the temporary PNG, which avoids
	// decoding an unbounded video frame in the Go process.
	command := exec.CommandContext(
		ctx,
		e.command,
		"-hide_banner", "-loglevel", "error", "-i", inputPath,
		"-frames:v", "1",
		"-vf", "scale=320:320:force_original_aspect_ratio=decrease",
		"-y", outputPath,
	)
	if output, err := command.CombinedOutput(); err != nil {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("extract video frame: %w: %s", err, strings.TrimSpace(string(output)))
	}

	return os.ReadFile(outputPath)
}

func SupportsThumbnail(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	switch contentType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return strings.HasPrefix(contentType, "video/")
	}
}

func (s *Service) GenerateThumbnailForActiveFile(
	ctx context.Context,
	fileID int64,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	object, err := s.repo.FindObjectForActiveFile(fileID)
	if err != nil {
		return err
	}
	if !SupportsThumbnail(object.ContentType) {
		return ErrThumbUnsupportedContentType
	}

	if _, err := s.repo.FindFilePreviewByObjectID(object.ID); err == nil {
		return nil
	} else if !errors.Is(err, ErrFilePreviewNotFound) {
		return err
	}

	source, err := s.storage.Open(object.StoragePath)
	if err != nil {
		return err
	}
	defer source.Close()

	encoded, width, height, err := s.generateThumbnail(ctx, object.ContentType, source)
	if err != nil {
		return err
	}

	storagePath, size, _, err := s.storage.Save(bytes.NewReader(encoded), "thumbnail.png")
	if err != nil {
		return err
	}

	created, err := s.repo.CreateFilePreview(&FilePreview{
		FileObjectID: object.ID,
		StoragePath:  storagePath,
		Size:         size,
		ContentType:  "image/png",
		Width:        width,
		Height:       height,
	})
	if err != nil {
		_ = s.storage.Delete(storagePath)
		return err
	}
	if !created {
		return s.storage.Delete(storagePath)
	}

	return nil
}

func (s *Service) generateThumbnail(
	ctx context.Context,
	contentType string,
	source io.ReadSeeker,
) ([]byte, int, int, error) {
	if strings.HasPrefix(strings.ToLower(contentType), "video/") {
		if s.videoThumbnailExtractor == nil {
			return nil, 0, 0, ErrVideoThumbnailUnavailable
		}
		encoded, err := s.videoThumbnailExtractor.ExtractFirstFrame(ctx, source)
		if err != nil {
			return nil, 0, 0, err
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(encoded))
		if err != nil {
			return nil, 0, 0, fmt.Errorf("read generated video thumbnail: %w", err)
		}
		return encoded, config.Width, config.Height, nil
	}

	config, _, err := image.DecodeConfig(source)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("read image dimensions: %w", err)
	}
	if int64(config.Width)*int64(config.Height) > maxThumbnailSourcePixels {
		return nil, 0, 0, ErrThumbnailSourceTooLarge
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return nil, 0, 0, err
	}

	original, _, err := image.Decode(source)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("decode source image: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, 0, err
	}

	width, height := thumbnailDimensions(original.Bounds().Dx(), original.Bounds().Dy(), ThumbnailMaxDimension)
	thumbnail := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(thumbnail, thumbnail.Bounds(), original, original.Bounds(), draw.Over, nil)

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, thumbnail); err != nil {
		return nil, 0, 0, fmt.Errorf("encode thumbnail: %w", err)
	}
	return encoded.Bytes(), width, height, nil
}

func thumbnailDimensions(width int, height int, maxDimension int) (int, int) {
	if width <= maxDimension && height <= maxDimension {
		return width, height
	}

	if width >= height {
		scaledHeight := height * maxDimension / width
		if scaledHeight < 1 {
			scaledHeight = 1
		}

		return maxDimension, scaledHeight
	}

	scaleWidth := width * maxDimension / height
	if scaleWidth < 1 {
		scaleWidth = 1
	}

	return scaleWidth, maxDimension
}
