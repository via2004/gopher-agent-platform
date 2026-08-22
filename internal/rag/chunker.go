package rag

import (
	"fmt"
)

const (
	chunkSize = 1000 // 1000 rune
	overlap   = 150  // 150 rune
)

func SplitText(text string) ([]Chunk, error) {
	if chunkSize <= 0 {
		return nil, fmt.Errorf("chunkSize must be a positive")
	}
	if overlap < 0 || overlap >= chunkSize {
		return nil, fmt.Errorf("overlap must be in [0, chunkSize)")
	}

	if text == "" {
		return []Chunk{}, nil
	}

	runes := []rune(text)
	total := len(runes)
	var contents []Chunk

	index := 0
	step := chunkSize - overlap // 每次前进的步长

	start := 0
	for start < total {
		end := min(total, start+chunkSize)

		contents = append(contents, Chunk{
			Index:   index,
			Content: string(runes[start:end]),
		})
		if total == end {
			break
		}
		start += step

		index++
	}

	return contents, nil
}
