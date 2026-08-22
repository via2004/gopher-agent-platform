package rag

import (
	"strings"
	"testing"
)

func TestSplitTextUsesRunesAndOverlap(t *testing.T) {
	text := strings.Repeat("你", 1100)
	chunks, err := SplitText(text)
	if err != nil {
		t.Fatalf("SplitText() error = %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d, want 2", len(chunks))
	}
	if got := len([]rune(chunks[0].Content)); got != chunkSize {
		t.Fatalf("first chunk runes = %d, want %d", got, chunkSize)
	}
	if got := len([]rune(chunks[1].Content)); got != 250 {
		t.Fatalf("second chunk runes = %d, want 250", got)
	}
	first := []rune(chunks[0].Content)
	second := []rune(chunks[1].Content)
	if string(first[len(first)-overlap:]) != string(second[:overlap]) {
		t.Fatal("adjacent chunks do not preserve the configured overlap")
	}
	if chunks[0].Index != 0 || chunks[1].Index != 1 {
		t.Fatalf("indices = %d, %d, want 0, 1", chunks[0].Index, chunks[1].Index)
	}
}

func TestSplitTextEmpty(t *testing.T) {
	chunks, err := SplitText("")
	if err != nil || len(chunks) != 0 {
		t.Fatalf("SplitText(empty) = %#v, %v, want empty result", chunks, err)
	}
}
