package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	sessionIdle    = 15 * time.Minute // 会话记录状态多久没用就丢掉
	bigReadRelease = 16 << 20         // 一次读了这么多字节后把内存还给系统
)

// Engine 持有所有缓存。常驻进程里只有一个，被所有会话共用。
type Engine struct {
	mu          sync.Mutex
	transcripts map[string]*transcriptState
	git         *gitCache
	cfg         *configCache
	ver         versionCache
}

func newEngine() *Engine {
	return &Engine{transcripts: map[string]*transcriptState{}, git: newGitCache(), cfg: newConfigCache()}
}

// Render 处理一次状态栏请求，返回要输出的文本。
func (e *Engine) Render(req request, now time.Time) string {
	var in StdinData
	if err := json.Unmarshal(req.Input, &in); err != nil {
		return "[cchud] 无法解析状态栏输入\n"
	}
	cfg := e.cfg.load(configPath(req.Env), now)

	var tr TranscriptData
	if p := strings.TrimSpace(in.TranscriptPath); p != "" {
		st := e.transcript(p, now)
		st.mu.Lock()
		if st.refresh() > bigReadRelease {
			defer freeMemorySoon()
		}
		if in.ContextWindow != nil {
			st.applyContextFallback(in.ContextWindow, now)
		}
		tr = st.data
		st.mu.Unlock()
	}

	var git *GitStatus
	if cfg.Git.Enabled {
		git = e.git.get(in.Cwd, now)
	}

	version := strings.TrimSpace(in.Version)
	if version == "" && cfg.Display.ShowClaudeCodeVersion {
		version = e.ver.get()
	}

	width := req.Cols
	if cfg.ForceMaxWidth && cfg.MaxWidth > 0 || width == 0 && cfg.MaxWidth > 0 {
		width = cfg.MaxWidth
	}

	f := &frame{in: &in, tr: tr, cfg: cfg, git: git, version: version, width: width, now: now,
		env: req.Env, t: newTranslator(cfg.Language), p: palette{cfg.Colors}}
	return render(f)
}

func (e *Engine) transcript(path string, now time.Time) *transcriptState {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.transcripts[path]
	if st == nil {
		st = newTranscriptState(path)
		e.transcripts[path] = st
	}
	st.lastUsed = now
	return st
}

// Evict 清掉长时间没用的会话和 git 缓存，有清理时返回 true。
func (e *Engine) Evict(now time.Time) bool {
	e.mu.Lock()
	n := 0
	for k, st := range e.transcripts {
		if now.Sub(st.lastUsed) > sessionIdle {
			delete(e.transcripts, k)
			n++
		}
	}
	e.mu.Unlock()
	n += e.git.evict(now, sessionIdle)
	return n > 0
}

func (e *Engine) Stats() (sessions, repos int) {
	e.mu.Lock()
	sessions = len(e.transcripts)
	e.mu.Unlock()
	return sessions, e.git.size()
}

// versionCache：stdin 里没有 version 字段时，才运行一次 `claude --version`，按程序文件修改时间缓存。
type versionCache struct {
	mu      sync.Mutex
	key     string
	version string
}

var versionRe = regexp.MustCompile(`\d+(?:\.\d+)+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?`)

func (v *versionCache) get() string {
	bin, err := exec.LookPath("claude")
	if err != nil {
		return ""
	}
	real, err := filepath.EvalSymlinks(bin)
	if err != nil {
		real = bin
	}
	fi, err := os.Stat(real)
	if err != nil {
		return ""
	}
	key := real + ":" + fi.ModTime().String()
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.key == key {
		return v.version
	}
	cmd := exec.Command(real, "--version")
	cmd.SysProcAttr = hideWindowAttr()
	out, err := cmd.Output()
	v.key = key
	v.version = ""
	if err == nil {
		v.version = versionRe.FindString(strings.TrimSpace(string(out)))
	}
	return v.version
}
