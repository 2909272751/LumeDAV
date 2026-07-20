package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var officeConvertMu sync.Mutex
var officeExt = map[string]bool{".doc": true, ".docx": true, ".xls": true, ".xlsx": true, ".ppt": true, ".pptx": true, ".odt": true, ".ods": true, ".odp": true}

func findSoffice() string {
	c := []string{filepath.Join(os.Getenv("ProgramFiles"), "LibreOffice", "program", "soffice.exe"), filepath.Join(os.Getenv("ProgramFiles(x86)"), "LibreOffice", "program", "soffice.exe")}
	if p, e := exec.LookPath("soffice"); e == nil {
		c = append([]string{p}, c...)
	}
	for _, p := range c {
		if p != "" {
			if _, e := os.Stat(p); e == nil {
				return p
			}
		}
	}
	return ""
}
func officeStatus() string {
	if findSoffice() != "" {
		return "ready"
	}
	return "missing"
}
func (s *Server) officePreview(w http.ResponseWriter, r *http.Request) {
	p, e := s.safe(r, r.URL.Query().Get("path"))
	if e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	if !officeExt[strings.ToLower(filepath.Ext(p))] {
		http.Error(w, "不支持的 Office 格式", 415)
		return
	}
	soffice := findSoffice()
	if soffice == "" {
		http.Error(w, "主机尚未安装 LibreOffice，请管理员先安装 Office 预览引擎", 503)
		return
	}
	info, e := os.Stat(p)
	if e != nil {
		http.Error(w, e.Error(), 404)
		return
	}
	key := sha256.Sum256([]byte(p + fmt.Sprint(info.Size(), info.ModTime().UnixNano())))
	cache := filepath.Join(lumeDataDir(), "cache", "office")
	_ = os.MkdirAll(cache, 0700)
	out := filepath.Join(cache, hex.EncodeToString(key[:])+".pdf")
	if _, e = os.Stat(out); e != nil {
		officeConvertMu.Lock()
		defer officeConvertMu.Unlock()
		if _, cached := os.Stat(out); cached != nil {
			job := filepath.Join(cache, "job-"+randomID(5))
			_ = os.MkdirAll(job, 0700)
			defer os.RemoveAll(job)
			profileURL := "file:///" + strings.ReplaceAll(filepath.ToSlash(filepath.Join(job, "profile")), " ", "%20")
			if _, e = url.Parse(profileURL); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, soffice, "--headless", "--nologo", "--nodefault", "--nofirststartwizard", "-env:UserInstallation="+profileURL, "--convert-to", "pdf", "--outdir", job, p)
			var stderr strings.Builder
			cmd.Stderr = &stderr
			if e = cmd.Run(); e != nil {
				if ctx.Err() != nil {
					http.Error(w, "Office 转换超过 2 分钟", 504)
				} else {
					http.Error(w, "Office 转换失败: "+stderr.String(), 422)
				}
				return
			}
			generated := filepath.Join(job, strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))+".pdf")
			if _, e = os.Stat(generated); e != nil {
				http.Error(w, "LibreOffice 未生成预览文件", 422)
				return
			}
			if e = os.Rename(generated, out); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Security-Policy", "sandbox")
	http.ServeFile(w, r, out)
}
