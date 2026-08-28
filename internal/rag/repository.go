package rag

import "context"

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type DocumentStore interface {
	Save(ctx context.Context,
		userID uint64,
		version string,
		filename string,
		content []byte,
	) error

	Load(
		ctx context.Context,
		userID uint64,
		version string,
	) ([]byte, string, error)
	Delete(
		ctx context.Context,
		userID uint64,
		version string,
	) error
}

// 每个用户按 version 保存 chunks，并通过 CurrentVersion/Activate 管理生效版本。
type ChunkRepository interface {
	Replace(
		ctx context.Context,
		userID uint64,
		version string,
		chunks []Chunk,
	) error

	List(
		ctx context.Context,
		userID uint64,
		version string,
	) ([]Chunk, error)

	Delete(
		ctx context.Context,
		userID uint64,
		version string,
	) error

	CurrentVersion(ctx context.Context, userID uint64) (string, error)
	Activate(ctx context.Context, userID uint64, version string) (previous string, err error)
}
