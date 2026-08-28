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

// NewRagStore 创建以 root 为根目录的 RAG 文档存储，并确保根目录可用。
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

// Save 将用户文档原子写入指定版本文件，不覆盖其他版本。
func (s *RAGStore) Save(ctx context.Context,
	userID uint64,
	version string,
	filename string,
	content []byte,
) error {
	if userID == 0 {
		return ErrInvalidUserID
	}
	if !validVersion(version) {
		return ErrInvalidVersion
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

	documentPath := filepath.Join(userDir, version+ext)

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

	return nil
}

// Load 读取用户指定版本的文档内容，并返回实际保存的文件名。
func (s *RAGStore) Load(
	ctx context.Context,
	userID uint64,
	version string,
) ([]byte, string, error) {
	if userID == 0 {
		return nil, "", ErrInvalidUserID
	}
	if !validVersion(version) {
		return nil, "", ErrInvalidVersion
	}

	if err := ctx.Err(); err != nil {
		return nil, "", err
	}

	userDir := filepath.Join(s.root, strconv.FormatUint(userID, 10))
	for _, candidate := range []string{version + ".txt", version + ".md"} {
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

// Delete 幂等删除用户指定版本的 .txt 或 .md 文档文件。
func (s *RAGStore) Delete(
	ctx context.Context,
	userID uint64,
	version string,
) error {
	if userID == 0 {
		return ErrInvalidUserID
	}
	if !validVersion(version) {
		return ErrInvalidVersion
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	var errs error
	userDir := filepath.Join(s.root, strconv.FormatUint(userID, 10))
	for _, candidate := range []string{version + ".txt", version + ".md"} {
		if err := os.Remove(filepath.Join(userDir,
			candidate)); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = errors.Join(errs, err)
		}
	}

	return errs
}

// validVersion 判断版本名能否安全地作为单层文件名使用。
func validVersion(version string) bool {
	return version != "" && filepath.Base(version) == version && version != "." && version != ".."
}
