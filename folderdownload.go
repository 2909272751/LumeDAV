package main

import (
	"archive/zip"
	"compress/flate"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	archiveStreamMaxBytes = int64(2 << 30)
	archiveStreamMaxFiles = 5000
	archiveTaskLimit      = 32
)

type folderArchiveTask struct {
	mu             sync.RWMutex
	ID             string
	Owner          string
	Source         string
	Name           string
	Key            string
	Mode           string
	Status         string
	FilePath       string
	CacheDir       string
	Error          string
	TotalBytes     int64
	ProcessedBytes int64
	TotalFiles     int
	ProcessedFiles int
	Skipped        int
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeleteAfter    time.Time
	ctx            context.Context
	cancel         context.CancelFunc
}

type folderArchiveView struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Mode           string `json:"mode,omitempty"`
	Status         string `json:"status"`
	Error          string `json:"error,omitempty"`
	TotalBytes     int64  `json:"totalBytes"`
	ProcessedBytes int64  `json:"processedBytes"`
	TotalFiles     int    `json:"totalFiles"`
	ProcessedFiles int    `json:"processedFiles"`
	Skipped        int    `json:"skipped"`
	Progress       int    `json:"progress"`
	DownloadURL    string `json:"downloadUrl,omitempty"`
}

func cacheRootDir() string {
	if root := strings.TrimSpace(os.Getenv("LUMEDAV_CACHE_DIR")); root != "" {
		return root
	}
	root, err := os.UserConfigDir()
	if err != nil || root == "" {
		root = os.TempDir()
	}
	return filepath.Join(root, "LumeDAV", "cache")
}

func archiveCacheDir() string { return filepath.Join(cacheRootDir(), "downloads") }

func (t *folderArchiveTask) view() folderArchiveView {
	t.mu.RLock()
	defer t.mu.RUnlock()
	progress := 0
	if t.TotalBytes > 0 {
		progress = int(t.ProcessedBytes * 100 / t.TotalBytes)
	} else if t.TotalFiles > 0 {
		progress = t.ProcessedFiles * 100 / t.TotalFiles
	}
	if progress > 100 {
		progress = 100
	}
	v := folderArchiveView{
		ID:             t.ID,
		Name:           t.Name,
		Mode:           t.Mode,
		Status:         t.Status,
		Error:          t.Error,
		TotalBytes:     t.TotalBytes,
		ProcessedBytes: t.ProcessedBytes,
		TotalFiles:     t.TotalFiles,
		ProcessedFiles: t.ProcessedFiles,
		Skipped:        t.Skipped,
		Progress:       progress,
	}
	if (t.Status == "ready" || t.Status == "complete") && t.Key != "" {
		v.DownloadURL = "/api/folder-download/content?id=" + url.QueryEscape(t.ID) + "&key=" + url.QueryEscape(t.Key)
	}
	return v
}

func (t *folderArchiveTask) setStatus(status string) {
	t.mu.Lock()
	t.Status = status
	t.UpdatedAt = time.Now()
	t.mu.Unlock()
}

func (t *folderArchiveTask) fail(err error) {
	t.mu.Lock()
	if errors.Is(err, context.Canceled) {
		t.Status = "cancelled"
	} else {
		t.Status = "error"
		t.Error = err.Error()
	}
	t.UpdatedAt = time.Now()
	t.DeleteAfter = time.Now().Add(10 * time.Minute)
	t.mu.Unlock()
}

func (t *folderArchiveTask) addBytes(n int64) {
	t.mu.Lock()
	t.ProcessedBytes += n
	t.UpdatedAt = time.Now()
	t.mu.Unlock()
}

func (t *folderArchiveTask) fileDone() {
	t.mu.Lock()
	t.ProcessedFiles++
	t.UpdatedAt = time.Now()
	t.mu.Unlock()
}

func (t *folderArchiveTask) skip() {
	t.mu.Lock()
	t.Skipped++
	t.mu.Unlock()
}

