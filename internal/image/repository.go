package image

import (
	"context"
	stdimage "image"
)

type Classifier interface {
	Classify(
		ctx context.Context,
		img stdimage.Image,
	) (string, error)
}
