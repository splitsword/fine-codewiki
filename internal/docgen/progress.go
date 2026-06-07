package docgen

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// activeRenderer 由 generateWikiEnhanced 设置，供 streamComplete 等底层
// 函数在遇到超时/降级时将警告信息中转到进度渲染器留痕（而非直接打 stdout）。
var activeRenderer *progressRenderer

// warnf formats a warning message and routes it to the active progress renderer
// so it appears as a history line above the progress bar. Falls back to stdout
// if no renderer is active.
func warnf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if activeRenderer != nil {
		activeRenderer.log("[警告] " + msg)
		return
	}
	fmt.Fprintln(os.Stdout, msg)
}

// spinnerFrames 用于进度条左侧的旋转动画。
var spinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

// progressRenderer 在终端中渲染单行动画进度条。历史行一次性写出（不会再次
// 触碰），仅底部一行动画进度条用 \r\033[K 原地刷新，从根本上避免长行折行
// 导致的 \033[NA 回退错位和全屏闪烁。
type progressRenderer struct {
	mu        sync.Mutex
	total     int
	completed int
	active    []activeEntry
	startTime time.Time
	frame     int
	isTTY     bool
	ticker    *time.Ticker
	stopped   chan struct{}

	// 以下仅在 isTTY 时有效：追踪当前进度条行内容，避免无变化时刷屏。
	lastBar string
}

type activeEntry struct {
	name   string
	detail string
}

func newProgressRenderer() *progressRenderer {
	fi, _ := os.Stdout.Stat()
	isTTY := (fi.Mode() & os.ModeCharDevice) != 0
	return &progressRenderer{
		startTime: time.Now(),
		isTTY:     isTTY,
		stopped:   make(chan struct{}),
	}
}

// start begins the 250ms render ticker (no-op on non-TTY).
func (p *progressRenderer) start() {
	if !p.isTTY {
		return
	}
	// 隐藏闪烁的光标
	fmt.Fprint(os.Stdout, "\033[?25l")
	p.ticker = time.NewTicker(250 * time.Millisecond)
	go func() {
		for {
			select {
			case <-p.ticker.C:
				p.render()
			case <-p.stopped:
				return
			}
		}
	}()
}

// stop halts the ticker, overwrites the bar line with a ✓ completion line,
// and restores the cursor.
func (p *progressRenderer) stop() {
	if p.ticker != nil {
		p.ticker.Stop()
	}
	close(p.stopped)

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isTTY {
		// 显示进度条最终完成行
		bar := progressBarStr(100, 24)
		elapsed := formatDuration(time.Since(p.startTime))
		fmt.Fprintf(os.Stdout, "\r\033[K\033[32m✓ 文档生成完成\033[0m  [%s]  100%%  %d/%d  ·  %s\n",
			bar, p.total, p.total, elapsed)
		// 恢复光标
		fmt.Fprint(os.Stdout, "\033[?25h")
	}
}

// --- 状态报告 API ---

func (p *progressRenderer) done(name, msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.completed++
	line := fmt.Sprintf("[%s] %s", name, msg)

	for i, a := range p.active {
		if a.name == name {
			p.active = append(p.active[:i], p.active[i+1:]...)
			break
		}
	}

	if p.isTTY {
		// 先把当前进度行清掉 → 换行写历史 → 重画进度条
		fmt.Fprintf(os.Stdout, "\r\033[K%s\n", line)
		p.lastBar = "" // 强制下一帧重绘进度条
	} else {
		fmt.Fprintln(os.Stdout, line)
	}
}

func (p *progressRenderer) tick(name, msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.completed++
	line := fmt.Sprintf("[%s] %s", name, msg)

	if p.isTTY {
		fmt.Fprintf(os.Stdout, "\r\033[K%s\n", line)
		p.lastBar = ""
	} else {
		fmt.Fprintln(os.Stdout, line)
	}
}

func (p *progressRenderer) update(name, detail string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i, a := range p.active {
		if a.name == name {
			p.active[i].detail = detail
			return
		}
	}
	p.active = append(p.active, activeEntry{name: name, detail: detail})
}

func (p *progressRenderer) log(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isTTY {
		fmt.Fprintf(os.Stdout, "\r\033[K%s\n", msg)
		p.lastBar = ""
	} else {
		fmt.Fprintln(os.Stdout, msg)
	}
}

func (p *progressRenderer) bump(n int) {
	p.mu.Lock()
	p.total += n
	p.mu.Unlock()
}

func (p *progressRenderer) remove(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, a := range p.active {
		if a.name == name {
			p.active = append(p.active[:i], p.active[i+1:]...)
			break
		}
	}
}

// --- 渲染核心 — 仅刷新单行进度条 ---

func (p *progressRenderer) render() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.total == 0 {
		return
	}
	p.drawBar()
}

func (p *progressRenderer) drawBar() {
	pct := float64(p.completed) * 100 / float64(p.total)
	bar := progressBarStr(pct, 24)
	sp := spinnerFrames[p.frame%len(spinnerFrames)]
	p.frame++
	elapsed := formatDuration(time.Since(p.startTime))

	var parts []string
	for _, a := range p.active {
		if a.detail != "" {
			parts = append(parts, a.detail)
		} else {
			parts = append(parts, a.name)
		}
	}
	activeStr := strings.Join(parts, "  ")
	if len(parts) > 3 {
		activeStr = strings.Join(parts[:3], "  ") + " …"
	}

	line := fmt.Sprintf("%s 生成中  [%s] %3.0f%%  %d/%d  ·  %s   ▸ %s",
		string(sp), bar, pct, p.completed, p.total, elapsed, activeStr)

	// 仅当内容变化时才写终端，避免无意义的屏幕刷新
	if line == p.lastBar {
		return
	}
	p.lastBar = line

	fmt.Fprintf(os.Stdout, "\r\033[K%s", line)
}

// --- 工具函数 ---

func progressBarStr(pct float64, width int) string {
	filled := int(pct / 100 * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func formatDuration(d time.Duration) string {
	s := int(d.Seconds())
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}
