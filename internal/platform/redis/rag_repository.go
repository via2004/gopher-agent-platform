package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/redis/go-redis/v9"
	"gopherai/internal/rag"
)

type RAGChunkRepository struct {
	client *redis.Client
	prefix string
}

const (
	defaultRAGPrefix = "gopherai:rag:chunks"
)

func NewRAGChunkRepository(client *redis.Client) (*RAGChunkRepository, error) {
	if client == nil {
		return nil, ErrClientInvalid
	}

	return &RAGChunkRepository{
		client: client,
		prefix: defaultRAGPrefix,
	}, nil
}

// need to make sure the userID is valid
func (r *RAGChunkRepository) key(userID uint64) string {
	return fmt.Sprintf("%s:%d", r.prefix, userID)
}

func (r *RAGChunkRepository) Replace(
	ctx context.Context,
	userID uint64,
	chunks []rag.Chunk,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// 1. valid userID
	if userID == 0 {
		return rag.ErrInvalidUserID
	}
	// 2. valid chunks is not empty
	if len(chunks) == 0 {
		return ErrEmptyChunk
	}

	// 3&4&5. valid every chunk's Content And Vector is not empty
	for i := 0; i < len(chunks); i++ {
		if len(chunks[i].Content) == 0 {
			return ErrEmptyChunkContent
		}

		if len(chunks[i].Vector) == 0 {
			return ErrEmptyChunkVector
		}
		if i > 0 && len(chunks[i].Vector) != len(chunks[i-1].Vector) {
			return ErrInvalidChunkVector
		}
	}

	bytes, err := json.Marshal(chunks)
	if err != nil {
		return fmt.Errorf("json marshal: %w", err)
	}
	return r.client.Set(ctx, r.key(userID), bytes, 0).Err()
}

func (r *RAGChunkRepository) List(
	ctx context.Context,
	userID uint64,
) ([]rag.Chunk, error) {
	if userID == 0 {
		return nil, rag.ErrInvalidUserID
	}

	value, err := r.client.Get(ctx, r.key(userID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, rag.ErrChunksNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get rag chunks: %w", err)
	}

	chunks := []rag.Chunk{}
	if err := json.Unmarshal(value, &chunks); err != nil {
		return nil, fmt.Errorf("decode rag chunks: %w", err)
	}

	return chunks, nil
}

func (r *RAGChunkRepository) Delete(
	ctx context.Context,
	userID uint64,
) error {
	if userID == 0 {
		return rag.ErrInvalidUserID
	}

	_, err := r.client.Del(ctx, r.key(userID)).Result()
	if err != nil {
		return err
	}

	return nil
}
