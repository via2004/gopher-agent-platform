package rag

import "errors"

// Request errors describe input the caller can correct.
var (
	ErrInvalidUserID           = errors.New("user ID is invalid")
	ErrInvalidContent          = errors.New("document content is invalid")
	ErrUnsupportedDocumentType = errors.New("document type must be .md or .txt")
	ErrDocumentTooLarge        = errors.New("document is too large")
	ErrInvalidEncoding         = errors.New("document must be valid UTF-8")
	ErrInvalidQuery            = errors.New("query is invalid")
	ErrInvalidTopK             = errors.New("topK is invalid")
)

// Not-found errors describe optional RAG state that does not exist.
var (
	ErrChunksNotFound   = errors.New("RAG chunks not found")
	ErrDocumentNotFound = errors.New("RAG document not found")
)

// Configuration errors indicate invalid Service dependencies.
var (
	ErrInvalidDocumentStore   = errors.New("document store is invalid")
	ErrInvalidChunkRepository = errors.New("chunk repository is invalid")
	ErrInvalidEmbedder        = errors.New("embedder is invalid")
)

// Processing errors indicate invalid vectors or dependency results.
var (
	ErrInvalidEmbeddingResult = errors.New("embedding result is invalid")
	ErrInvalidVectorSize      = errors.New("vector size is invalid")
	ErrInvalidVectorElement   = errors.New("vector must not be all zero")
)
