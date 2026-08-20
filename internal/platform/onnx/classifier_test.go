package onnx

import (
	"context"
	"errors"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewClassifierValidatesPathsBeforeRuntimeInitialization(t *testing.T) {
	dir := t.TempDir()
	sharedLibrary := filepath.Join(dir, "runtime.so")
	model := filepath.Join(dir, "model.onnx")
	labels := filepath.Join(dir, "labels.txt")
	for _, path := range []string{sharedLibrary, model, labels} {
		if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name string
		cfg  Config
		want error
	}{
		{
			name: "missing shared library",
			cfg: Config{
				SharedLibraryPath: filepath.Join(dir, "missing-runtime.so"),
				ModelPath:         model,
				LabelsPath:        labels,
			},
			want: ErrInvalidSharedLibraryPath,
		},
		{
			name: "missing model",
			cfg: Config{
				SharedLibraryPath: sharedLibrary,
				ModelPath:         filepath.Join(dir, "missing-model.onnx"),
				LabelsPath:        labels,
			},
			want: ErrInvalidModelPath,
		},
		{
			name: "missing labels",
			cfg: Config{
				SharedLibraryPath: sharedLibrary,
				ModelPath:         model,
				LabelsPath:        filepath.Join(dir, "missing-labels.txt"),
			},
			want: ErrInvalidLabelsPath,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewClassifier(tt.cfg)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestLoadLabels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "labels.txt")
	if err := os.WriteFile(path, []byte(" first \n\nsecond\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	labels, err := loadLabels(path)
	if err != nil {
		t.Fatalf("loadLabels() error = %v", err)
	}
	if got, want := strings.Join(labels, "|"), "first|second"; got != want {
		t.Fatalf("labels = %q, want %q", got, want)
	}
}

func TestClassifierRejectsInvalidImageAndClosedClassifier(t *testing.T) {
	c := &Classifier{}
	if _, err := c.Classify(context.Background(), nil); !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("nil image error = %v, want %v", err, ErrInvalidImage)
	}

	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	if _, err := c.Classify(context.Background(), img); !errors.Is(err, ErrClassifierClosed) {
		t.Fatalf("closed classifier error = %v, want %v", err, ErrClassifierClosed)
	}
}

func TestPreprocessProducesNormalizedCHWData(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 10, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 10; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 128, B: 0, A: 255})
		}
	}

	data := preprocess(img)
	planeSize := inputWidth * inputHeight
	if got, want := len(data), 3*planeSize; got != want {
		t.Fatalf("data length = %d, want %d", got, want)
	}

	checks := []struct {
		name string
		got  float32
		want float32
	}{
		{name: "red", got: data[0], want: (1 - 0.485) / 0.229},
		{name: "green", got: data[planeSize], want: (128.0/255 - 0.456) / 0.224},
		{name: "blue", got: data[2*planeSize], want: (0 - 0.406) / 0.225},
	}
	for _, check := range checks {
		if diff := check.got - check.want; diff > 0.01 || diff < -0.01 {
			t.Errorf("%s = %f, want %f", check.name, check.got, check.want)
		}
	}
}
