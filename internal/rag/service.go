package rag

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const maxDocumentBytes = 5 << 20 // 5MB

type Service struct {
	documents DocumentStore
	chunks    ChunkRepository
	embedder  Embedder
}

func NewService(documents DocumentStore, chunks ChunkRepository, embedder Embedder) (*Service, error) {
	if documents == nil {
		return nil, ErrInvalidDocuments
	}
	if chunks == nil {
		return nil, ErrInvalidChunks
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

	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".md" && ext != ".txt" {
		return nil, ErrUnsupportedDocumentType
	}
	chunks, err := SplitText(string(content))
	if err != nil {
		return nil, err
	}
	contents := make([]string, len(chunks))
	for i, chunk := range chunks {
		contents[i] = chunk.Content
	}

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

	if err := s.chunks.Replace(ctx, userID, chunks); err != nil {
		return nil, err
	}

	if err := s.documents.Save(ctx, userID, filename, content); err != nil {
		return nil, err
	}

	return &Document{
		Filename: filename,
		Size:     int64(len(content)),
		Chunks:   chunks,
	}, nil
}

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

	chunks, err := s.chunks.List(ctx, userID)
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
