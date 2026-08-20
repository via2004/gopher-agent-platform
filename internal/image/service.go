package image

import (
	"bytes"
	"context"
	stdimage "image"
	_ "image/jpeg"
	_ "image/png"
)

const (
	maxImageDimension = 8192       // 最大图像尺寸
	maxImagePixels    = 25_000_000 // 最大图像像素数
)

type Service struct {
	classifier Classifier
}

func NewService(classifier Classifier) *Service {
	return &Service{
		classifier: classifier,
	}
}

func (s *Service) Recognize(ctx context.Context, data []byte) (*Result, error) {
	if len(data) == 0 {
		return nil, ErrEmptyImage
	}

	config, format, err := stdimage.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, ErrInvalidImage
	}
	if config.Width > maxImageDimension || config.Height > maxImageDimension {
		return nil, ErrInvalidImage
	}
	if config.Width*config.Height > maxImagePixels {
		return nil, ErrInvalidImage
	}

	img, _, err := stdimage.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrInvalidImage
	}

	if format != "jpeg" && format != "png" {
		return nil, ErrUnsupportedImage
	}

	className, err := s.classifier.Classify(ctx, img)
	if err != nil {
		return nil, err
	}

	return &Result{
		ClassName: className,
	}, nil
}
