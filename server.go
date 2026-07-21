package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/net/webdav"
)

//go:embed all:webui
var webAssets embed.FS

type Server struct {
	cfgMu           sync.RWMutex
	cfg             Config
	http            *http.Server
	tokens          sync.Map
	security        sync.Mutex
	failures        map[string]*loginFailure
	started         time.Time
	requests        atomic.Int64
	uploaded        atomic.Int64
	downloaded      atomic.Int64
	archiveMu       sync.RWMutex
	archiveTasks    map[string]*folderArchiveTask
	archiveQueue    chan *folderArchiveTask
	archiveStop     chan struct{}
	archiveStopOnce sync.Once
}
type loginFailure struct {
	Count        int
	First        time.Time
	BlockedUntil time.Time
}
type session struct {
	Root     string
	ReadOnly bool
	Main     bool
	Expires  time.Time
}
type fileItem struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	IsDir    bool      `json:"isDir"`
	Modified time.Time `json:"modified"`
}

func NewServer(cfg Config) *Server {
	return &Server{
		cfg:          cfg,
		archiveTasks: map[string]*folderArchiveTask{},
		archiveQueue: make(chan *folderArchiveTask, archiveTaskLimit),
		archiveStop:  make(chan struct{}),
	}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.Handle("/dav/", s.basicAuth(http.HandlerFunc(s.mainDAV)))
	mux.Handle("/share/", http.HandlerFunc(s.temporaryDAV))
	mux.HandleFunc("/api/login", s.login)
	mux.HandleFunc("/api/register", s.register)
	mux.Handle("/api/logout", s.tokenAuth(http.HandlerFunc(s.logout)))
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"version": appVersion})
	})
	mux.Handle("/api/files", s.tokenAuth(http.HandlerFunc(s.files)))
	mux.Handle("/api/files-page", s.tokenAuth(http.HandlerFunc(s.filesPage)))
	mux.Handle("/api/upload", s.tokenAuth(http.HandlerFunc(s.upload)))
	mux.Handle("/api/mkdir", s.tokenAuth(http.HandlerFunc(s.mkdir)))
	mux.Handle("/api/delete", s.tokenAuth(http.HandlerFunc(s.delete)))
	mux.Handle("/api/rename", s.tokenAuth(http.HandlerFunc(s.rename)))
	mux.Handle("/api/download", s.tokenAuth(http.HandlerFunc(s.download)))
	mux.Handle("/api/folder-download/start", s.tokenAuth(http.HandlerFunc(s.folderArchiveStart)))
	mux.Handle("/api/folder-download/status", s.tokenAuth(http.HandlerFunc(s.folderArchiveStatus)))
	mux.Handle("/api/folder-download/cancel", s.tokenAuth(http.HandlerFunc(s.folderArchiveCancel)))
	mux.HandleFunc("/api/folder-download/content", s.folderArchiveContent)
	mux.Handle("/api/folder-size", s.tokenAuth(http.HandlerFunc(s.folderSize)))
	mux.Handle("/api/stats", s.tokenAuth(http.HandlerFunc(s.stats)))
	mux.Handle("/api/search", s.tokenAuth(http.HandlerFunc(s.search)))
	mux.Handle("/api/preview", s.tokenAuth(http.HandlerFunc(s.preview)))
	mux.Handle("/api/office-preview", s.tokenAuth(http.HandlerFunc(s.officePreview)))
	mux.Handle("/api/trash", s.tokenAuth(http.HandlerFunc(s.trash)))
	mux.Handle("/api/trash/restore", s.tokenAuth(http.HandlerFunc(s.restoreTrash)))
	mux.Handle("/api/trash/empty", s.tokenAuth(http.HandlerFunc(s.emptyTrash)))
	root, _ := fs.Sub(webAssets, "webui")
	mux.Handle("/", http.FileServer(http.FS(root)))
	s.failures = map[string]*loginFailure{}
	s.started = time.Now()
	s.http = &http.Server{Addr: s.cfg.Listen + ":" + strconv.Itoa(s.cfg.Port), Handler: securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.requests.Add(1); mux.ServeHTTP(w, r) })), ReadHeaderTimeout: 10 * time.Second}
	ln, err := net.Listen("tcp", s.http.Addr)
	if err != nil {
		return err
	}
	go s.archiveWorker()
	go s.archiveCleanupLoop()
	go s.http.Serve(ln)
	return nil
}

func (s *Server) Stop() error {
	s.stopArchiveManager()
	if s.http == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return s.http.Shutdown(ctx)
}
func (s *Server) UpdateConfig(c Config) { s.cfgMu.Lock(); s.cfg = c; s.cfgMu.Unlock() }

