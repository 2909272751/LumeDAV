package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const cadToolURL = "https://github.com/LibreDWG/libredwg/releases/download/0.14/libredwg-0.14-win64.zip"
const cadToolSHA256 = "1ad7e15344d20b3426c3435b078d82fb84b35062815946b2cca9c5fc9810fea8"

var cadInstallMu sync.Mutex
var cadConvertMu sync.Mutex

func lumeDataDir() string { d, _ := os.UserConfigDir(); return filepath.Join(d, "LumeDAV") }
func cadToolDir() string  { return filepath.Join(lumeDataDir(), "tools", "libredwg-0.14") }

func ensureCADTool() (string, error) {
	cadInstallMu.Lock()
	defer cadInstallMu.Unlock()
	exe := filepath.Join(cadToolDir(), "dwg2SVG.exe")
	if _, e := os.Stat(exe); e == nil {
		return exe, nil
	}
	if e := os.MkdirAll(cadToolDir(), 0700); e != nil {
		return "", e
	}
	tmp := filepath.Join(lumeDataDir(), "libredwg-0.14-win64.zip.download")
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, e := client.Get(cadToolURL)
	if e != nil {
		return "", fmt.Errorf("下载 CAD 预览引擎失败: %w", e)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("下载 CAD 预览引擎失败: HTTP %d", resp.StatusCode)
	}
	f, e := os.Create(tmp)
	if e != nil {
		return "", e
	}
	h := sha256.New()
	_, e = io.Copy(io.MultiWriter(f, h), resp.Body)
	f.Close()
	if e != nil {
		return "", e
	}
	if hex.EncodeToString(h.Sum(nil)) != cadToolSHA256 {
		os.Remove(tmp)
		return "", fmt.Errorf("CAD 预览引擎校验失败")
	}
	zr, e := zip.OpenReader(tmp)
	if e != nil {
		return "", e
	}
	defer zr.Close()
	need := map[string]bool{"dwg2SVG.exe": true, "libiconv-2.dll": true, "libpcre2-16-0.dll": true, "libpcre2-8-0.dll": true, "libredwg-0.dll": true, "README.txt": true}
	for _, z := range zr.File {
		name := filepath.Base(z.Name)
		if !need[name] {
			continue
		}
		src, e := z.Open()
		if e != nil {
			return "", e
		}
		dst, e := os.OpenFile(filepath.Join(cadToolDir(), name), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0700)
		if e == nil {
			_, e = io.Copy(dst, src)
			dst.Close()
		}
		src.Close()
		if e != nil {
			return "", e
		}
	}
	os.Remove(tmp)
	if _, e = os.Stat(exe); e != nil {
		return "", fmt.Errorf("CAD 转换器安装不完整")
	}
	return exe, nil
}

func (s *Server) cadPreview(w http.ResponseWriter, r *http.Request) {
	p, e := s.safe(r, r.URL.Query().Get("path"))
	if e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	if strings.ToLower(filepath.Ext(p)) != ".dwg" {
		http.Error(w, "仅支持 DWG", 400)
		return
	}
	info, e := os.Stat(p)
	if e != nil {
		http.Error(w, e.Error(), 404)
		return
	}
	key := sha256.Sum256([]byte(p + fmt.Sprint(info.Size(), info.ModTime().UnixNano())))
	dir := filepath.Join(lumeDataDir(), "cache", "cad")
	_ = os.MkdirAll(dir, 0700)
	out := filepath.Join(dir, hex.EncodeToString(key[:])+".svg")
	if _, e = os.Stat(out); e != nil {
		cadConvertMu.Lock()
		defer cadConvertMu.Unlock()
		if _, cached := os.Stat(out); cached == nil {
			w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
			http.ServeFile(w, r, out)
			return
		}
		exe := filepath.Join(cadToolDir(), "dwg2SVG.exe")
		if _, e = os.Stat(exe); e != nil {
			http.Error(w, "CAD 预览引擎尚未安装，请管理员在 LumeDAV EXE 的“CAD 预览”页面下载安装", 503)
			return
		}
		tmp := out + ".tmp"
		f, e := os.Create(tmp)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, exe, "--force-free", p)
		cmd.Dir = filepath.Dir(exe)
		cmd.Stdout = f
		var stderr strings.Builder
		cmd.Stderr = &stderr
		e = cmd.Run()
		f.Close()
		if e != nil {
			os.Remove(tmp)
			if ctx.Err() != nil {
				http.Error(w, "图纸转换超过 90 秒", 504)
			} else {
				http.Error(w, "DWG 转换失败: "+stderr.String(), 422)
			}
			return
		}
		if e = os.Rename(tmp, out); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
	}
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "sandbox")
	http.ServeFile(w, r, out)
}
