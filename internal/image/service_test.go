package image

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

type fakeClassifier struct {
	calls int
	class string
	err   error
}

func (f *fakeClassifier) Classify(context.Context, image.Image) (string, error) {
	f.calls++
	return f.class, f.err
}

func encodedImage(t *testing.T, format string, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 20, G: 80, B: 140, A: 255})
		}
	}
	var buf bytes.Buffer
	var err error
	if format == "png" {
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	}
	if err != nil {
		t.Fatalf("encode %s: %v", format, err)
	}
	return buf.Bytes()
}

func TestServiceRecognize(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr error
	}{
		{name: "empty", wantErr: ErrEmptyImage},
		{name: "invalid", data: []byte("not an image"), wantErr: ErrInvalidImage},
		{name: "too wide", data: encodedImage(t, "png", maxImageDimension+1, 1), wantErr: ErrInvalidImage},
		{name: "too many pixels", data: encodedImage(t, "png", 5000, 5001), wantErr: ErrInvalidImage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			classifier := &fakeClassifier{}
			_, err := NewService(classifier).Recognize(context.Background(), tt.data)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if classifier.calls != 0 {
				t.Fatalf("classifier calls = %d, want 0", classifier.calls)
			}
		})
	}
}

func TestServiceRecognizeSupportsJPEGAndPNG(t *testing.T) {
	for _, format := range []string{"jpeg", "png"} {
		t.Run(format, func(t *testing.T) {
			classifier := &fakeClassifier{class: "cat"}
			result, err := NewService(classifier).Recognize(context.Background(), encodedImage(t, format, 2, 2))
			if err != nil {
				t.Fatalf("Recognize() error = %v", err)
			}
			if result.ClassName != "cat" || classifier.calls != 1 {
				t.Fatalf("result = %#v, calls = %d", result, classifier.calls)
			}
		})
	}
}

func TestServiceRecognizePropagatesClassifierError(t *testing.T) {
	wantErr := errors.New("classifier unavailable")
	classifier := &fakeClassifier{err: wantErr}
	_, err := NewService(classifier).Recognize(context.Background(), encodedImage(t, "png", 2, 2))
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}