func (s *Server) folderArchiveStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method", http.StatusMethodNotAllowed)
		return
	}
	var request struct{ Path string }
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	source, err := s.safe(r, request.Path)
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

	s.archiveMu.Lock()
	active := 0
	for _, existing := range s.archiveTasks {
		existing.mu.RLock()
		status := existing.Status
		existing.mu.RUnlock()
		if status != "complete" && status != "cancelled" && status != "error" {
			active++
		}
	}
	if active >= archiveTaskLimit {
		s.archiveMu.Unlock()
		http.Error(w, "打包任务过多，请稍后再试", http.StatusTooManyRequests)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	name := filepath.Base(source)
	if virtualName := filepath.Base(filepath.FromSlash(strings.Trim(request.Path, "/\\"))); virtualName != "." && virtualName != "" {
		name = virtualName
	}
	task := &folderArchiveTask{
		ID:        randomID(12),
		Owner:     bearerToken(r),
		Source:    source,
		Name:      cleanArchiveName(name),
		Key:       randomID(24),
		Status:    "queued",
		CacheDir:  s.archiveDirectory(),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		ctx:       ctx,
		cancel:    cancel,
	}
	s.archiveTasks[task.ID] = task
	s.archiveMu.Unlock()

	select {
	case s.archiveQueue <- task:
		writeJSON(w, task.view())
	case <-s.archiveStop:
		cancel()
		http.Error(w, "服务正在停止", http.StatusServiceUnavailable)
	}
}

func (s *Server) folderArchiveStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method", http.StatusMethodNotAllowed)
		return
	}
	task, ok := s.ownedArchiveTask(r, r.URL.Query().Get("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, task.view())
}

func (s *Server) folderArchiveCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method", http.StatusMethodNotAllowed)
		return
	}
	task, ok := s.ownedArchiveTask(r, r.URL.Query().Get("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	task.cancel()
	task.fail(context.Canceled)
	task.mu.RLock()
	filePath := task.FilePath
	task.mu.RUnlock()
	if filePath != "" {
		_ = os.Remove(filePath)
	}
	_ = os.Remove(filepath.Join(taskCacheDir(task), task.ID+".zip.part"))
	writeJSON(w, task.view())
}

func (s *Server) folderArchiveContent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method", http.StatusMethodNotAllowed)
		return
	}
	s.archiveMu.RLock()
	task := s.archiveTasks[r.URL.Query().Get("id")]
	s.archiveMu.RUnlock()
	if task == nil {
		http.NotFound(w, r)
		return
	}
	task.mu.RLock()
	key, mode, status := task.Key, task.Mode, task.Status
	filePath, name := task.FilePath, task.Name
	task.mu.RUnlock()
	if key == "" || r.URL.Query().Get("key") != key {
		http.NotFound(w, r)
		return
	}
	if status != "ready" && status != "complete" && !(mode == "file" && status == "downloading") {
		http.Error(w, "压缩包尚未准备完成", http.StatusConflict)
		return
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name + ".zip"}))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if mode == "stream" {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		s.streamFolderArchive(w, r, task)
		return
	}
	file, err := os.Open(filePath)
	if err != nil {
		http.Error(w, "压缩包已过期，请重新生成", http.StatusGone)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if r.Method == http.MethodGet {
		task.setStatus("downloading")
	}
	http.ServeContent(w, r, name+".zip", info.ModTime(), file)
	if r.Method == http.MethodGet {
		s.downloaded.Add(info.Size())
		task.mu.Lock()
		task.Status = "complete"
		task.UpdatedAt = time.Now()
		task.DeleteAfter = time.Now().Add(10 * time.Minute)
		task.mu.Unlock()
	}
}

