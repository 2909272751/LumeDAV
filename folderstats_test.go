package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMeasureFolderCountsFilesFoldersAndBytes(t *testing.T) {
	root := t.TempDir()
	cache := filepath.Join(root, "cache")
	t.Setenv("LUMEDAV_CACHE_DIR", cache)
	for _, directory := range []string{
		filepath.Join(root, "docs"),
		filepath.Join(root, "empty"),
		filepath.Join(root, ".lumedav-trash"),
		cache,
	} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(root, "readme.txt"):                  "hello",
		filepath.Join(root, "docs", "manual.txt"):          "1234567",
		filepath.Join(root, ".lumedav-trash", "old.bin"):   "not counted",
		filepath.Join(cache, "downloads", "temporary.zip"): "not counted",
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	result, err := measureFolder(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if result.Bytes != 12 || result.Files != 2 || result.Folders != 2 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Skipped != 2 {
		t.Fatalf("skipped = %d, want 2", result.Skipped)
	}
}

func TestMeasureFolderHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := measureFolder(ctx, t.TempDir())
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestFolderSizeEndpointUsesAuthenticatedRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "sample.bin"), make([]byte, 32), 0600); err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{Folder: root})
	server.tokens.Store("folder-size-token", session{
		Root:    root,
		Main:    true,
		Expires: time.Now().Add(time.Hour),
	})
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/folder-size?path="+url.QueryEscape(shareName(root)),
		nil,
	)
	request.Header.Set("Authorization", "Bearer folder-size-token")
	recorder := httptest.NewRecorder()
	server.tokenAuth(http.HandlerFunc(server.folderSize)).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var result folderSizeResult
	if err := json.NewDecoder(recorder.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Files != 1 || result.Bytes != 32 {
		t.Fatalf("unexpected result: %#v", result)
	}
}
