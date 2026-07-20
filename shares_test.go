package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestShareNameForWindowsDriveRoot(t *testing.T) {
	if got := shareName(`E:\`); got != "E盘" {
		t.Fatalf("shareName(E drive) = %q, want E盘", got)
	}
}

func TestSharesKeepDriveRootsDistinct(t *testing.T) {
	s := NewServer(Config{Folders: []string{`C:\`, `D:\`, `E:\`, `F:\`}})
	shares := s.shares()
	for _, name := range []string{"C盘", "D盘", "E盘", "F盘"} {
		if shares[name] == "" {
			t.Fatalf("missing share alias %q in %#v", name, shares)
		}
	}
}

func TestSafeResolvesShareAlias(t *testing.T) {
	root := t.TempDir()
	s := NewServer(Config{Folders: []string{root}})
	token := "test-token"
	s.tokens.Store(token, session{Main: true, Expires: time.Now().Add(time.Hour)})
	r := httptest.NewRequest(http.MethodGet, "/api/files-page", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	alias := shareName(root)
	got, err := s.safe(r, filepath.ToSlash(filepath.Join(alias, "child")))
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "child")
	if got != want {
		t.Fatalf("safe path = %q, want %q", got, want)
	}
}

func TestSafeResolvesNestedPathFromDriveRoot(t *testing.T) {
	s := NewServer(Config{Folders: []string{`E:\`}})
	token := "drive-token"
	s.tokens.Store(token, session{Main: true, Expires: time.Now().Add(time.Hour)})
	r := httptest.NewRequest(http.MethodGet, "/api/files-page", nil)
	r.Header.Set("Authorization", "Bearer "+token)

	got, err := s.safe(r, "E盘/一级目录/二级目录")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Clean(`E:\一级目录\二级目录`)
	if got != want {
		t.Fatalf("safe path = %q, want %q", got, want)
	}
}

func TestSafeRejectsTraversalOutsideShare(t *testing.T) {
	root := t.TempDir()
	s := NewServer(Config{Folders: []string{root}})
	token := "traversal-token"
	s.tokens.Store(token, session{Main: true, Expires: time.Now().Add(time.Hour)})
	r := httptest.NewRequest(http.MethodGet, "/api/files-page", nil)
	r.Header.Set("Authorization", "Bearer "+token)

	alias := shareName(root)
	if _, err := s.safe(r, filepath.ToSlash(filepath.Join(alias, "..", "..", "outside"))); err == nil {
		t.Fatal("expected traversal outside the shared root to be rejected")
	}
}

func TestReadOnlyAllowsPagedBrowsingAndPreviews(t *testing.T) {
	allowed := []string{"/api/files-page", "/api/office-preview"}
	for _, path := range allowed {
		if !readOnlyEndpoint(path) {
			t.Errorf("read-only endpoint rejected: %s", path)
		}
	}
	if readOnlyEndpoint("/api/delete") {
		t.Fatal("read-only user must not be allowed to delete")
	}
}
