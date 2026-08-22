package rag

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeDocumentStore struct {
	savedFilename string
	savedContent  []byte
	err           error
}

func (f *fakeDocumentStore) Save(_ context.Context, _ uint64, filename string, content []byte) error {
	f.savedFilename = filename
	f.savedContent = append([]byte(nil), content...)
	return f.err
}
func (*fakeDocumentStore) Load(context.Context, uint64) ([]byte, string, error) { return nil, "", nil }
func (*fakeDocumentStore) Delete(context.Context, uint64) error                 { return nil }

type fakeChunkRepository struct {
	stored  []Chunk
	listed  []Chunk
	listErr error
	err     error
}

func (f *fakeChunkRepository) Replace(_ context.Context, _ uint64, chunks []Chunk) error {
	f.stored = append([]Chunk(nil), chunks...)
	return f.err
}
func (f *fakeChunkRepository) List(context.Context, uint64) ([]Chunk, error) {
	return f.listed, f.listErr
}
func (*fakeChunkRepository) Delete(context.Context, uint64) error { return nil }

type fakeEmbedder struct {
	inputs  []string
	vectors [][]float32
	err     error
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.inputs = append([]string(nil), texts...)
	return f.vectors, f.err
}

func newTestRAGService(t *testing.T, documents DocumentStore, chunks ChunkRepository, embedder Embedder) *Service {
	t.Helper()
	service, err := NewService(documents, chunks, embedder)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestServiceUploadBuildsAndPersistsChunks(t *testing.T) {
	documents := &fakeDocumentStore{}
	repository := &fakeChunkRepository{}
	embedder := &fakeEmbedder{vectors: [][]float32{{1, 2}}}
	service := newTestRAGService(t, documents, repository, embedder)

	document, err := service.Upload(context.Background(), 42, "notes.md", []byte("hello"))
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if len(embedder.inputs) != 1 || embedder.inputs[0] != "hello" {
		t.Fatalf("embedding inputs = %#v", embedder.inputs)
	}
	if len(repository.stored) != 1 || len(repository.stored[0].Vector) != 2 {
		t.Fatalf("stored chunks = %#v", repository.stored)
	}
	if document.Filename != "notes.md" || document.Size != 5 || documents.savedFilename != "notes.md" {
		t.Fatalf("document = %#v, saved filename = %q", document, documents.savedFilename)
	}
}

func TestServiceUploadRejectsInvalidInputBeforeDependencies(t *testing.T) {
	embedder := &fakeEmbedder{}
	service := newTestRAGService(t, &fakeDocumentStore{}, &fakeChunkRepository{}, embedder)
	_, err := service.Upload(context.Background(), 42, "notes.pdf", []byte("hello"))
	if !errors.Is(err, ErrUnsupportedDocumentType) || len(embedder.inputs) != 0 {
		t.Fatalf("Upload() = %v, inputs = %#v", err, embedder.inputs)
	}
	_, err = service.Upload(context.Background(), 42, "notes.txt", []byte{0xff})
	if !errors.Is(err, ErrInvalidEncoding) {
		t.Fatalf("invalid UTF-8 error = %v", err)
	}
}

func TestServiceUploadRejectsInvalidEmbeddingResult(t *testing.T) {
	service := newTestRAGService(t, &fakeDocumentStore{}, &fakeChunkRepository{}, &fakeEmbedder{})
	_, err := service.Upload(context.Background(), 42, "notes.txt", []byte("hello"))
	if !errors.Is(err, ErrInvalidEmbeddingResult) {
		t.Fatalf("error = %v, want %v", err, ErrInvalidEmbeddingResult)
	}
}

func TestServiceRetrieveFindsRelevantChunks(t *testing.T) {
	repository := &fakeChunkRepository{listed: []Chunk{
		{Content: "unrelated", Vector: []float32{0, 1}},
		{Content: "relevant", Vector: []float32{1, 0}},
	}}
	embedder := &fakeEmbedder{vectors: [][]float32{{1, 0}}}
	service := newTestRAGService(t, &fakeDocumentStore{}, repository, embedder)
	got, err := service.Retrieve(context.Background(), 42, " question ", 1)
	if err != nil || len(got) != 1 || got[0].Content != "relevant" {
		t.Fatalf("Retrieve() = %#v, %v", got, err)
	}
	if strings.Join(embedder.inputs, "") != "question" {
		t.Fatalf("embedding input = %#v", embedder.inputs)
	}
}

func TestServiceRetrieveDoesNotEmbedMissingDocument(t *testing.T) {
	embedder := &fakeEmbedder{}
	service := newTestRAGService(t, &fakeDocumentStore{}, &fakeChunkRepository{listErr: ErrChunksNotFound}, embedder)
	_, err := service.Retrieve(context.Background(), 42, "question", 1)
	if !errors.Is(err, ErrDocumentNotFound) || len(embedder.inputs) != 0 {
		t.Fatalf("Retrieve() = %v, embedding inputs = %#v", err, embedder.inputs)
	}
}
