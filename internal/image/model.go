package image

import "errors"

var (
	ErrEmptyImage       = errors.New("image is empty")
	ErrInvalidImage     = errors.New("image is invalid")
	ErrUnsupportedImage = errors.New("image format is unsupported")
)

type Result struct {
	ClassName string
}
