package main

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

var folderSizeSlots = make(chan struct{}, 2)

type folderSizeResult struct {
	Bytes      int64 `json:"bytes"`
	Files      int   `json:"files"`
	Folders    int   `json:"folders"`
	Skipped    int   `json:"skipped"`
	DurationMS int64 `json:"durationMs"`
}

func (s *Server) folderSize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method", http.StatusMethodNotAllowed)
		return
	}
	source, err := s.safe(r, r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	info, err := os.Stat(source)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !info.IsDir() {
		http.Error(w, "请选择文件夹", http.StatusBadRequest)
		return
	}
	select {
	case folderSizeSlots <- struct{}{}:
		defer func() { <-folderSizeSlots }()
	case <-r.Context().Done():
		return
	}
	started := time.Now()
	result, err := measureFolder(r.Context(), source, cacheRootDir(), s.archiveDirectory(), officeCacheDir())
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	result.DurationMS = time.Since(started).Milliseconds()
	writeJSON(w, result)
}

func measureFolder(ctx context.Context, root string, cacheDirs ...string) (folderSizeResult, error) {
	var result folderSizeResult
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			if path == root {
				return walkErr
			}
			result.Skipped++
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path != root && skipArchivePath(path, entry, cacheDirs...) {
			result.Skipped++
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if path != root {
				result.Folders++
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			result.Skipped++
			return nil
		}
		result.Files++
		result.Bytes += info.Size()
		return nil
	})
	return result, err
}
