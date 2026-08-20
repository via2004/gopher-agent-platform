package onnx

import "errors"

var (
	ErrInvalidImage             = errors.New("image is invalid")
	ErrInvalidSharedLibraryPath = errors.New("shared library path is invalid")
	ErrInvalidModelPath         = errors.New("model path is invalid")
	ErrInvalidLabelsPath        = errors.New("labels path is invalid")
	ErrLoadLabelFailed          = errors.New("load label failed")
	ErrClassifierClosed         = errors.New("classifier is closed")
)
