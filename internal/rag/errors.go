package rag

import "errors"

var (
	ErrInvalidVectorSize       = errors.New("vector size is invalid")
	ErrInvalidVectorElement    = errors.New("vector element is all zero")
	ErrInvalidTopK             = errors.New("topK is invalid")
	ErrInvalidUserID           = errors.New("user id is invalid")
	ErrInvalidContent          = errors.New("content is invalid")
	ErrInvalidQuery            = errors.New("query is invalid")
	ErrChunksNotFound          = errors.New("rag chunks not found")
	ErrDocumentNotFound        = errors.New("document not found")
	ErrInvalidDocuments        = errors.New("documents is invalid")
	ErrInvalidChunks           = errors.New("chunks is invalid")
	ErrInvalidEmbedder         = errors.New("embedder is invalid")
	ErrInvalidEmbeddingResult  = errors.New("embedding result is invalid")
	ErrUnsupportedDocumentType = errors.New("doc type must be .md or .txt")
	ErrDocumentTooLarge        = errors.New("document is too large")
	ErrInvalidEncoding         = errors.New("content is not valid")
)
