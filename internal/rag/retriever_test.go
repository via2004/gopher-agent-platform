package rag

import (
	"errors"
	"reflect"
	"testing"
)

func TestSearchSimilarReturnsTopKWithoutMutatingInput(t *testing.T) {
	chunks := []Chunk{
		{Index: 0, Content: "opposite", Vector: []float32{-1, 0}},
		{Index: 1, Content: "same", Vector: []float32{1, 0}},
		{Index: 2, Content: "orthogonal", Vector: []float32{0, 1}},
	}
	original := append([]Chunk(nil), chunks...)
	got, err := SearchSimilar(chunks, []float32{1, 0}, 2)
	if err != nil {
		t.Fatalf("SearchSimilar() error = %v", err)
	}
	if got[0].Content != "same" || got[1].Content != "orthogonal" {
		t.Fatalf("results = %#v, want same then orthogonal", got)
	}
	if !reflect.DeepEqual(chunks, original) {
		t.Fatalf("input chunks were mutated: %#v", chunks)
	}
}

func TestSearchSimilarCapsTopKAndHandlesEmptyChunks(t *testing.T) {
	got, err := SearchSimilar([]Chunk{{Vector: []float32{1}}}, []float32{1}, 4)
	if err != nil || len(got) != 1 {
		t.Fatalf("capped result = %#v, %v, want one chunk", got, err)
	}
	got, err = SearchSimilar(nil, []float32{1}, 1)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty result = %#v, %v, want empty", got, err)
	}
}

func TestSearchSimilarRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name   string
		chunks []Chunk
		query  []float32
		topK   int
		want   error
	}{
		{name: "top k", topK: 0, want: ErrInvalidTopK},
		{name: "empty query", chunks: []Chunk{{Vector: []float32{1}}}, topK: 1, want: ErrInvalidVectorSize},
		{name: "dimension", chunks: []Chunk{{Vector: []float32{1, 2}}}, query: []float32{1}, topK: 1, want: ErrInvalidVectorSize},
		{name: "zero vector", chunks: []Chunk{{Vector: []float32{0, 0}}}, query: []float32{1, 0}, topK: 1, want: ErrInvalidVectorElement},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SearchSimilar(tt.chunks, tt.query, tt.topK)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}
