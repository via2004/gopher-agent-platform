package rag

import "context"

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type DocumentStore interface {
	Save(ctx context.Context,
		userID uint64,
		filename string,
		content []byte,
	) error

	Load(
		ctx context.Context,
		userID uint64,
	) ([]byte, string, error)
	Delete(
		ctx context.Context,
		userID uint64,
	) error
}

// 每一个用户一个redis key, gopherai:rag:chunks:{userID}
type ChunkRepository interface {
	Replace(
		ctx context.Context,
		userID uint64,
		chunks []Chunk,
	) error

	List(
		ctx context.Context,
		userID uint64,
	) ([]Chunk, error)

	Delete(
		ctx context.Context,
		userID uint64,
	) error
}
