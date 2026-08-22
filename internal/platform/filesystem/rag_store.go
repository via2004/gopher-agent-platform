package filesystem

import (
	"context"
	"errors"
	"fmt"
	"gopherai/internal/rag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxDocumentBytes = 5 << 20 // 5MB

type RAGStore struct {
	root string
}

func NewRagStore(root string) (*RAGStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("root is empty")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create RAG storage root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, ErrInvalidRoot
	}

	return &RAGStore{
		root: root,
	}, nil
}

func (s *RAGStore) Save(ctx context.Context,
	userID uint64,
	filename string,
	content []byte,
) error {
	if userID == 0 {
		return ErrInvalidUserID
	}
	if len(content) == 0 {
		return ErrEmptyDocument
	}
	if len(content) > maxDocumentBytes {
		return rag.ErrDocumentTooLarge
	}
	if !utf8.Valid(content) {
		return rag.ErrInvalidEncoding
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	ext := strings.ToLower(filepath.Ext(filename))
	if ext != ".md" && ext != ".txt" {
		return rag.ErrUnsupportedDocumentType
	}
	userDir := filepath.Join(s.root, strconv.FormatUint(userID, 10))
	if err := os.MkdirAll(userDir, 0o700); err != nil {
		return fmt.Errorf("create user document directory: %w", err)
	}

	documentPath := filepath.Join(userDir, "document"+ext)

	temp, err := os.CreateTemp(userDir, ".document-*")
	if err != nil {
		return err
	}

	tempPath := temp.Name()
	defer os.Remove(tempPath)

	if _, err := temp.Write(content); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}

	if err := os.Rename(tempPath, documentPath); err != nil {
		return err
	}

	// 新文件rename成功，再删除旧的
	for _, candidate := range []string{"document.txt", "document.md"} {
		if candidate == "document"+ext {
			continue
		}
		err := os.Remove(filepath.Join(userDir, candidate))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	return nil
}

func (s *RAGStore) Load(
	ctx context.Context,
	userID uint64,
) ([]byte, string, error) {
	if userID == 0 {
		return nil, "", ErrInvalidUserID
	}

	if err := ctx.Err(); err != nil {
		return nil, "", err
	}

	userDir := filepath.Join(s.root, strconv.FormatUint(userID, 10))
	for _, candidate := range []string{"document.txt", "document.md"} {
		content, err := os.ReadFile(filepath.Join(userDir, candidate))

		// 找到了，并且没有error
		if err == nil {
			return content, candidate, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, "", fmt.Errorf("read document: %w", err)
		}
	}

	return nil, "", rag.ErrDocumentNotFound
}

func (s *RAGStore) Delete(
	ctx context.Context,
	userID uint64,
) error {
	if userID == 0 {
		return ErrInvalidUserID
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	var errs error
	userDir := filepath.Join(s.root, strconv.FormatUint(userID, 10))
	for _, candidate := range []string{"document.txt", "document.md"} {
		if err := os.Remove(filepath.Join(userDir,
			candidate)); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = errors.Join(errs, err)
		}
	}

	return errs
}
