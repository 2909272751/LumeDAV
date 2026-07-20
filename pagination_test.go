package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFilesPageLimitsLargeDirectory(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 450; i++ {
		if e := os.WriteFile(filepath.Join(root, fmt.Sprintf("file-%04d.txt", i)), []byte("x"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	s := NewServer(Config{})
	s.tokens.Store("test", session{Root: root, Expires: time.Now().Add(time.Hour)})
	req := httptest.NewRequest("GET", "/api/files-page?limit=200&offset=0", nil)
	req.Header.Set("Authorization", "Bearer test")
	w := httptest.NewRecorder()
	s.filesPage(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var p filePage
	if e := json.Unmarshal(w.Body.Bytes(), &p); e != nil {
		t.Fatal(e)
	}
	if len(p.Items) != 200 || !p.HasMore {
		t.Fatalf("items=%d hasMore=%v", len(p.Items), p.HasMore)
	}
}
