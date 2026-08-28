package filesystem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"gopherai/internal/rag"
)

func TestRAGStoreSaveLoadReplaceAndDelete(t *testing.T) {
	root := filepath.Join(t.TempDir(), "rag")
	store, err := NewRagStore(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.Save(ctx, 42, "v1", "notes.TXT", []byte("first")); err != nil {
		t.Fatal(err)
	}
	content, filename, err := store.Load(ctx, 42, "v1")
	if err != nil || string(content) != "first" || filename != "v1.txt" {
		t.Fatalf("Load() = %q, %q, %v", content, filename, err)
	}

	if err := store.Save(ctx, 42, "v2", "notes.md", []byte("second")); err != nil {
		t.Fatal(err)
	}
	content, filename, err = store.Load(ctx, 42, "v2")
	if err != nil || string(content) != "second" || filename != "v2.md" {
		t.Fatalf("Load() after replace = %q, %q, %v", content, filename, err)
	}
	if _, err := os.Stat(filepath.Join(root, "42", "v1.txt")); err != nil {
		t.Fatalf("versioned old document missing: %v", err)
	}

	if err := store.Delete(ctx, 42, "v2"); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, 42, "v2"); err != nil {
		t.Fatalf("second Delete() error = %v", err)
	}
	if _, _, err := store.Load(ctx, 42, "v2"); !errors.Is(err, rag.ErrDocumentNotFound) {
		t.Fatalf("Load() error = %v, want %v", err, rag.ErrDocumentNotFound)
	}
}

func TestRAGStoreRejectsInvalidInputs(t *testing.T) {
	store, err := NewRagStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		userID   uint64
		filename string
		content  []byte
		want     error
	}{
		{name: "user", filename: "a.txt", content: []byte("x"), want: ErrInvalidUserID},
		{name: "empty", userID: 1, filename: "a.txt", want: ErrEmptyDocument},
		{name: "type", userID: 1, filename: "a.pdf", content: []byte("x"), want: rag.ErrUnsupportedDocumentType},
		{name: "encoding", userID: 1, filename: "a.txt", content: []byte{0xff}, want: rag.ErrInvalidEncoding},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.Save(context.Background(), tt.userID, "v1", tt.filename, tt.content)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestRAGStoreHonorsCanceledContext(t *testing.T) {
	store, err := NewRagStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Save(ctx, 1, "v1", "a.txt", []byte("x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("Save() error = %v", err)
	}
	if _, _, err := store.Load(ctx, 1, "v1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Load() error = %v", err)
	}
	if err := store.Delete(ctx, 1, "v1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Delete() error = %v", err)
	}
}

func TestNewRAGStoreRejectsFileRoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "root")
	if err := os.WriteFile(path, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRagStore(path); err == nil {
		t.Fatal("error = nil, want invalid root error")
	}
}
