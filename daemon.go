package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

const (
	idleExit      = 30 * time.Minute
	watchInterval = 30 * time.Second
)

var errAlreadyRunning = errors.New("already running")

type daemon struct {
	ln       net.Listener
	path     string
	version  string
	engine   *Engine
	started  time.Time
	lastReq  atomic.Int64
	requests atomic.Int64
	stopping atomic.Bool
	inflight sync.WaitGroup
}

func runDaemon() int {
	// 状态栏数据量小，压低内存上限，让 Go 运行时尽早回收。
	debug.SetGCPercent(50)
	debug.SetMemoryLimit(48 << 20)

	path := socketPath()
	ln, err := listenExclusive(path)
	if err != nil {
		return 0
	}
	d := &daemon{ln: ln, path: path, version: selfVersion(), engine: newEngine(), started: time.Now()}
	d.lastReq.Store(time.Now().UnixNano())
	go d.watch()
	d.serve()
	return 0
}

// listenExclusive 保证同一用户只有一个常驻进程在监听。
// 已有常驻进程能连上时直接放弃；socket 文件是上次异常退出留下的，就删掉重建。
func listenExclusive(path string) (net.Listener, error) {
	ln, err := net.Listen("unix", path)
	if err == nil {
		_ = os.Chmod(path, 0o600)
		return ln, nil
	}
	if c, derr := net.DialTimeout("unix", path, 200*time.Millisecond); derr == nil {
		c.Close()
		return nil, errAlreadyRunning
	}
	_ = os.Remove(path)
	ln, err = net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	return ln, nil
}

func (d *daemon) serve() {
	for {
		conn, err := d.ln.Accept()
		if err != nil {
			if d.stopping.Load() {
				break
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			break
		}
		d.inflight.Add(1)
		go d.handle(conn)
	}
	// 等正在处理的请求写完回复再退出，最多等 2 秒。
	done := make(chan struct{})
	go func() { d.inflight.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

func (d *daemon) handle(conn net.Conn) {
	defer d.inflight.Done()
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	var req request
	if err := readFrame(conn, &req, maxRequestBytes); err != nil {
		return
	}
	d.lastReq.Store(time.Now().UnixNano())
	d.requests.Add(1)

	switch req.Cmd {
	case "status":
		_ = writeFrame(conn, response{Code: codeOK, Out: d.statusText()})
		return
	case "stop":
		d.shutdown(true)
		_ = writeFrame(conn, response{Code: codeOK, Out: "常驻进程已退出\n"})
		return
	}
	if req.Version != d.version {
		// 先关监听并删掉 socket 文件，客户端收到回复后可以马上拉起新版本。
		d.shutdown(true)
		_ = writeFrame(conn, response{Code: codeVersionMismatch})
		return
	}
	out := d.engine.Render(req, time.Now())
	_ = writeFrame(conn, response{Code: codeOK, Out: out})
}

// shutdown 关闭监听。unlink 为 false 时不删 socket 文件，用于文件已被别的常驻进程接管的情况。
func (d *daemon) shutdown(unlink bool) {
	if !d.stopping.CompareAndSwap(false, true) {
		return
	}
	if ul, ok := d.ln.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(unlink)
	}
	_ = d.ln.Close()
}

func (d *daemon) watch() {
	own, _ := os.Lstat(d.path)
	t := time.NewTicker(watchInterval)
	defer t.Stop()
	for range t.C {
		if d.stopping.Load() {
			return
		}
		now := time.Now()
		if now.Sub(time.Unix(0, d.lastReq.Load())) > idleExit {
			d.shutdown(true)
			return
		}
		// socket 文件被删或被别的常驻进程替换，这个进程已经没人能连上，退出。
		if cur, err := os.Lstat(d.path); err != nil {
			if os.IsNotExist(err) {
				d.shutdown(false)
				return
			}
		} else if own != nil && runtime.GOOS != "windows" && !os.SameFile(own, cur) {
			// Windows 上 socket 文件是重解析点，SameFile 不一定可靠，只检查文件是否还在。
			d.shutdown(false)
			return
		}
		if d.engine.Evict(now) {
			debug.FreeOSMemory()
		}
	}
}

func (d *daemon) statusText() string {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	sessions, repos := d.engine.Stats()
	return fmt.Sprintf("pid %d\n版本 %s\n已运行 %s\n请求数 %d\n跟踪的会话 %d\n缓存的 git 仓库 %d\nGo 堆内存 %.1f MB（向系统申请 %.1f MB）\nsocket %s\n",
		os.Getpid(), d.version, time.Since(d.started).Round(time.Second), d.requests.Load(),
		sessions, repos, float64(ms.HeapInuse)/(1<<20), float64(ms.Sys)/(1<<20), d.path)
}
