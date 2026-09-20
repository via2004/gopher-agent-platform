package rag

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

const maxDocumentBytes = 5 << 20 // 5MB

type Service struct {
	documents DocumentStore
	chunks    ChunkRepository
	embedder  Embedder
}

func NewService(documents DocumentStore, chunks ChunkRepository, embedder Embedder) (*Service, error) {
	if documents == nil {
		return nil, ErrInvalidDocumentStore
	}
	if chunks == nil {
		return nil, ErrInvalidChunkRepository
	}
	if embedder == nil {
		return nil, ErrInvalidEmbedder
	}

	return &Service{
		documents: documents,
		chunks:    chunks,
		embedder:  embedder,
	}, nil
}

// 用户上传的文档切片, 目前文档支持 .md 和 .txt, 只接受utf8内的字符
func (s *Service) Upload(ctx context.Context, userID uint64,
	filename string, content []byte,
) (*Document, error) {
	if userID == 0 {
		return nil, ErrInvalidUserID
	}
	if len(content) == 0 {
		return nil, ErrInvalidContent
	}
	if len(content) > maxDocumentBytes {
		return nil, ErrDocumentTooLarge
	}
	if !utf8.Valid(content) {
		return nil, ErrInvalidEncoding
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".md" && ext != ".txt" {
		return nil, ErrUnsupportedDocumentType
	}

	// 文档切片
	chunks, err := SplitText(string(content))
	if err != nil {
		return nil, err
	}
	contents := make([]string, len(chunks))
	for i, chunk := range chunks {
		contents[i] = chunk.Content
	}

	// Embedding成向量
	embedding, err := s.embedder.Embed(ctx, contents)
	if err != nil {
		return nil, err
	}

	if len(embedding) != len(chunks) {
		return nil, ErrInvalidEmbeddingResult
	}
	for i := range chunks {
		if len(embedding[i]) == 0 {
			return nil, ErrInvalidEmbeddingResult
		}
		chunks[i].Vector = embedding[i]
	}

	version := uuid.NewString()
	if err := s.documents.Save(ctx, userID, version, filename, content); err != nil {
		return nil, err
	}

	if err := s.chunks.Replace(ctx, userID, version, chunks); err != nil {
		_ = s.documents.Delete(context.WithoutCancel(ctx), userID, version)
		return nil, err
	}

	// 当前用户的current指向新版本, Activate返回之前的版本, 尽力删除之前的版本
	previous, err := s.chunks.Activate(ctx, userID, version)
	/*
		Activate 有三个步骤:
		Go 发出 Activate 请求
		→ Redis 执行 Lua，切换 current
		→ Redis 把执行结果返回给 Go
		可能 Redis 已切换 current，但返回结果时发生错误，因此保守地保留新版本原文件，避免误删已生效的资料。
	*/
	if err != nil {
		return nil, err
	}
	if previous != "" && previous != version {
		cleanupCtx := context.WithoutCancel(ctx)
		_ = s.documents.Delete(cleanupCtx, userID, previous)
	}

	return &Document{
		Version:  version,
		Filename: filename,
		Size:     int64(len(content)),
		Chunks:   chunks,
	}, nil
}

// 检索用户当前已激活文档的 RAG 资料，根据提问返回最多 topK 个相关 chunk。
func (s *Service) Retrieve(ctx context.Context, userID uint64,
	query string, topK int,
) ([]Chunk, error) {
	if userID == 0 {
		return nil, ErrInvalidUserID
	}

	query = strings.TrimSpace(query)
	if query == "" {
		return nil, ErrInvalidQuery
	}
	if topK <= 0 {
		return nil, ErrInvalidTopK
	}

	version, err := s.chunks.CurrentVersion(ctx, userID)
	if errors.Is(err, ErrChunksNotFound) {
		return nil, ErrDocumentNotFound
	}
	if err != nil {
		return nil, err
	}

	chunks, err := s.chunks.List(ctx, userID, version)
	if errors.Is(err, ErrChunksNotFound) {
		return nil, ErrDocumentNotFound
	}
	if err != nil {
		return nil, err
	}

	queryChunk, err := s.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(queryChunk) != 1 || len(queryChunk[0]) == 0 {
		return nil, ErrInvalidEmbeddingResult
	}

	topKChunk, err := SearchSimilar(chunks, queryChunk[0], topK)
	if err != nil {
		return nil, err
	}

	return topKChunk, nil
}
