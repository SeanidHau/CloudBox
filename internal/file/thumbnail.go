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
)

func SupportsThumbnail(contentType string) bool {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
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

	config, _, err := image.DecodeConfig(source)
	if err != nil {
		return fmt.Errorf("read image dimensions: %w", err)
	}
	if int64(config.Width)*int64(config.Height) > maxThumbnailSourcePixels {
		return ErrThumbnailSourceTooLarge
	}

	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return err
	}

	original, _, err := image.Decode(source)
	if err != nil {
		return fmt.Errorf("decode source image: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	width, height := thumbnailDimensions(
		original.Bounds().Dx(),
		original.Bounds().Dy(),
		ThumbnailMaxDimension,
	)

	thumbnail := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(
		thumbnail,
		thumbnail.Bounds(),
		original,
		original.Bounds(),
		draw.Over,
		nil,
	)

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, thumbnail); err != nil {
		return fmt.Errorf("encode thumbnail: %w", err)
	}

	storagePath, size, _, err := s.storage.Save(&encoded, "thumbnail.png")
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
