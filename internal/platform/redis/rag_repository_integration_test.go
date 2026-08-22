package redis

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"gopherai/internal/rag"
)

func newIntegrationRAGRepository(t *testing.T) (*RAGChunkRepository, *goredis.Client) {
	t.Helper()
	client := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:6379"})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	err := client.Ping(ctx).Err()
	cancel()
	if err != nil {
		_ = client.Close()
		t.Skipf("Redis/Valkey is unavailable: %v", err)
	}
	repository, err := NewRAGChunkRepository(client)
	if err != nil {
		t.Fatal(err)
	}
	repository.prefix = fmt.Sprintf("gopherai:test:rag:%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_ = client.Del(context.Background(), repository.key(1), repository.key(2)).Err()
		_ = client.Close()
	})
	return repository, client
}

func TestRAGChunkRepositoryReplaceListAndDelete(t *testing.T) {
	repository, client := newIntegrationRAGRepository(t)
	ctx := context.Background()
	chunks := []rag.Chunk{{Index: 0, Content: "first", Vector: []float32{1, 2}}}
	if err := repository.Replace(ctx, 1, chunks); err != nil {
		t.Fatal(err)
	}
	got, err := repository.List(ctx, 1)
	if err != nil || len(got) != 1 || got[0].Content != "first" {
		t.Fatalf("List() = %#v, %v", got, err)
	}
	if ttl, err := client.TTL(ctx, repository.key(1)).Result(); err != nil || ttl != -1 {
		t.Fatalf("TTL = %s, %v, want no expiry", ttl, err)
	}
	if err := repository.Delete(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err := repository.Delete(ctx, 1); err != nil {
		t.Fatalf("second Delete() error = %v", err)
	}
	if _, err := repository.List(ctx, 1); !errors.Is(err, rag.ErrChunksNotFound) {
		t.Fatalf("missing List() error = %v", err)
	}
}

func TestRAGChunkRepositorySeparatesUsersAndRejectsInvalidChunks(t *testing.T) {
	repository, _ := newIntegrationRAGRepository(t)
	ctx := context.Background()
	if err := repository.Replace(ctx, 1, []rag.Chunk{{Content: "one", Vector: []float32{1}}}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Replace(ctx, 2, []rag.Chunk{{Content: "two", Vector: []float32{2}}}); err != nil {
		t.Fatal(err)
	}
	one, _ := repository.List(ctx, 1)
	two, _ := repository.List(ctx, 2)
	if one[0].Content != "one" || two[0].Content != "two" {
		t.Fatalf("user data mixed: %#v %#v", one, two)
	}
	if err := repository.Replace(ctx, 1, []rag.Chunk{
		{Content: "a", Vector: []float32{1}},
		{Content: "b", Vector: []float32{1, 2}},
	}); !errors.Is(err, ErrInvalidChunkVector) {
		t.Fatalf("dimension error = %v", err)
	}
}

func TestRAGChunkRepositoryReturnsDecodeError(t *testing.T) {
	repository, client := newIntegrationRAGRepository(t)
	if err := client.Set(context.Background(), repository.key(1), "not-json", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.List(context.Background(), 1); err == nil {
		t.Fatal("List() error = nil, want JSON decode error")
	}
}
