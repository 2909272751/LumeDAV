package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeArchiveCacheDriveRoot(t *testing.T) {
	want := filepath.Clean(`D:\LumeDAVCache\downloads`)
	if got := normalizeArchiveCacheSelection(`D:\`); got != want {
		t.Fatalf("normalize drive root = %q, want %q", got, want)
	}
}

func TestValidateArchiveCacheRejectsSharedFolderOverlap(t *testing.T) {
	share := t.TempDir()
	cache := filepath.Join(share, "cache")
	if _, err := validateArchiveCacheDir(cache, []string{share}); err == nil {
		t.Fatal("expected overlapping cache and share to be rejected")
	}
}

func TestServerUsesConfiguredArchiveCacheDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "archives")
	server := NewServer(Config{ArchiveCacheDir: directory})
	if got := server.archiveDirectory(); got != directory {
		t.Fatalf("archive directory = %q, want %q", got, directory)
	}
}

func TestClearArchiveFilesLeavesUnrelatedFiles(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"ready.zip", "working.zip.part", "keep.txt"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := clearArchiveFiles(directory); err != nil {
		t.Fatal(err)
	}
	for _, removed := range []string{"ready.zip", "working.zip.part"} {
		if _, err := os.Stat(filepath.Join(directory, removed)); !os.IsNotExist(err) {
			t.Fatalf("%s was not removed", removed)
		}
	}
	if _, err := os.Stat(filepath.Join(directory, "keep.txt")); err != nil {
		t.Fatal("unrelated file was removed")
	}
}