func (s *Server) streamFolderArchive(w http.ResponseWriter, r *http.Request, task *folderArchiveTask) {
	task.mu.Lock()
	if task.Status == "downloading" {
		task.mu.Unlock()
		http.Error(w, "该文件夹正在下载", http.StatusConflict)
		return
	}
	task.Status = "downloading"
	task.ProcessedBytes = 0
	task.ProcessedFiles = 0
	task.Error = ""
	task.UpdatedAt = time.Now()
	task.mu.Unlock()

	ctx, cancel := context.WithCancel(task.ctx)
	defer cancel()
	go func() {
		select {
		case <-r.Context().Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	err := writeFolderZip(ctx, task, w)
	if err != nil {
		if errors.Is(err, context.Canceled) && task.ctx.Err() == nil {
			task.mu.Lock()
			task.Status = "ready"
			task.Error = "传输已中断，可以重新下载"
			task.UpdatedAt = time.Now()
			task.mu.Unlock()
			return
		}
		task.fail(err)
		return
	}
	task.mu.Lock()
	task.Status = "complete"
	task.ProcessedBytes = task.TotalBytes
	task.ProcessedFiles = task.TotalFiles
	task.UpdatedAt = time.Now()
	task.DeleteAfter = time.Now().Add(10 * time.Minute)
	task.mu.Unlock()
}

func (s *Server) ownedArchiveTask(r *http.Request, id string) (*folderArchiveTask, bool) {
	s.archiveMu.RLock()
	task := s.archiveTasks[id]
	s.archiveMu.RUnlock()
	return task, task != nil && task.Owner == bearerToken(r)
}

func bearerToken(r *http.Request) string {
	return sessionToken(r)
}

func (s *Server) archiveWorker() {
	cacheDir := s.archiveDirectory()
	_ = os.MkdirAll(cacheDir, 0700)
	removeStaleArchiveFiles(cacheDir, 24*time.Hour)
	for {
		select {
		case task := <-s.archiveQueue:
			s.prepareFolderArchive(task)
		case <-s.archiveStop:
			return
		}
	}
}

func (s *Server) prepareFolderArchive(task *folderArchiveTask) {
	if task.ctx.Err() != nil {
		task.fail(task.ctx.Err())
		return
	}
	task.setStatus("scanning")
	files, bytes, err := scanFolderArchive(task.ctx, task)
	if err != nil {
		task.fail(err)
		return
	}
	task.mu.Lock()
	task.TotalFiles = files
	task.TotalBytes = bytes
	task.ProcessedBytes = 0
	task.ProcessedFiles = 0
	if files <= archiveStreamMaxFiles && bytes <= archiveStreamMaxBytes {
		task.Mode = "stream"
		task.Status = "ready"
		task.UpdatedAt = time.Now()
		task.mu.Unlock()
		return
	}
	task.Mode = "file"
	task.Status = "packing"
	task.UpdatedAt = time.Now()
	task.mu.Unlock()

	cacheDir := taskCacheDir(task)
	if err = ensureArchiveDiskSpace(cacheDir, bytes); err != nil {
		task.fail(err)
		return
	}
	if err = os.MkdirAll(cacheDir, 0700); err != nil {
		task.fail(err)
		return
	}
	part := filepath.Join(cacheDir, task.ID+".zip.part")
	final := filepath.Join(cacheDir, task.ID+".zip")
	file, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err == nil {
		err = writeFolderZip(task.ctx, task, file)
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}
	if err == nil {
		err = os.Rename(part, final)
	}
	if err != nil {
		_ = os.Remove(part)
		task.fail(err)
		return
	}
	task.mu.Lock()
	task.FilePath = final
	task.Status = "ready"
	task.ProcessedBytes = task.TotalBytes
	task.ProcessedFiles = task.TotalFiles
	task.UpdatedAt = time.Now()
	task.mu.Unlock()
}

func scanFolderArchive(ctx context.Context, task *folderArchiveTask) (int, int64, error) {
	var files int
	var bytes int64
	err := filepath.WalkDir(task.Source, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if path != task.Source && skipArchivePath(path, entry, cacheRootDir(), taskCacheDir(task), officeCacheDir()) {
			task.skip()
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files++
		bytes += info.Size()
		task.mu.Lock()
		task.TotalFiles = files
		task.TotalBytes = bytes
		task.UpdatedAt = time.Now()
		task.mu.Unlock()
		return nil
	})
	return files, bytes, err
}

func writeFolderZip(ctx context.Context, task *folderArchiveTask, output io.Writer) error {
	writer := zip.NewWriter(output)
	copyBuffer := make([]byte, 256*1024)
	writer.RegisterCompressor(zip.Deflate, func(target io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(target, flate.BestSpeed)
	})
	rootName := cleanArchiveName(task.Name)
	err := filepath.WalkDir(task.Source, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if path != task.Source && skipArchivePath(path, entry, cacheRootDir(), taskCacheDir(task), officeCacheDir()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		relative, err := filepath.Rel(task.Source, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errors.New("invalid archive path")
		}
		name := rootName
		if relative != "." {
			name += "/" + filepath.ToSlash(relative)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = name
		if entry.IsDir() {
			header.Name += "/"
			header.Method = zip.Store
			_, err = writer.CreateHeader(header)
			return err
		}
		header.Method = archiveCompressionMethod(name)
		target, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		source, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.CopyBuffer(target, &archiveProgressReader{reader: source, task: task, ctx: ctx}, copyBuffer)
		closeErr := source.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		task.fileDone()
		return nil
	})
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	return err
}

type archiveProgressReader struct {
	reader io.Reader
	task   *folderArchiveTask
	ctx    context.Context
}

func (r *archiveProgressReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(buffer)
	if n > 0 {
		r.task.addBytes(int64(n))
	}
	return n, err
}

func archiveCompressionMethod(name string) uint16 {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".zip", ".rar", ".7z", ".gz", ".bz2", ".xz", ".jpg", ".jpeg", ".png", ".gif", ".webp", ".mp3", ".mp4", ".mkv", ".avi", ".mov", ".pdf", ".docx", ".xlsx", ".pptx", ".dwg":
		return zip.Store
	default:
		return zip.Deflate
	}
}

func skipArchivePath(path string, entry fs.DirEntry, cacheDirs ...string) bool {
	if strings.EqualFold(entry.Name(), ".lumedav-trash") {
		return true
	}
	if entry.Type()&os.ModeSymlink != 0 {
		return true
	}
	if len(cacheDirs) == 0 {
		cacheDirs = []string{cacheRootDir()}
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	for _, directory := range cacheDirs {
		if strings.TrimSpace(directory) == "" {
			continue
		}
		cache, err := filepath.Abs(directory)
		if err != nil {
			continue
		}
		relative, err := filepath.Rel(cache, absolute)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) {
			return true
		}
	}
	return false
}

func taskCacheDir(task *folderArchiveTask) string {
	if strings.TrimSpace(task.CacheDir) != "" {
		return task.CacheDir
	}
	return archiveCacheDir()
}

func cleanArchiveName(name string) string {
	name = strings.TrimSpace(strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(name))
	name = strings.TrimRight(name, ". ")
	if name == "" || name == "." {
		return "文件夹"
	}
	return name
}

func (s *Server) archiveCleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	lastDiskCleanup := time.Now()
	for {
		select {
		case now := <-ticker.C:
			var remove []*folderArchiveTask
			s.archiveMu.Lock()
			for id, task := range s.archiveTasks {
				task.mu.RLock()
				deleteAfter := task.DeleteAfter
				created := task.CreatedAt
				task.mu.RUnlock()
				if (!deleteAfter.IsZero() && now.After(deleteAfter)) || now.Sub(created) > 24*time.Hour {
					delete(s.archiveTasks, id)
					remove = append(remove, task)
				}
			}
			s.archiveMu.Unlock()
			for _, task := range remove {
				task.cancel()
				task.mu.RLock()
				filePath := task.FilePath
				task.mu.RUnlock()
				if filePath != "" {
					_ = os.Remove(filePath)
				}
				_ = os.Remove(filepath.Join(taskCacheDir(task), task.ID+".zip.part"))
			}
			if now.Sub(lastDiskCleanup) >= time.Hour {
				removeStaleArchiveFiles(s.archiveDirectory(), 24*time.Hour)
				lastDiskCleanup = now
			}
		case <-s.archiveStop:
			return
		}
	}
}

func removeStaleArchiveFiles(cacheDir string, maxAge time.Duration) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, entry := range entries {
		if entry.IsDir() || (!strings.HasSuffix(entry.Name(), ".zip") && !strings.HasSuffix(entry.Name(), ".zip.part")) {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(cacheDir, entry.Name()))
		}
	}
}

func (s *Server) stopArchiveManager() {
	s.archiveStopOnce.Do(func() { close(s.archiveStop) })
	s.archiveMu.RLock()
	defer s.archiveMu.RUnlock()
	for _, task := range s.archiveTasks {
		task.cancel()
	}
}