func (s *Server) valid(user, pass string) bool {
	_, ok := s.authenticate(user, pass)
	return ok
}
func (s *Server) authenticate(user, pass string) (bool, bool) {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	if user == s.cfg.Username && bcrypt.CompareHashAndPassword([]byte(s.cfg.PasswordHash), []byte(pass)) == nil {
		return s.cfg.ReadOnly, true
	}
	for _, u := range s.cfg.Users {
		if u.Enabled && strings.EqualFold(u.Username, user) && bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(pass)) == nil {
			return u.ReadOnly, true
		}
	}
	return false, false
}

func (s *Server) findTemporary(user, pass string) (TemporaryAccess, bool) {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	now := time.Now().Unix()
	for _, t := range s.cfg.Temporary {
		if t.ExpiresAt > now && t.Username == user && bcrypt.CompareHashAndPassword([]byte(t.PasswordHash), []byte(pass)) == nil {
			return t, true
		}
	}
	return TemporaryAccess{}, false
}
func (s *Server) basicAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		ro, auth := s.authenticate(u, p)
		if !ok || !auth {
			w.Header().Set("WWW-Authenticate", `Basic realm="LumeDAV"`)
			http.Error(w, "Unauthorized", 401)
			return
		}
		if ro && r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" && r.Method != "PROPFIND" {
			http.Error(w, "Read only", 403)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) tokenAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t := sessionToken(r)
		v, ok := s.tokens.Load(t)
		if !ok || t == "" || time.Now().After(v.(session).Expires) {
			s.tokens.Delete(t)
			http.Error(w, "Unauthorized", 401)
			return
		}
		if v.(session).ReadOnly && !readOnlyEndpoint(r.URL.Path) {
			http.Error(w, "只读模式", 403)
			return
		}
		if cookie, err := r.Cookie(sessionCookieName); err != nil || cookie.Value != t {
			setSessionCookie(w, r, t, v.(session).Expires)
		}
		next.ServeHTTP(w, r)
	})
}

const sessionCookieName = "lumedav_session"

func sessionToken(r *http.Request) string {
	if token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")); token != "" {
		return token
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   max(1, int(time.Until(expires).Seconds())),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
	})
}

func readOnlyEndpoint(path string) bool {
	switch path {
	case "/api/logout", "/api/files", "/api/files-page", "/api/download", "/api/folder-download/start", "/api/folder-download/status", "/api/folder-download/cancel", "/api/folder-size", "/api/stats", "/api/search", "/api/preview", "/api/office-preview":
		return true
	default:
		return false
	}
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method", http.StatusMethodNotAllowed)
		return
	}
	s.tokens.Delete(sessionToken(r))
	clearSessionCookie(w, r)
	writeJSON(w, map[string]bool{"ok": true})
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method", 405)
		return
	}
	var v struct{ Username, Password string }
	_ = json.NewDecoder(r.Body).Decode(&v)
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip == "" {
		ip = r.RemoteAddr
	}
	s.security.Lock()
	f := s.failures[ip]
	if f != nil && time.Now().Before(f.BlockedUntil) {
		wait := time.Until(f.BlockedUntil).Round(time.Second)
		s.security.Unlock()
		http.Error(w, "尝试次数过多，请在 "+wait.String()+" 后重试", 429)
		return
	}
	s.security.Unlock()
	ro, authenticated := s.authenticate(v.Username, v.Password)
	sess := session{Root: s.cfg.Folder, ReadOnly: ro, Main: true, Expires: time.Now().Add(12 * time.Hour)}
	if !authenticated {
		if t, ok := s.findTemporary(v.Username, v.Password); ok {
			sess = session{Root: t.Folder, ReadOnly: t.ReadOnly, Expires: time.Now().Add(12 * time.Hour)}
		} else {
			s.security.Lock()
			f = s.failures[ip]
			if f == nil || time.Since(f.First) > 15*time.Minute {
				f = &loginFailure{First: time.Now()}
				s.failures[ip] = f
			}
			f.Count++
			if f.Count >= 5 {
				f.BlockedUntil = time.Now().Add(15 * time.Minute)
			}
			s.security.Unlock()
			http.Error(w, "用户名或密码错误", 401)
			return
		}
	}
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	t := hex.EncodeToString(b)
	s.tokens.Store(t, sess)
	setSessionCookie(w, r, t, sess.Expires)
	s.security.Lock()
	delete(s.failures, ip)
	s.security.Unlock()
	writeJSON(w, map[string]any{"token": t, "readOnly": sess.ReadOnly})
}
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method", 405)
		return
	}
	var v struct{ Code, Username, Password string }
	json.NewDecoder(r.Body).Decode(&v)
	if v.Username == "" || v.Password == "" {
		http.Error(w, "账号和密码不能为空", 400)
		return
	}
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	idx := -1
	for i, x := range s.cfg.Invites {
		if x.Code == v.Code && !x.Used && x.ExpiresAt > time.Now().Unix() {
			idx = i
			break
		}
	}
	if idx < 0 {
		http.Error(w, "邀请码无效或已过期", 400)
		return
	}
	if strings.EqualFold(v.Username, s.cfg.Username) {
		http.Error(w, "账号已存在", 409)
		return
	}
	for _, u := range s.cfg.Users {
		if strings.EqualFold(u.Username, v.Username) {
			http.Error(w, "账号已存在", 409)
			return
		}
	}
	h, e := bcrypt.GenerateFromPassword([]byte(v.Password), bcrypt.DefaultCost)
	if e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	inv := &s.cfg.Invites[idx]
	s.cfg.Users = append(s.cfg.Users, UserAccount{randomID(6), v.Username, string(h), inv.ReadOnly, true, time.Now().Unix()})
	inv.Used = true
	b, _ := json.MarshalIndent(s.cfg, "", "  ")
	if e = os.MkdirAll(filepath.Dir(configPath()), 0700); e == nil {
		e = os.WriteFile(configPath(), b, 0600)
	}
	if e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) rootFor(r *http.Request) session {
	t := sessionToken(r)
	if v, ok := s.tokens.Load(t); ok {
		return v.(session)
	}
	return session{Root: s.cfg.Folder, ReadOnly: s.cfg.ReadOnly, Main: true}
}

