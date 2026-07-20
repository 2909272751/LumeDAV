package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type filePage struct {
	Items   []fileItem `json:"items"`
	HasMore bool       `json:"hasMore"`
	Offset  int        `json:"offset"`
	Limit   int        `json:"limit"`
}

func (s *Server) filesPage(w http.ResponseWriter, r *http.Request) {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if offset < 0 {
		offset = 0
	}
	if limit < 20 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	relPath := r.URL.Query().Get("path")
	if s.rootFor(r).Main && strings.Trim(relPath, "/\\") == "" {
		items := []fileItem{}
		for name := range s.shares() {
			items = append(items, fileItem{Name: name, Path: name, IsDir: true, Modified: time.Now()})
		}
		sort.Slice(items, func(i, j int) bool { return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name) })
		writeJSON(w, filePage{items, false, 0, limit})
		return
	}
	p, e := s.safe(r, relPath)
	if e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	f, e := os.Open(p)
	if e != nil {
		http.Error(w, e.Error(), 404)
		return
	}
	defer f.Close()
	items := make([]fileItem, 0, limit+1)
	seen := 0
	for len(items) <= limit {
		batch, e := f.ReadDir(256)
		for _, x := range batch {
			if x.Name() == ".lumedav-trash" {
				continue
			}
			if seen < offset {
				seen++
				continue
			}
			i, er := x.Info()
			if er != nil {
				continue
			}
			root := s.rootFor(r).Root
			if s.rootFor(r).Main {
				first := strings.SplitN(filepath.ToSlash(relPath), "/", 2)[0]
				root = s.shares()[first]
			}
			rel, _ := filepath.Rel(root, filepath.Join(p, x.Name()))
			if s.rootFor(r).Main {
				first := strings.SplitN(filepath.ToSlash(relPath), "/", 2)[0]
				rel = filepath.Join(first, rel)
			}
			items = append(items, fileItem{x.Name(), filepath.ToSlash(rel), i.Size(), x.IsDir(), i.ModTime()})
			seen++
			if len(items) > limit {
				break
			}
		}
		if e == io.EOF || len(batch) == 0 || len(items) > limit {
			break
		}
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
	}
	more := len(items) > limit
	if more {
		items = items[:limit]
	}
	sortMode := r.URL.Query().Get("sort")
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].IsDir != items[j].IsDir {
			return items[i].IsDir
		}
		switch sortMode {
		case "time":
			return items[i].Modified.After(items[j].Modified)
		case "size":
			return items[i].Size > items[j].Size
		default:
			return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
		}
	})
	writeJSON(w, filePage{items, more, offset, limit})
}
