package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"gopherai/internal/rag"
)

type RAGChunkRepository struct {
	client *redis.Client
	prefix string
}

const (
	defaultRAGPrefix     = "gopherai:rag:chunks"
	currentVersionSuffix = "current"
	stagingVersionTTL    = 24 * time.Hour
	previousVersionTTL   = 24 * time.Hour
)

// 原子发布新版本：取消新版本过期时间、切换 current，并给旧版本设置过期时间。
var activateScript = redis.NewScript(`
if redis.call("EXISTS", KEYS[2]) == 0 then
    return redis.error_reply("RAG version does not exist")
end
local previous = redis.call("GET", KEYS[1])
redis.call("PERSIST", KEYS[2])
redis.call("SET", KEYS[1], ARGV[1])
if previous and previous ~= ARGV[1] then
    redis.call("EXPIRE", ARGV[2] .. previous, ARGV[3])
end
return previous or ""
`)

// NewRAGChunkRepository 创建使用 Redis 保存版本化 RAG chunks 的仓储。
func NewRAGChunkRepository(client *redis.Client) (*RAGChunkRepository, error) {
	if client == nil {
		return nil, ErrClientInvalid
	}

	return &RAGChunkRepository{
		client: client,
		prefix: defaultRAGPrefix,
	}, nil
}

// Replace 将用户指定版本的 chunks 写入 Redis，并设置 staging TTL 防止未激活版本永久残留。
func (r *RAGChunkRepository) Replace(
	ctx context.Context,
	userID uint64,
	version string,
	chunks []rag.Chunk,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// 1. valid userID
	if userID == 0 {
		return rag.ErrInvalidUserID
	}
	if !validVersion(version) {
		return ErrInvalidVersion
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
	return r.client.Set(ctx, r.versionKey(userID, version), bytes, stagingVersionTTL).Err()
}

// List 读取用户指定版本的全部 chunks。
func (r *RAGChunkRepository) List(
	ctx context.Context,
	userID uint64,
	version string,
) ([]rag.Chunk, error) {
	if userID == 0 {
		return nil, rag.ErrInvalidUserID
	}
	if !validVersion(version) {
		return nil, ErrInvalidVersion
	}

	value, err := r.client.Get(ctx, r.versionKey(userID, version)).Bytes()
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

// Delete 幂等删除用户指定版本的 chunks，但不修改当前版本指针。
func (r *RAGChunkRepository) Delete(
	ctx context.Context,
	userID uint64,
	version string,
) error {
	if userID == 0 {
		return rag.ErrInvalidUserID
	}
	if !validVersion(version) {
		return ErrInvalidVersion
	}

	_, err := r.client.Del(ctx, r.versionKey(userID, version)).Result()
	if err != nil {
		return err
	}

	return nil
}

// CurrentVersion 返回用户当前生效的 RAG 版本。
func (r *RAGChunkRepository) CurrentVersion(ctx context.Context, userID uint64) (string, error) {
	if userID == 0 {
		return "", rag.ErrInvalidUserID
	}
	version, err := r.client.Get(ctx, r.currentKey(userID)).Result()
	if errors.Is(err, redis.Nil) {
		return "", rag.ErrChunksNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get current rag version: %w", err)
	}
	if !validVersion(version) {
		return "", ErrInvalidVersion
	}
	return version, nil
}

// Activate 原子切换用户当前版本，使新版本永久有效，并为旧版本设置清理 TTL。
func (r *RAGChunkRepository) Activate(ctx context.Context, userID uint64, version string) (string, error) {
	if userID == 0 {
		return "", rag.ErrInvalidUserID
	}
	if !validVersion(version) {
		return "", ErrInvalidVersion
	}
	versionPrefix := fmt.Sprintf("%s:%d:", r.prefix, userID)
	previous, err := activateScript.Run(
		ctx,
		r.client,
		[]string{r.currentKey(userID), r.versionKey(userID, version)},
		version,
		versionPrefix,
		int64(previousVersionTTL/time.Second),
	).Result()
	if err != nil {
		return "", fmt.Errorf("activate rag version: %w", err)
	}
	previousVersion, ok := previous.(string)
	if !ok {
		return "", fmt.Errorf("activate rag version returned %T", previous)
	}
	return previousVersion, nil
}

// versionKey 返回用户指定版本 chunks 的 Redis key。
func (r *RAGChunkRepository) versionKey(userID uint64, version string) string {
	return fmt.Sprintf("%s:%d:%s", r.prefix, userID, version)
}

// currentKey 返回用户当前版本指针的 Redis key。
func (r *RAGChunkRepository) currentKey(userID uint64) string {
	return fmt.Sprintf("%s:%d:%s", r.prefix, userID, currentVersionSuffix)
}

// validVersion 判断版本名能否安全地作为 Redis key 的单个片段。NewScript
func validVersion(version string) bool {
	return version != "" && version != "." && version != ".." &&
		version != currentVersionSuffix && !strings.ContainsAny(version, ":/\\")
}
