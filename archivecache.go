package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
)

type ArchiveDriveInfo struct {
	Root  string `json:"root"`
	Total uint64 `json:"total"`
	Free  uint64 `json:"free"`
}

type ArchiveCacheInfo struct {
	Path       string `json:"path"`
	Total      uint64 `json:"total"`
	Free       uint64 `json:"free"`
	CacheBytes int64  `json:"cacheBytes"`
	CacheFiles int    `json:"cacheFiles"`
	Available  bool   `json:"available"`
	Error      string `json:"error,omitempty"`
}

func archiveCacheDirForConfig(configured string) string {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		return archiveCacheDir()
	}
	return normalizeArchiveCacheSelection(configured)
}

func normalizeArchiveCacheSelection(path string) string {
	clean := filepath.Clean(strings.TrimSpace(path))
	volume := filepath.VolumeName(clean)
	if volume != "" && strings.Trim(clean[len(volume):], `/\`) == "" {
		return filepath.Join(clean, "LumeDAVCache", "downloads")
	}
	return clean
}

func validateArchiveCacheDir(path string, shares []string) (string, error) {
	path = archiveCacheDirForConfig(path)
	if path == "." || filepath.VolumeName(path) == "" {
		return "", errors.New("请选择本地磁盘上的压缩缓存目录")
	}
	root := filepath.VolumeName(path) + string(filepath.Separator)
	rootPointer, err := windows.UTF16PtrFromString(root)
	if err != nil || windows.GetDriveType(rootPointer) != windows.DRIVE_FIXED {
		return "", errors.New("压缩缓存仅支持本地固定磁盘")
	}
	for _, share := range shares {
		if pathsOverlap(path, share) {
			return "", fmt.Errorf("压缩缓存目录不能与共享文件夹重叠: %s", share)
		}
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return "", fmt.Errorf("无法创建压缩缓存目录: %w", err)
	}
	probe, err := os.CreateTemp(path, ".lumedav-write-test-")
	if err != nil {
		return "", fmt.Errorf("压缩缓存目录不可写: %w", err)
	}
	probeName := probe.Name()
	closeErr := probe.Close()
	_ = os.Remove(probeName)
	if closeErr != nil {
		return "", fmt.Errorf("压缩缓存目录不可写: %w", closeErr)
	}
	return path, nil
}

func pathsOverlap(first, second string) bool {
	return pathContains(first, second) || pathContains(second, first)
}

func pathContains(parent, child string) bool {
	parent, err := filepath.Abs(parent)
	if err != nil {
		return false
	}
	child, err = filepath.Abs(child)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(parent, child)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func diskSpace(path string) (uint64, uint64, error) {
	current := filepath.Clean(path)
	for {
		if _, err := os.Stat(current); err == nil {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			return 0, 0, fmt.Errorf("磁盘不可用: %s", path)
		}
		current = parent
	}
	pointer, err := windows.UTF16PtrFromString(current)
	if err != nil {
		return 0, 0, err
	}
	var available, total, free uint64
	if err = windows.GetDiskFreeSpaceEx(pointer, &available, &total, &free); err != nil {
		return 0, 0, err
	}
	return total, available, nil
}

func ensureArchiveDiskSpace(path string, sourceBytes int64) error {
	_, free, err := diskSpace(path)
	if err != nil {
		return fmt.Errorf("压缩磁盘不可用: %w", err)
	}
	margin := int64(512 << 20)
	if sourceBytes/20 > margin {
		margin = sourceBytes / 20
	}
	required := uint64(sourceBytes + margin)
	if free < required {
		return fmt.Errorf("压缩磁盘空间不足：预计需要 %s，当前可用 %s，请在 EXE 的“压缩与缓存”中更换磁盘", formatStorageBytes(required), formatStorageBytes(free))
	}
	return nil
}

func formatStorageBytes(value uint64) string {
	const (
		gb = uint64(1 << 30)
		mb = uint64(1 << 20)
	)
	if value >= gb {
		return fmt.Sprintf("%.1f GB", float64(value)/float64(gb))
	}
	return fmt.Sprintf("%.1f MB", float64(value)/float64(mb))
}

func inspectArchiveCache(path string) ArchiveCacheInfo {
	path = archiveCacheDirForConfig(path)
	info := ArchiveCacheInfo{Path: path}
	total, free, err := diskSpace(path)
	if err != nil {
		info.Error = err.Error()
		return info
	}
	info.Total, info.Free, info.Available = total, free, true
	_ = filepath.WalkDir(path, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return nil
		}
		fileInfo, err := entry.Info()
		if err == nil {
			info.CacheFiles++
			info.CacheBytes += fileInfo.Size()
		}
		return nil
	})
	return info
}

func clearArchiveFiles(path string) error {
	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return os.MkdirAll(path, 0700)
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".zip.part") {
			if err := os.Remove(filepath.Join(path, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) archiveDirectory() string {
	s.cfgMu.RLock()
	configured := s.cfg.ArchiveCacheDir
	s.cfgMu.RUnlock()
	return archiveCacheDirForConfig(configured)
}

func (s *Server) clearArchiveCache(path string) error {
	s.archiveMu.RLock()
	defer s.archiveMu.RUnlock()
	for _, task := range s.archiveTasks {
		task.mu.RLock()
		status, taskDir := task.Status, task.CacheDir
		task.mu.RUnlock()
		if filepath.Clean(taskDir) == filepath.Clean(path) && (status == "queued" || status == "scanning" || status == "packing" || status == "ready" || status == "downloading") {
			return errors.New("当前有打包任务正在使用该目录，请完成或取消任务后再清理")
		}
	}
	return clearArchiveFiles(path)
}

func (a *App) SelectArchiveCacheFolder() (string, error) {
	a.mu.Lock()
	current := archiveCacheDirForConfig(a.cfg.ArchiveCacheDir)
	a.mu.Unlock()
	selected, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            "选择文件夹打包缓存所在磁盘或目录",
		DefaultDirectory: current,
	})
	if err != nil || selected == "" {
		return selected, err
	}
	return normalizeArchiveCacheSelection(selected), nil
}

func (a *App) GetArchiveDrives() []ArchiveDriveInfo {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil
	}
	result := []ArchiveDriveInfo{}
	for index := 0; index < 26; index++ {
		if mask&(1<<index) == 0 {
			continue
		}
		root := fmt.Sprintf("%c:\\", 'A'+index)
		pointer, err := windows.UTF16PtrFromString(root)
		if err != nil || windows.GetDriveType(pointer) != windows.DRIVE_FIXED {
			continue
		}
		total, free, err := diskSpace(root)
		if err == nil {
			result = append(result, ArchiveDriveInfo{Root: root, Total: total, Free: free})
		}
	}
	return result
}

func (a *App) InspectArchiveCache(path string) ArchiveCacheInfo {
	if strings.TrimSpace(path) == "" {
		a.mu.Lock()
		path = a.cfg.ArchiveCacheDir
		a.mu.Unlock()
	}
	return inspectArchiveCache(path)
}

func (a *App) ClearArchiveCache() (ArchiveCacheInfo, error) {
	a.mu.Lock()
	path := archiveCacheDirForConfig(a.cfg.ArchiveCacheDir)
	server := a.server
	a.mu.Unlock()
	var err error
	if server != nil {
		err = server.clearArchiveCache(path)
	} else {
		err = clearArchiveFiles(path)
	}
	return inspectArchiveCache(path), err
}

func (a *App) OpenArchiveCacheFolder() error {
	a.mu.Lock()
	path := archiveCacheDirForConfig(a.cfg.ArchiveCacheDir)
	a.mu.Unlock()
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	return exec.Command("explorer.exe", path).Start()
}
