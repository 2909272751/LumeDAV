package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/sys/windows/registry"
)

type Config struct {
	Folder       string            `json:"folder"`
	Folders      []string          `json:"folders,omitempty"`
	Port         int               `json:"port"`
	Listen       string            `json:"listen"`
	Username     string            `json:"username"`
	PasswordHash string            `json:"passwordHash"`
	ReadOnly     bool              `json:"readOnly"`
	AutoStart    bool              `json:"autoStart"`
	Temporary    []TemporaryAccess `json:"temporary,omitempty"`
	Users        []UserAccount     `json:"users,omitempty"`
	Invites      []Invite          `json:"invites,omitempty"`
}

type UserAccount struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"passwordHash"`
	ReadOnly     bool   `json:"readOnly"`
	Enabled      bool   `json:"enabled"`
	CreatedAt    int64  `json:"createdAt"`
}
type UserView struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	ReadOnly  bool   `json:"readOnly"`
	Enabled   bool   `json:"enabled"`
	CreatedAt int64  `json:"createdAt"`
}
type Invite struct {
	Code      string `json:"code"`
	ExpiresAt int64  `json:"expiresAt"`
	ReadOnly  bool   `json:"readOnly"`
	Used      bool   `json:"used"`
}

type TemporaryAccess struct {
	ID           string `json:"id"`
	Folder       string `json:"folder"`
	Username     string `json:"username"`
	PasswordHash string `json:"passwordHash"`
	ExpiresAt    int64  `json:"expiresAt"`
	ReadOnly     bool   `json:"readOnly"`
}
type TemporaryView struct {
	ID        string `json:"id"`
	Folder    string `json:"folder"`
	Username  string `json:"username"`
	ExpiresAt int64  `json:"expiresAt"`
	ReadOnly  bool   `json:"readOnly"`
	DAVPath   string `json:"davPath"`
}

type Settings struct {
	Folder      string   `json:"folder"`
	Folders     []string `json:"folders"`
	Port        int      `json:"port"`
	Listen      string   `json:"listen"`
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	PasswordSet bool     `json:"passwordSet"`
	ReadOnly    bool     `json:"readOnly"`
	AutoStart   bool     `json:"autoStart"`
}

type Status struct {
	Running bool     `json:"running"`
	URLs    []string `json:"urls"`
	DAVURL  string   `json:"davUrl"`
	Error   string   `json:"error"`
}
type Dashboard struct {
	Running    bool  `json:"running"`
	Uptime     int64 `json:"uptime"`
	Requests   int64 `json:"requests"`
	Uploaded   int64 `json:"uploaded"`
	Downloaded int64 `json:"downloaded"`
	Online     int   `json:"online"`
	Folders    int   `json:"folders"`
	Trash      int   `json:"trash"`
	Blocked    int   `json:"blocked"`
}

type App struct {
	ctx      context.Context
	mu       sync.Mutex
	cfg      Config
	server   *Server
	quitting bool
}

func NewApp() *App {
	a := &App{}
	a.cfg = Config{Port: 8088, Listen: "127.0.0.1", Username: "admin"}
	_ = a.loadConfig()
	return a
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.startTray()
	if len(os.Args) > 1 && os.Args[1] == "--autostart" && a.configReady() {
		_ = a.startLocked()
	}
}

func (a *App) shutdown(context.Context) { _ = a.stopLocked() }
func (a *App) beforeClose(ctx context.Context) bool {
	a.mu.Lock()
	q := a.quitting
	a.mu.Unlock()
	if q {
		return false
	}
	runtime.WindowHide(ctx)
	return true
}

func (a *App) GetSettings() Settings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settingsLocked()
}

func (a *App) settingsLocked() Settings {
	folders := append([]string(nil), a.cfg.Folders...)
	if len(folders) == 0 && a.cfg.Folder != "" {
		folders = []string{a.cfg.Folder}
	}
	return Settings{Folder: a.cfg.Folder, Folders: folders, Port: a.cfg.Port, Listen: a.cfg.Listen, Username: a.cfg.Username, PasswordSet: a.cfg.PasswordHash != "", ReadOnly: a.cfg.ReadOnly, AutoStart: a.cfg.AutoStart}
}

func (a *App) SelectFolder() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "选择 WebDAV 共享文件夹", DefaultDirectory: a.cfg.Folder})
}

func (a *App) SelectTemporaryFolder() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "选择临时开放的文件夹", DefaultDirectory: a.cfg.Folder})
}
func (a *App) ListTemporaryAccess() []TemporaryView {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.purgeExpired()
	out := make([]TemporaryView, 0, len(a.cfg.Temporary))
	for _, t := range a.cfg.Temporary {
		out = append(out, TemporaryView{t.ID, t.Folder, t.Username, t.ExpiresAt, t.ReadOnly, "/share/" + t.ID + "/"})
	}
	return out
}
func (a *App) CreateTemporaryAccess(folder, username, password string, hours int, readOnly bool) (TemporaryView, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if hours < 1 || hours > 720 {
		return TemporaryView{}, errors.New("有效期须为 1–720 小时")
	}
	i, e := os.Stat(folder)
	if e != nil || !i.IsDir() {
		return TemporaryView{}, errors.New("请选择有效目录")
	}
	if username == "" || password == "" {
		return TemporaryView{}, errors.New("账号和密码不能为空")
	}
	h, e := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if e != nil {
		return TemporaryView{}, e
	}
	id := randomID(6)
	t := TemporaryAccess{id, folder, username, string(h), time.Now().Add(time.Duration(hours) * time.Hour).Unix(), readOnly}
	a.cfg.Temporary = append(a.cfg.Temporary, t)
	e = a.saveConfig()
	if a.server != nil {
		a.server.UpdateConfig(a.cfg)
	}
	return TemporaryView{id, folder, username, t.ExpiresAt, readOnly, "/share/" + id + "/"}, e
}
func (a *App) RevokeTemporaryAccess(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := a.cfg.Temporary[:0]
	for _, t := range a.cfg.Temporary {
		if t.ID != id {
			out = append(out, t)
		}
	}
	a.cfg.Temporary = out
	if a.server != nil {
		a.server.UpdateConfig(a.cfg)
	}
	return a.saveConfig()
}
func (a *App) purgeExpired() {
	now := time.Now().Unix()
	out := a.cfg.Temporary[:0]
	for _, t := range a.cfg.Temporary {
		if t.ExpiresAt > now {
			out = append(out, t)
		}
	}
	a.cfg.Temporary = out
}

func (a *App) SaveSettings(s Settings) (Settings, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if s.Port < 1 || s.Port > 65535 {
		return Settings{}, errors.New("端口必须在 1–65535 之间")
	}
	if len(s.Folders) == 0 && s.Folder != "" {
		s.Folders = []string{s.Folder}
	}
	if len(s.Folders) == 0 {
		return Settings{}, errors.New("请至少添加一个共享文件夹")
	}
	clean := make([]string, 0, len(s.Folders))
	seen := map[string]bool{}
	for _, f := range s.Folders {
		f = filepath.Clean(f)
		info, err := os.Stat(f)
		if err != nil || !info.IsDir() {
			return Settings{}, fmt.Errorf("无效共享文件夹: %s", f)
		}
		k := strings.ToLower(f)
		if !seen[k] {
			seen[k] = true
			clean = append(clean, f)
		}
	}
	if s.Listen != "127.0.0.1" && s.Listen != "0.0.0.0" {
		return Settings{}, errors.New("监听地址无效")
	}
	if s.Username == "" {
		return Settings{}, errors.New("用户名不能为空")
	}
	if s.Password != "" {
		h, err := bcrypt.GenerateFromPassword([]byte(s.Password), bcrypt.DefaultCost)
		if err != nil {
			return Settings{}, err
		}
		a.cfg.PasswordHash = string(h)
	} else if a.cfg.PasswordHash == "" {
		return Settings{}, errors.New("请设置访问密码")
	}
	a.cfg.Folders = clean
	a.cfg.Folder = clean[0]
	a.cfg.Port, a.cfg.Listen, a.cfg.Username, a.cfg.ReadOnly = s.Port, s.Listen, s.Username, s.ReadOnly
	if s.AutoStart != a.cfg.AutoStart {
		if err := setAutoStart(s.AutoStart); err != nil {
			return Settings{}, fmt.Errorf("设置自启动失败: %w", err)
		}
		a.cfg.AutoStart = s.AutoStart
	}
	if err := a.saveConfig(); err != nil {
		return Settings{}, err
	}
	return a.settingsLocked(), nil
}

func (a *App) Start() (Status, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.startLocked(); err != nil {
		return a.statusLocked(err.Error()), err
	}
	return a.statusLocked(""), nil
}

func (a *App) Stop() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	_ = a.stopLocked()
	return a.statusLocked("")
}
func (a *App) GetStatus() Status { a.mu.Lock(); defer a.mu.Unlock(); return a.statusLocked("") }
func (a *App) CheckPort(port int, listen string) string {
	if port < 1 || port > 65535 {
		return "端口必须在 1–65535 之间"
	}
	ln, e := net.Listen("tcp", net.JoinHostPort(listen, strconv.Itoa(port)))
	if e != nil {
		return "端口已被占用或无权使用: " + e.Error()
	}
	ln.Close()
	return "端口可用"
}
func (a *App) GetDashboard() Dashboard {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.server == nil {
		return Dashboard{Folders: len(a.settingsLocked().Folders), Trash: len(NewServer(a.cfg).trashEntries())}
	}
	return a.server.dashboard()
}
func (a *App) ListTrash() []trashMeta {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.server != nil {
		return a.server.trashEntries()
	}
	return NewServer(a.cfg).trashEntries()
}
func (a *App) RestoreTrash(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.server
	if s == nil {
		s = NewServer(a.cfg)
	}
	return s.restoreTrashID(id)
}
func (a *App) EmptyTrash() {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.server
	if s == nil {
		s = NewServer(a.cfg)
	}
	s.emptyTrashAll()
}
func (a *App) ListUsers() []UserView {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := []UserView{}
	for _, u := range a.cfg.Users {
		out = append(out, UserView{u.ID, u.Username, u.ReadOnly, u.Enabled, u.CreatedAt})
	}
	return out
}
func (a *App) CreateUser(username, password string, readOnly bool) (UserView, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if username == "" || password == "" {
		return UserView{}, errors.New("账号和密码不能为空")
	}
	if strings.EqualFold(username, a.cfg.Username) {
		return UserView{}, errors.New("账号已存在")
	}
	for _, u := range a.cfg.Users {
		if strings.EqualFold(u.Username, username) {
			return UserView{}, errors.New("账号已存在")
		}
	}
	h, e := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if e != nil {
		return UserView{}, e
	}
	u := UserAccount{randomID(6), username, string(h), readOnly, true, time.Now().Unix()}
	a.cfg.Users = append(a.cfg.Users, u)
	e = a.saveConfig()
	if a.server != nil {
		a.server.UpdateConfig(a.cfg)
	}
	return UserView{u.ID, u.Username, u.ReadOnly, u.Enabled, u.CreatedAt}, e
}
func (a *App) DeleteUser(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := a.cfg.Users[:0]
	for _, u := range a.cfg.Users {
		if u.ID != id {
			out = append(out, u)
		}
	}
	a.cfg.Users = out
	if a.server != nil {
		a.server.UpdateConfig(a.cfg)
	}
	return a.saveConfig()
}
func (a *App) SetUserEnabled(id string, enabled bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.cfg.Users {
		if a.cfg.Users[i].ID == id {
			a.cfg.Users[i].Enabled = enabled
		}
	}
	if a.server != nil {
		a.server.UpdateConfig(a.cfg)
	}
	return a.saveConfig()
}
func (a *App) ListInvites() []Invite {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Invite, 0, len(a.cfg.Invites))
	return append(out, a.cfg.Invites...)
}
func (a *App) CreateInvite(hours int, readOnly bool) (Invite, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if hours < 1 || hours > 720 {
		return Invite{}, errors.New("有效期须为 1–720 小时")
	}
	v := Invite{randomID(12), time.Now().Add(time.Duration(hours) * time.Hour).Unix(), readOnly, false}
	a.cfg.Invites = append(a.cfg.Invites, v)
	e := a.saveConfig()
	if a.server != nil {
		a.server.UpdateConfig(a.cfg)
	}
	return v, e
}
func (a *App) RevokeInvite(code string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := a.cfg.Invites[:0]
	for _, v := range a.cfg.Invites {
		if v.Code != code {
			out = append(out, v)
		}
	}
	a.cfg.Invites = out
	if a.server != nil {
		a.server.UpdateConfig(a.cfg)
	}
	return a.saveConfig()
}
func (a *App) CADPreviewStatus() string {
	if _, e := os.Stat(filepath.Join(cadToolDir(), "dwg2SVG.exe")); e == nil {
		return "ready"
	}
	return "missing"
}
func (a *App) InstallCADPreviewEngine() string {
	if _, e := ensureCADTool(); e != nil {
		return e.Error()
	}
	return "ready"
}
func (a *App) ClearCADPreviewCache() error {
	p := filepath.Join(lumeDataDir(), "cache", "cad")
	if e := os.RemoveAll(p); e != nil {
		return e
	}
	return os.MkdirAll(p, 0700)
}
func (a *App) OfficePreviewStatus() string { return officeStatus() }
func (a *App) OpenLibreOfficeDownload() {
	runtime.BrowserOpenURL(a.ctx, "https://www.libreoffice.org/download/download-libreoffice/")
}
func (a *App) ClearOfficePreviewCache() error {
	p := filepath.Join(lumeDataDir(), "cache", "office")
	if e := os.RemoveAll(p); e != nil {
		return e
	}
	return os.MkdirAll(p, 0700)
}

func (a *App) startLocked() error {
	if a.server != nil {
		return nil
	}
	if !a.configReady() {
		return errors.New("请先完成文件夹、账号和密码设置")
	}
	s := NewServer(a.cfg)
	if err := s.Start(); err != nil {
		return err
	}
	a.server = s
	return nil
}

func (a *App) stopLocked() error {
	if a.server == nil {
		return nil
	}
	err := a.server.Stop()
	a.server = nil
	return err
}

func (a *App) statusLocked(msg string) Status {
	st := Status{Running: a.server != nil, Error: msg}
	if !st.Running {
		return st
	}
	hosts := []string{"127.0.0.1"}
	if a.cfg.Listen == "0.0.0.0" {
		hosts = localIPs()
	}
	for _, h := range hosts {
		st.URLs = append(st.URLs, "http://"+net.JoinHostPort(h, strconv.Itoa(a.cfg.Port)))
	}
	if len(st.URLs) > 0 {
		st.DAVURL = st.URLs[0] + "/dav/"
	}
	return st
}

func (a *App) configReady() bool {
	return (len(a.cfg.Folders) > 0 || a.cfg.Folder != "") && a.cfg.PasswordHash != ""
}

func configPath() string {
	d, _ := os.UserConfigDir()
	return filepath.Join(d, "LumeDAV", "config.json")
}
func (a *App) loadConfig() error {
	b, err := os.ReadFile(configPath())
	if err != nil {
		return err
	}
	return json.Unmarshal(b, &a.cfg)
}
func (a *App) saveConfig() error {
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(a.cfg, "", "  ")
	return os.WriteFile(p, b, 0600)
}

func setAutoStart(enable bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !enable {
		err := k.DeleteValue("LumeDAV")
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue("LumeDAV", `"`+exe+`" --autostart`)
}

func localIPs() []string {
	var out []string
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ip, _, e := net.ParseCIDR(a.String()); e == nil && ip.To4() != nil && !ip.IsLoopback() {
			out = append(out, ip.String())
		}
	}
	if len(out) == 0 {
		out = []string{"127.0.0.1"}
	}
	return out
}

func randomID(n int) string { b := make([]byte, n); _, _ = rand.Read(b); return hex.EncodeToString(b) }
