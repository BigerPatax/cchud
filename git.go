package main

import (
	"bufio"
	"bytes"
	"context"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type GitStatus struct {
	Branch    string
	Dirty     bool
	Ahead     int
	Behind    int
	BranchURL string
}

const (
	gitFresh     = 2 * time.Second  // 这段时间内直接用缓存
	gitStaleMax  = 30 * time.Second // 超过这个时间的缓存不再返回，改为同步刷新
	remoteFresh  = 60 * time.Second
	gitCmdTimout = time.Second
)

// gitCache 按工作目录缓存 git 状态，同一目录的多个会话共用。
// 缓存过期但不太旧时先返回旧值，同时在后台刷新，状态栏不用等 git。
type gitCache struct {
	mu      sync.Mutex
	entries map[string]*gitEntry
}

type gitEntry struct {
	status     *GitStatus
	fetched    time.Time
	refreshing bool
	remote     string
	remoteAt   time.Time
	lastUsed   time.Time
	done       chan struct{} // 首次查询进行中时，其他请求等它
}

func newGitCache() *gitCache { return &gitCache{entries: map[string]*gitEntry{}} }

func (c *gitCache) get(cwd string, now time.Time) *GitStatus {
	if cwd == "" {
		return nil
	}
	c.mu.Lock()
	e := c.entries[cwd]
	if e == nil {
		e = &gitEntry{done: make(chan struct{})}
		c.entries[cwd] = e
		e.lastUsed = now
		c.mu.Unlock()
		c.refresh(cwd, e)
		close(e.done)
		c.mu.Lock()
		st := e.status
		c.mu.Unlock()
		return st
	}
	e.lastUsed = now
	if done := e.done; done != nil && e.fetched.IsZero() {
		c.mu.Unlock()
		select {
		case <-done:
		case <-time.After(gitCmdTimout):
		}
		c.mu.Lock()
		st := e.status
		c.mu.Unlock()
		return st
	}
	age := now.Sub(e.fetched)
	st := e.status
	if age < gitFresh {
		c.mu.Unlock()
		return st
	}
	if age > gitStaleMax {
		c.mu.Unlock()
		c.refresh(cwd, e)
		c.mu.Lock()
		st = e.status
		c.mu.Unlock()
		return st
	}
	if !e.refreshing {
		e.refreshing = true
		go c.refresh(cwd, e)
	}
	c.mu.Unlock()
	return st
}

func (c *gitCache) refresh(cwd string, e *gitEntry) {
	st := readGitStatus(cwd)
	now := time.Now()
	c.mu.Lock()
	needRemote := st != nil && now.Sub(e.remoteAt) > remoteFresh
	c.mu.Unlock()
	remote := ""
	if needRemote {
		remote = gitOutput(cwd, "remote", "get-url", "origin")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if needRemote {
		e.remote = strings.TrimSpace(remote)
		e.remoteAt = now
	}
	if st != nil {
		st.BranchURL = githubBranchURL(e.remote, st.Branch)
	}
	e.status = st
	e.fetched = now
	e.refreshing = false
}

func (c *gitCache) evict(now time.Time, maxIdle time.Duration) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for k, e := range c.entries {
		if now.Sub(e.lastUsed) > maxIdle && !e.refreshing {
			delete(c.entries, k)
			n++
		}
	}
	return n
}

func (c *gitCache) size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// readGitStatus 用一条命令拿到分支、领先落后数和是否有改动。claude-hud 原来要跑四条。
func readGitStatus(cwd string) *GitStatus {
	out := gitOutput(cwd, "-c", "core.quotePath=false", "--no-optional-locks", "status", "--porcelain=v2", "--branch")
	if out == "" {
		return nil
	}
	st := &GitStatus{}
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "# branch.head "):
			st.Branch = strings.TrimPrefix(line, "# branch.head ")
			if st.Branch == "(detached)" {
				st.Branch = "HEAD"
			}
		case strings.HasPrefix(line, "# branch.ab "):
			for _, f := range strings.Fields(strings.TrimPrefix(line, "# branch.ab ")) {
				n, _ := strconv.Atoi(f[1:])
				if f[0] == '+' {
					st.Ahead = n
				} else if f[0] == '-' {
					st.Behind = n
				}
			}
		case strings.HasPrefix(line, "#"):
		case line != "":
			st.Dirty = true
		}
	}
	if st.Branch == "" {
		return nil
	}
	return st
}

func gitOutput(cwd string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), gitCmdTimout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	cmd.SysProcAttr = hideWindowAttr()
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return ""
	}
	return out.String()
}

func githubBranchURL(remote, branch string) string {
	base := remote
	switch {
	case strings.HasPrefix(base, "git@github.com:"):
		base = "https://github.com/" + strings.TrimPrefix(base, "git@github.com:")
	case strings.HasPrefix(base, "ssh://git@github.com/"):
		base = "https://github.com/" + strings.TrimPrefix(base, "ssh://git@github.com/")
	}
	base = strings.TrimSuffix(base, ".git")
	if !strings.HasPrefix(base, "https://github.com/") || branch == "" {
		return ""
	}
	return base + "/tree/" + jsEncodeURIComponent(branch)
}

// jsEncodeURIComponent 和 JavaScript 的 encodeURIComponent 一致。
func jsEncodeURIComponent(s string) string {
	e := url.QueryEscape(s)
	e = strings.ReplaceAll(e, "+", "%20")
	for _, ch := range []string{"!", "'", "(", ")", "*"} {
		e = strings.ReplaceAll(e, url.QueryEscape(ch), ch)
	}
	return e
}