func (s *Server) shares() map[string]string {
	s.cfgMu.RLock()
	defer s.cfgMu.RUnlock()
	folders := s.cfg.Folders
	if len(folders) == 0 && s.cfg.Folder != "" {
		folders = []string{s.cfg.Folder}
	}
	out := map[string]string{}
	for _, f := range folders {
		name := shareName(f)
		base := name
		for i := 2; out[name] != ""; i++ {
			name = base + " (" + strconv.Itoa(i) + ")"
		}
		out[name] = f
	}
	return out
}

func shareName(folder string) string {
	clean := filepath.Clean(folder)
	volume := filepath.VolumeName(clean)
	if volume != "" && strings.Trim(clean[len(volume):], `/\`) == "" {
		if len(volume) == 2 && volume[1] == ':' {
			return strings.ToUpper(volume[:1]) + "盘"
		}
		if base := filepath.Base(volume); base != "." && strings.Trim(base, `/\`) != "" {
			return base
		}
	}
	name := filepath.Base(clean)
	if name == "." || strings.Trim(name, `/\`) == "" {
		return "共享文件"
	}
	return name
}
func (s *Server) safe(r *http.Request, rel string) (string, error) {
	sess := s.rootFor(r)
	rel = strings.TrimPrefix(filepath.Clean("/"+rel), string(filepath.Separator))
	if sess.Main {
		parts := strings.SplitN(filepath.ToSlash(rel), "/", 2)
		root, ok := s.shares()[parts[0]]
		if !ok {
			return "", errors.New("请选择一个共享目录")
		}
		sess.Root = root
		if len(parts) == 2 {
			rel = parts[1]
		} else {
			rel = ""
		}
	}
	p := filepath.Join(sess.Root, rel)
	root, err := filepath.Abs(sess.Root)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	within, err := filepath.Rel(root, abs)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) || filepath.IsAbs(within) {
		return "", errors.New("invalid path")
	}
	return abs, nil
}
func (s *Server) files(w http.ResponseWriter, r *http.Request) {
	if s.rootFor(r).Main && strings.Trim(r.URL.Query().Get("path"), "/\\") == "" {
		out := []fileItem{}
		for name := range s.shares() {
			out = append(out, fileItem{Name: name, Path: name, IsDir: true, Modified: time.Now()})
		}
		writeJSON(w, out)
		return
	}
	p, e := s.safe(r, r.URL.Query().Get("path"))
	if e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	es, e := os.ReadDir(p)
	if e != nil {
		http.Error(w, e.Error(), 404)
		return
	}
	out := []fileItem{}
	for _, x := range es {
		if x.Name() == ".lumedav-trash" {
			continue
		}
		i, _ := x.Info()
		root := s.rootFor(r).Root
		if s.rootFor(r).Main {
			first := strings.SplitN(filepath.ToSlash(r.URL.Query().Get("path")), "/", 2)[0]
			root = s.shares()[first]
		}
		rel, _ := filepath.Rel(root, filepath.Join(p, x.Name()))
		if s.rootFor(r).Main {
			first := strings.SplitN(filepath.ToSlash(r.URL.Query().Get("path")), "/", 2)[0]
			rel = filepath.Join(first, rel)
		}
		out = append(out, fileItem{x.Name(), filepath.ToSlash(rel), i.Size(), x.IsDir(), i.ModTime()})
	}
	writeJSON(w, out)
}
func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	if s.denyWrite(w) {
		return
	}
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	dir, e := s.safe(r, r.FormValue("path"))
	if e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	files := r.MultipartForm.File["files"]
	for _, h := range files {
		src, e := h.Open()
		if e != nil {
			continue
		}
		dst, e := os.OpenFile(filepath.Join(dir, filepath.Base(h.Filename)), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if e == nil {
			var n int64
			n, e = io.Copy(dst, src)
			s.uploaded.Add(n)
			dst.Close()
		}
		src.Close()
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
	}
	writeJSON(w, map[string]bool{"ok": true})
}
func (s *Server) mkdir(w http.ResponseWriter, r *http.Request) {
	if s.denyWrite(w) {
		return
	}
	var v struct{ Path, Name string }
	json.NewDecoder(r.Body).Decode(&v)
	p, e := s.safe(r, filepath.Join(v.Path, filepath.Base(v.Name)))
	if e == nil {
		e = os.Mkdir(p, 0755)
	}
	done(w, e)
}
func (s *Server) delete(w http.ResponseWriter, r *http.Request) {
	if s.denyWrite(w) {
		return
	}
	var v struct{ Path string }
	json.NewDecoder(r.Body).Decode(&v)
	if s.rootFor(r).Main && !strings.Contains(strings.Trim(v.Path, "/\\"), "/") && !strings.Contains(strings.Trim(v.Path, "/\\"), "\\") {
		http.Error(w, "不能删除共享目录入口", 400)
		return
	}
	p, e := s.safe(r, v.Path)
	if e == nil && p == s.rootFor(r).Root {
		e = errors.New("cannot delete root")
	}
	if e == nil {
		e = s.moveToTrash(r, p)
	}
	done(w, e)
}
func (s *Server) rename(w http.ResponseWriter, r *http.Request) {
	if s.denyWrite(w) {
		return
	}
	var v struct{ Path, Name string }
	json.NewDecoder(r.Body).Decode(&v)
	p, e := s.safe(r, v.Path)
	if e == nil {
		e = os.Rename(p, filepath.Join(filepath.Dir(p), filepath.Base(v.Name)))
	}
	done(w, e)
}
func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	p, e := s.safe(r, r.URL.Query().Get("path"))
	if e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	if i, e := os.Stat(p); e == nil {
		s.downloaded.Add(i.Size())
	}
	http.ServeFile(w, r, p)
}
func (s *Server) denyWrite(w http.ResponseWriter) bool {
	return false
}

func (s *Server) temporaryDAV(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(strings.ToLower(r.URL.Path), "/.lumedav-trash") {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/share/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	u, p, ok := r.BasicAuth()
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="LumeDAV Temporary"`)
		http.Error(w, "Unauthorized", 401)
		return
	}
	s.cfgMu.RLock()
	var found *TemporaryAccess
	for i := range s.cfg.Temporary {
		t := &s.cfg.Temporary[i]
		if t.ID == id && t.ExpiresAt > time.Now().Unix() && t.Username == u && bcrypt.CompareHashAndPassword([]byte(t.PasswordHash), []byte(p)) == nil {
			copy := *t
			found = &copy
			break
		}
	}
	s.cfgMu.RUnlock()
	if found == nil {
		http.Error(w, "临时访问不存在或已过期", 401)
		return
	}
	if found.ReadOnly && r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" && r.Method != "PROPFIND" {
		http.Error(w, "Read only", 403)
		return
	}
	prefix := "/share/" + id
	h := &webdav.Handler{Prefix: prefix, FileSystem: webdav.Dir(found.Folder), LockSystem: webdav.NewMemLS()}
	h.ServeHTTP(w, r)
}

