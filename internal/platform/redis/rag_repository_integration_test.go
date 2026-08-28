package redis

import (
	"context"
	"errors"
	"fmt"
	"sync"
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
		_ = client.Del(context.Background(),
			repository.currentKey(1), repository.currentKey(2),
			repository.versionKey(1, "v1"), repository.versionKey(1, "v2"),
			repository.versionKey(2, "v1"), repository.versionKey(2, "v2"),
		).Err()
		_ = client.Close()
	})
	return repository, client
}

func TestRAGChunkRepositoryReplaceListAndDelete(t *testing.T) {
	repository, client := newIntegrationRAGRepository(t)
	ctx := context.Background()
	chunks := []rag.Chunk{{Index: 0, Content: "first", Vector: []float32{1, 2}}}
	if err := repository.Replace(ctx, 1, "v1", chunks); err != nil {
		t.Fatal(err)
	}
	if ttl, err := client.TTL(ctx, repository.versionKey(1, "v1")).Result(); err != nil || ttl <= 0 || ttl > stagingVersionTTL {
		t.Fatalf("staging TTL = %s, %v, want (0, %s]", ttl, err, stagingVersionTTL)
	}
	previous, err := repository.Activate(ctx, 1, "v1")
	if err != nil || previous != "" {
		t.Fatalf("first Activate() = %q, %v", previous, err)
	}
	if current, err := repository.CurrentVersion(ctx, 1); err != nil || current != "v1" {
		t.Fatalf("CurrentVersion() = %q, %v", current, err)
	}
	got, err := repository.List(ctx, 1, "v1")
	if err != nil || len(got) != 1 || got[0].Content != "first" {
		t.Fatalf("List() = %#v, %v", got, err)
	}
	if ttl, err := client.TTL(ctx, repository.versionKey(1, "v1")).Result(); err != nil || ttl != -1 {
		t.Fatalf("TTL = %s, %v, want no expiry", ttl, err)
	}
	if err := repository.Replace(ctx, 1, "v2", chunks); err != nil {
		t.Fatal(err)
	}
	previous, err = repository.Activate(ctx, 1, "v2")
	if err != nil || previous != "v1" {
		t.Fatalf("second Activate() = %q, %v, want v1", previous, err)
	}
	if ttl, err := client.TTL(ctx, repository.versionKey(1, "v1")).Result(); err != nil || ttl <= 0 || ttl > previousVersionTTL {
		t.Fatalf("previous TTL = %s, %v, want (0, %s]", ttl, err, previousVersionTTL)
	}
	if ttl, err := client.TTL(ctx, repository.versionKey(1, "v2")).Result(); err != nil || ttl != -1 {
		t.Fatalf("active TTL = %s, %v, want no expiry", ttl, err)
	}
	if err := repository.Delete(ctx, 1, "v1"); err != nil {
		t.Fatal(err)
	}
	if err := repository.Delete(ctx, 1, "v1"); err != nil {
		t.Fatalf("second Delete() error = %v", err)
	}
	if _, err := repository.List(ctx, 1, "v1"); !errors.Is(err, rag.ErrChunksNotFound) {
		t.Fatalf("missing List() error = %v", err)
	}
}

func TestRAGChunkRepositorySeparatesUsersAndRejectsInvalidChunks(t *testing.T) {
	repository, _ := newIntegrationRAGRepository(t)
	ctx := context.Background()
	if err := repository.Replace(ctx, 1, "v1", []rag.Chunk{{Content: "one", Vector: []float32{1}}}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Replace(ctx, 2, "v1", []rag.Chunk{{Content: "two", Vector: []float32{2}}}); err != nil {
		t.Fatal(err)
	}
	previous, err := repository.Activate(ctx, 2, "v1")
	if err != nil || previous != "" {
		t.Fatalf("second user Activate() = %q, %v", previous, err)
	}
	one, _ := repository.List(ctx, 1, "v1")
	two, _ := repository.List(ctx, 2, "v1")
	if one[0].Content != "one" || two[0].Content != "two" {
		t.Fatalf("user data mixed: %#v %#v", one, two)
	}
	if err := repository.Replace(ctx, 1, "v2", []rag.Chunk{
		{Content: "a", Vector: []float32{1}},
		{Content: "b", Vector: []float32{1, 2}},
	}); !errors.Is(err, ErrInvalidChunkVector) {
		t.Fatalf("dimension error = %v", err)
	}
}

func TestRAGChunkRepositoryReturnsDecodeError(t *testing.T) {
	repository, client := newIntegrationRAGRepository(t)
	if err := client.Set(context.Background(), repository.versionKey(1, "v1"), "not-json", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.List(context.Background(), 1, "v1"); err == nil {
		t.Fatal("List() error = nil, want JSON decode error")
	}
}

func TestRAGChunkRepositoryActivateMissingVersionKeepsCurrent(t *testing.T) {
	repository, _ := newIntegrationRAGRepository(t)
	ctx := context.Background()
	chunks := []rag.Chunk{{Content: "current", Vector: []float32{1}}}
	if err := repository.Replace(ctx, 1, "v1", chunks); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Activate(ctx, 1, "v1"); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Activate(ctx, 1, "missing"); err == nil {
		t.Fatal("Activate(missing) error = nil")
	}
	if current, err := repository.CurrentVersion(ctx, 1); err != nil || current != "v1" {
		t.Fatalf("CurrentVersion() = %q, %v, want v1", current, err)
	}
}

func TestRAGChunkRepositoryConcurrentActivateLeavesCompleteCurrentVersion(t *testing.T) {
	repository, client := newIntegrationRAGRepository(t)
	ctx := context.Background()
	const versionCount = 8
	versions := make([]string, versionCount)
	keys := make([]string, 0, versionCount)
	for i := range versions {
		versions[i] = fmt.Sprintf("concurrent-%d", i)
		keys = append(keys, repository.versionKey(1, versions[i]))
		if err := repository.Replace(ctx, 1, versions[i], []rag.Chunk{{Content: versions[i], Vector: []float32{1}}}); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = client.Del(context.Background(), keys...).Err() })

	var wg sync.WaitGroup
	errs := make(chan error, versionCount)
	for _, version := range versions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repository.Activate(ctx, 1, version)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Activate() error = %v", err)
		}
	}

	current, err := repository.CurrentVersion(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	currentChunks, err := repository.List(ctx, 1, current)
	if err != nil || len(currentChunks) != 1 || currentChunks[0].Content != current {
		t.Fatalf("current data = %#v, %v for version %q", currentChunks, err, current)
	}
	for _, version := range versions {
		ttl, err := client.TTL(ctx, repository.versionKey(1, version)).Result()
		if err != nil {
			t.Fatal(err)
		}
		if version == current && ttl != -1 {
			t.Fatalf("current version %q TTL = %s, want no expiry", version, ttl)
		}
		if version != current && (ttl <= 0 || ttl > previousVersionTTL) {
			t.Fatalf("old version %q TTL = %s, want (0, %s]", version, ttl, previousVersionTTL)
		}
	}
}
