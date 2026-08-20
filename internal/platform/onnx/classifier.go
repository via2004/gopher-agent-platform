package onnx

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	ort "github.com/yalue/onnxruntime_go"
	stdimage "image"
	"os"
	"strings"
	"sync"
)

const (
	labelSize = 1000
)

type Classifier struct {
	session      *ort.AdvancedSession // 加载好的模型
	inputTensor  *ort.Tensor[float32] // 输入
	outputTensor *ort.Tensor[float32] // 输出
	labels       []string             // 标签
	mu           sync.Mutex           // 保护复用的输入输出tensor
}

type Config struct {
	SharedLibraryPath string
	ModelPath         string
	LabelsPath        string
}

func NewClassifier(config Config) (*Classifier, error) {
	config.SharedLibraryPath = strings.TrimSpace(config.SharedLibraryPath)
	if config.SharedLibraryPath == "" {
		return nil, ErrInvalidSharedLibraryPath
	}
	if !isRegularFile(config.SharedLibraryPath) {
		return nil, ErrInvalidSharedLibraryPath
	}

	config.ModelPath = strings.TrimSpace(config.ModelPath)
	if config.ModelPath == "" {
		return nil, ErrInvalidModelPath
	}
	if !isRegularFile(config.ModelPath) {
		return nil, ErrInvalidModelPath
	}

	config.LabelsPath = strings.TrimSpace(config.LabelsPath)
	if config.LabelsPath == "" {
		return nil, ErrInvalidLabelsPath
	}
	if !isRegularFile(config.LabelsPath) {
		return nil, ErrInvalidLabelsPath
	}

	ort.SetSharedLibraryPath(config.SharedLibraryPath)
	if err := ort.InitializeEnvironment(); err != nil {
		return nil, fmt.Errorf("initialize ONNX Runtime: %w", err)
	}

	labels, err := loadLabels(config.LabelsPath)
	if err != nil || len(labels) != labelSize {
		ort.DestroyEnvironment()
		return nil, ErrLoadLabelFailed
	}

	inputShape := ort.NewShape(1, 3, 224, 224)
	inputTensor, err := ort.NewTensor(inputShape, make([]float32, inputShape.FlattenedSize()))
	if err != nil {
		ort.DestroyEnvironment()
		return nil, fmt.Errorf("new input tensor: %w", err)
	}
	outputShape := ort.NewShape(1, 1000)
	outputTensor, err := ort.NewEmptyTensor[float32](outputShape)
	if err != nil {
		ort.DestroyEnvironment()
		inputTensor.Destroy()
		return nil, fmt.Errorf("new output tensor: %w", err)
	}

	inputNames := []string{"data"}
	outputNames := []string{
		"mobilenetv20_output_flatten0_reshape0",
	}

	session, err := ort.NewAdvancedSession(config.ModelPath, inputNames, outputNames,
		[]ort.Value{inputTensor}, []ort.Value{outputTensor}, nil)
	if err != nil {
		ort.DestroyEnvironment()
		inputTensor.Destroy()
		outputTensor.Destroy()

		return nil, fmt.Errorf("new session: %w", err)
	}

	return &Classifier{
		session:      session,
		inputTensor:  inputTensor,
		outputTensor: outputTensor,
		labels:       labels,
	}, nil
}

func loadLabels(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer file.Close()

	labels := make([]string, 0, labelSize)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		label := strings.TrimSpace(scanner.Text())
		if label == "" {
			continue
		}
		labels = append(labels, label)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return labels, nil
}

func (c *Classifier) Classify(ctx context.Context, img stdimage.Image) (string, error) {
	if img == nil || img.Bounds().Empty() {
		return "", ErrInvalidImage
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	data := preprocess(img)
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.session == nil ||
		c.inputTensor == nil ||
		c.outputTensor == nil {
		return "", ErrClassifierClosed
	}

	copy(c.inputTensor.GetData(), data)

	if err := c.session.Run(); err != nil {
		return "", fmt.Errorf("run ONNX session: %w", err)
	}

	output := c.outputTensor.GetData()
	if len(output) != labelSize {
		return "", fmt.Errorf(
			"unexpected output size: %d", len(output),
		)
	}
	maxIndex := 0
	for i := 1; i < len(output); i++ {
		if output[i] > output[maxIndex] {
			maxIndex = i
		}
	}

	return c.labels[maxIndex], nil
}

func (c *Classifier) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var errs []error

	if c.session != nil {
		if err := c.session.Destroy(); err != nil {
			errs = append(errs, err)
		}
		c.session = nil
	}

	if c.outputTensor != nil {
		if err := c.outputTensor.Destroy(); err != nil {
			errs = append(errs, err)
		}
		c.outputTensor = nil
	}

	if c.inputTensor != nil {
		if err := c.inputTensor.Destroy(); err != nil {
			errs = append(errs, err)
		}
		c.inputTensor = nil
	}
	if err := ort.DestroyEnvironment(); err != nil &&
		!errors.Is(err, ort.NotInitializedError) {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
