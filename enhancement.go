package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type trashMeta struct {
	ID, Name, Original, Root string
	Deleted                  time.Time
	IsDir                    bool
	Size                     int64
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	d := s.dashboard()
	writeJSON(w, d)
}
func (s *Server) dashboard() Dashboard {
	online := 0
	s.tokens.Range(func(k, v any) bool {
		if time.Now().Before(v.(session).Expires) {
			online++
		}
		return true
	})
	blocked := 0
	s.security.Lock()
	for _, f := range s.failures {
		if time.Now().Before(f.BlockedUntil) {
			blocked++
		}
	}
	s.security.Unlock()
	up := int64(0)
	if !s.started.IsZero() {
		up = int64(time.Since(s.started).Seconds())
	}
	return Dashboard{true, up, s.requests.Load(), s.uploaded.Load(), s.downloaded.Load(), online, len(s.shares()), len(s.trashEntries()), blocked}
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	if q == "" {
		writeJSON(w, []fileItem{})
		return
	}
	out := []fileItem{}
	sess := s.rootFor(r)
	roots := map[string]string{"": sess.Root}
	if sess.Main {
		roots = s.shares()
	}
	for alias, root := range roots {
		_ = filepath.Walk(root, func(p string, i os.FileInfo, e error) error {
			if e != nil {
				return nil
			}
			if i.IsDir() && i.Name() == ".lumedav-trash" {
				return filepath.SkipDir
			}
			if p == root {
				return nil
			}
			if strings.Contains(strings.ToLower(i.Name()), q) {
				rel, _ := filepath.Rel(root, p)
				if alias != "" {
					rel = filepath.Join(alias, rel)
				}
				out = append(out, fileItem{i.Name(), filepath.ToSlash(rel), i.Size(), i.IsDir(), i.ModTime()})
				if len(out) >= 500 {
					return errors.New("limit")
				}
			}
			return nil
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	writeJSON(w, out)
}

func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	p, e := s.safe(r, r.URL.Query().Get("path"))
	if e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	i, e := os.Stat(p)
	if e != nil || i.IsDir() {
		http.Error(w, "无法预览", 400)
		return
	}
	ext := strings.ToLower(filepath.Ext(p))
	allowed := map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".svg": true, ".mp4": true, ".m4v": true, ".mov": true, ".webm": true, ".ogv": true, ".mp3": true, ".wav": true, ".ogg": true, ".pdf": true, ".txt": true, ".md": true, ".json": true, ".xml": true, ".yaml": true, ".yml": true, ".log": true, ".go": true, ".js": true, ".ts": true, ".css": true, ".html": true}
	if !allowed[ext] {
		http.Error(w, "此文件类型暂不支持预览", 415)
		return
	}
	http.ServeFile(w, r, p)
}

func (s *Server) moveToTrash(r *http.Request, p string) error {
	sess := s.rootFor(r)
	root := sess.Root
	if sess.Main {
		rel := filepath.ToSlash(strings.Trim(r.URL.Query().Get("path"), "/\\"))
		_ = rel
		for _, v := range s.shares() {
			a, _ := filepath.Abs(v)
			b, _ := filepath.Abs(p)
			if b == a || strings.HasPrefix(strings.ToLower(b), strings.ToLower(a)+string(filepath.Separator)) {
				root = v
				break
			}
		}
	}
	i, e := os.Stat(p)
	if e != nil {
		return e
	}
	dir := filepath.Join(root, ".lumedav-trash")
	if e = os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	id := randomID(8)
	dst := filepath.Join(dir, id+"_"+filepath.Base(p))
	if e = os.Rename(p, dst); e != nil {
		return fmt.Errorf("移入回收站失败: %w", e)
	}
	rel, _ := filepath.Rel(root, p)
	m := trashMeta{id, filepath.Base(p), filepath.ToSlash(rel), root, time.Now(), i.IsDir(), i.Size()}
	b, _ := json.Marshal(m)
	return os.WriteFile(filepath.Join(dir, id+".json"), b, 0600)
}

func (s *Server) trashEntries() []trashMeta {
	out := []trashMeta{}
	for _, root := range s.shares() {
		dir := filepath.Join(root, ".lumedav-trash")
		es, _ := os.ReadDir(dir)
		for _, e := range es {
			if filepath.Ext(e.Name()) != ".json" {
				continue
			}
			b, er := os.ReadFile(filepath.Join(dir, e.Name()))
			if er == nil {
				var m trashMeta
				if json.Unmarshal(b, &m) == nil {
					out = append(out, m)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Deleted.After(out[j].Deleted) })
	return out
}
func (s *Server) trash(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.trashEntries()) }
func (s *Server) restoreTrash(w http.ResponseWriter, r *http.Request) {
	var v struct{ ID string }
	json.NewDecoder(r.Body).Decode(&v)
	if e := s.restoreTrashID(v.ID); e != nil {
		http.Error(w, e.Error(), 404)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
func (s *Server) restoreTrashID(id string) error {
	for _, m := range s.trashEntries() {
		if m.ID != id {
			continue
		}
		dir := filepath.Join(m.Root, ".lumedav-trash")
		matches, _ := filepath.Glob(filepath.Join(dir, m.ID+"_*"))
		if len(matches) == 0 {
			break
		}
		dst := filepath.Join(m.Root, filepath.FromSlash(m.Original))
		if _, e := os.Stat(dst); e == nil {
			dst += " (恢复)"
		}
		if e := os.Rename(matches[0], dst); e != nil {
			return e
		}
		os.Remove(filepath.Join(dir, m.ID+".json"))
		return nil
	}
	return errors.New("回收站项目不存在")
}
func (s *Server) emptyTrash(w http.ResponseWriter, r *http.Request) {
	s.emptyTrashAll()
	writeJSON(w, map[string]bool{"ok": true})
}
func (s *Server) emptyTrashAll() {
	for _, root := range s.shares() {
		dir := filepath.Join(root, ".lumedav-trash")
		es, _ := os.ReadDir(dir)
		for _, e := range es {
			os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}
}

func copyFile(dst, src string) error {
	in, e := os.Open(src)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.Create(dst)
	if e != nil {
		return e
	}
	defer out.Close()
	_, e = io.Copy(out, in)
	return e
}