func (s *Server) mainDAV(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/dav/")
	if strings.Contains(strings.ToLower("/"+rel), "/.lumedav-trash") {
		http.NotFound(w, r)
		return
	}
	parts := strings.SplitN(rel, "/", 2)
	if parts[0] == "" {
		if r.Method == "PROPFIND" {
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			w.WriteHeader(207)
			io.WriteString(w, `<?xml version="1.0"?><D:multistatus xmlns:D="DAV:">`)
			for name := range s.shares() {
				fmt.Fprintf(w, `<D:response><D:href>/dav/%s/</D:href><D:propstat><D:prop><D:displayname>%s</D:displayname><D:resourcetype><D:collection/></D:resourcetype></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>`, url.PathEscape(name), html.EscapeString(name))
			}
			io.WriteString(w, "</D:multistatus>")
			return
		}
		http.Error(w, "请打开一个共享目录", 400)
		return
	}
	root, ok := s.shares()[parts[0]]
	if !ok {
		http.NotFound(w, r)
		return
	}
	prefix := "/dav/" + parts[0]
	h := &webdav.Handler{Prefix: prefix, FileSystem: webdav.Dir(root), LockSystem: webdav.NewMemLS()}
	h.ServeHTTP(w, r)
}
func done(w http.ResponseWriter, e error) {
	if e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
