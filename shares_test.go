package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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
	allowed := []string{"/api/logout", "/api/files-page", "/api/office-preview", "/api/folder-size"}
	for _, path := range allowed {
		if !readOnlyEndpoint(path) {
			t.Errorf("read-only endpoint rejected: %s", path)
		}
	}
	if readOnlyEndpoint("/api/delete") {
		t.Fatal("read-only user must not be allowed to delete")
	}
}

func TestLogoutRevokesCurrentToken(t *testing.T) {
	s := NewServer(Config{})
	token := "logout-token"
	s.tokens.Store(token, session{Main: true, Expires: time.Now().Add(time.Hour)})
	request := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()

	s.tokenAuth(http.HandlerFunc(s.logout)).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want %d", response.Code, http.StatusOK)
	}
	if _, exists := s.tokens.Load(token); exists {
		t.Fatal("logout token was not revoked")
	}
}

func TestPreviewStreamsVideoRangeWithSessionCookie(t *testing.T) {
	root := t.TempDir()
	content := []byte("0123456789-video-data")
	if err := os.WriteFile(filepath.Join(root, "sample.mp4"), content, 0600); err != nil {
		t.Fatal(err)
	}
	s := NewServer(Config{Folders: []string{root}})
	token := "video-cookie-token"
	s.tokens.Store(token, session{Main: true, Expires: time.Now().Add(time.Hour)})
	request := httptest.NewRequest(http.MethodGet, "/api/preview?path="+url.QueryEscape(shareName(root)+"/sample.mp4"), nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	request.Header.Set("Range", "bytes=2-7")
	response := httptest.NewRecorder()

	s.tokenAuth(http.HandlerFunc(s.preview)).ServeHTTP(response, request)

	if response.Code != http.StatusPartialContent {
		t.Fatalf("preview status = %d, want %d", response.Code, http.StatusPartialContent)
	}
	body, err := io.ReadAll(response.Result().Body)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(body), string(content[2:8]); got != want {
		t.Fatalf("preview range = %q, want %q", got, want)
	}
}

func TestLogoutClearsSessionCookie(t *testing.T) {
	s := NewServer(Config{})
	token := "cookie-logout-token"
	s.tokens.Store(token, session{Main: true, Expires: time.Now().Add(time.Hour)})
	request := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	response := httptest.NewRecorder()

	s.tokenAuth(http.HandlerFunc(s.logout)).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want %d", response.Code, http.StatusOK)
	}
	if _, exists := s.tokens.Load(token); exists {
		t.Fatal("logout cookie token was not revoked")
	}
	cookies := response.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != sessionCookieName || cookies[0].MaxAge >= 0 {
		t.Fatalf("logout did not clear session cookie: %#v", cookies)
	}
}
