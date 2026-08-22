package rag

type Chunk struct {
	Index   int       `json:"index"`
	Content string    `json:"content"`
	Vector  []float32 `json:"vector"`
}

type Document struct {
	Filename string
	Size     int64
	Chunks   []Chunk
}
