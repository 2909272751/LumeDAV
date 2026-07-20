package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFolderArchiveHTTPFlowUsesSessionScope(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LUMEDAV_CACHE_DIR", t.TempDir())
	mustWriteTestFile(t, filepath.Join(root, "项目", "图纸说明.txt"), "content")
	server := NewServer(Config{Folder: root, Folders: []string{root}})
	token := "owner-token"
	server.tokens.Store(token, session{Root: root, Main: true, Expires: time.Now().Add(time.Hour)})
	body := strings.NewReader(`{"Path":"` + shareName(root) + `/项目"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/folder-download/start", body)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	server.folderArchiveStart(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("start failed: %d %s", response.Code, response.Body.String())
	}
	var started folderArchiveView
	if err := json.NewDecoder(response.Body).Decode(&started); err != nil {
		t.Fatal(err)
	}
	server.archiveMu.RLock()
	task := server.archiveTasks[started.ID]
	server.archiveMu.RUnlock()
	if task == nil || task.Owner != token {
		t.Fatal("task was not bound to the authenticated session")
	}
	server.prepareFolderArchive(task)
	ready := task.view()
	if ready.Status != "ready" || ready.Mode != "stream" || ready.DownloadURL == "" {
		t.Fatalf("unexpected ready task: %#v", ready)
	}
	downloadRequest := httptest.NewRequest(http.MethodGet, ready.DownloadURL, nil)
	downloadResponse := httptest.NewRecorder()
	server.folderArchiveContent(downloadResponse, downloadRequest)
	if downloadResponse.Code != http.StatusOK {
		t.Fatalf("download failed: %d %s", downloadResponse.Code, downloadResponse.Body.String())
	}
	zipReader, err := zip.NewReader(bytes.NewReader(downloadResponse.Body.Bytes()), int64(downloadResponse.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, file := range zipReader.File {
		if file.Name == "项目/图纸说明.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("downloaded zip missed scoped file: %#v", zipReader.File)
	}

	foreignRequest := httptest.NewRequest(http.MethodGet, "/api/folder-download/status?id="+started.ID, nil)
	foreignRequest.Header.Set("Authorization", "Bearer another-token")
	foreignResponse := httptest.NewRecorder()
	server.folderArchiveStatus(foreignResponse, foreignRequest)
	if foreignResponse.Code != http.StatusNotFound {
		t.Fatalf("another session could inspect the task: %d", foreignResponse.Code)
	}
}

func TestFolderArchiveIncludesTreeAndSkipsPrivateData(t *testing.T) {
	root := t.TempDir()
	cache := filepath.Join(root, "LumeDAVCache")
	t.Setenv("LUMEDAV_CACHE_DIR", cache)
	mustWriteTestFile(t, filepath.Join(root, "说明.txt"), "LumeDAV")
	mustWriteTestFile(t, filepath.Join(root, "子目录", "数据.json"), `{"ok":true}`)
	mustWriteTestFile(t, filepath.Join(root, "空目录", ".keep"), "")
	if err := os.Remove(filepath.Join(root, "空目录", ".keep")); err != nil {
		t.Fatal(err)
	}
	mustWriteTestFile(t, filepath.Join(root, ".lumedav-trash", "secret.txt"), "secret")
	mustWriteTestFile(t, filepath.Join(cache, "downloads", "recursive.zip.part"), "partial")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	task := &folderArchiveTask{Name: "测试目录", Source: root, ctx: ctx, cancel: cancel, Status: "scanning", CreatedAt: time.Now()}
	files, totalBytes, err := scanFolderArchive(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	if files != 2 {
		t.Fatalf("expected 2 public files, got %d", files)
	}
	if totalBytes != int64(len("LumeDAV")+len(`{"ok":true}`)) {
		t.Fatalf("unexpected byte total: %d", totalBytes)
	}
	task.TotalFiles, task.TotalBytes = files, totalBytes
	task.ProcessedBytes, task.ProcessedFiles = 0, 0
	var output bytes.Buffer
	if err = writeFolderZip(ctx, task, &output); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]string{}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			entries[file.Name] = "<dir>"
			continue
		}
		opened, openErr := file.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		content, readErr := io.ReadAll(opened)
		opened.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		entries[file.Name] = string(content)
	}
	if entries["测试目录/说明.txt"] != "LumeDAV" {
		t.Fatalf("missing Chinese filename: %#v", entries)
	}
	if entries["测试目录/子目录/数据.json"] != `{"ok":true}` {
		t.Fatalf("missing nested file: %#v", entries)
	}
	if entries["测试目录/空目录/"] != "<dir>" {
		t.Fatalf("empty directory was not preserved: %#v", entries)
	}
	for name := range entries {
		if strings.Contains(name, ".lumedav-trash") || strings.Contains(name, "LumeDAVCache") {
			t.Fatalf("private/cache path leaked into zip: %s", name)
		}
	}
}

func TestFolderArchiveContentSupportsRangeAndCleanupDelay(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("LUMEDAV_CACHE_DIR", cache)
	archive := filepath.Join(cache, "downloads", "range.zip")
	mustWriteTestFile(t, archive, "0123456789")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	task := &folderArchiveTask{
		ID:        "range",
		Name:      "中文目录",
		Key:       "secret",
		Mode:      "file",
		Status:    "ready",
		FilePath:  archive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		ctx:       ctx,
		cancel:    cancel,
	}
	server := NewServer(Config{})
	server.archiveTasks[task.ID] = task
	request := httptest.NewRequest(http.MethodGet, "/api/folder-download/content?id=range&key=secret", nil)
	request.Header.Set("Range", "bytes=2-5")
	response := httptest.NewRecorder()
	server.folderArchiveContent(response, request)
	if response.Code != http.StatusPartialContent {
		t.Fatalf("expected range response, got %d: %s", response.Code, response.Body.String())
	}
	if response.Body.String() != "2345" {
		t.Fatalf("unexpected range body: %q", response.Body.String())
	}
	if !strings.Contains(response.Header().Get("Content-Disposition"), "filename*") {
		t.Fatalf("UTF-8 filename missing: %s", response.Header().Get("Content-Disposition"))
	}
	task.mu.RLock()
	status, deleteAfter := task.Status, task.DeleteAfter
	task.mu.RUnlock()
	if status != "complete" || time.Until(deleteAfter) < 9*time.Minute {
		t.Fatalf("task was not retained for retry: %s %s", status, deleteAfter)
	}
}

func TestFolderArchiveRejectsWrongDownloadKey(t *testing.T) {
	server := NewServer(Config{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server.archiveTasks["private"] = &folderArchiveTask{ID: "private", Key: "correct", Mode: "file", Status: "ready", ctx: ctx, cancel: cancel}
	request := httptest.NewRequest(http.MethodGet, "/api/folder-download/content?id=private&key=wrong", nil)
	response := httptest.NewRecorder()
	server.folderArchiveContent(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for wrong key, got %d", response.Code)
	}
}

func mustWriteTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
