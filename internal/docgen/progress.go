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

// progressRenderer 在终端中渲染单行动画进度条，已完成任务以上方留痕记录。
// 非 TTY（管道/文件重定向）时自动降级为逐行打印，不输出 ANSI 转义序列。
type progressRenderer struct {
	mu           sync.Mutex
	total        int
	completed    int
	history      []string // 已完成的任务消息行，会以留痕方式展示在进度条上方
	active       []activeEntry
	startTime    time.Time
	frame        int
	isTTY        bool
	ticker       *time.Ticker
	stopped      chan struct{}
	linesPrinted int // 上一次渲染占用的终端行数（用于 \033[NA 回退）
}

type activeEntry struct {
	name   string
	detail string // 当前子状态，如 "第 3/8 批进行中"
}

// newProgressRenderer creates a renderer; call start() to begin the render loop.
func newProgressRenderer() *progressRenderer {
	fi, _ := os.Stdout.Stat()
	isTTY := (fi.Mode() & os.ModeCharDevice) != 0
	return &progressRenderer{
		startTime: time.Now(),
		isTTY:     isTTY,
		stopped:   make(chan struct{}),
	}
}

// start begins the 100ms render ticker (no-op on non-TTY).
func (p *progressRenderer) start() {
	if !p.isTTY {
		return
	}
	p.ticker = time.NewTicker(100 * time.Millisecond)
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

// stop halts the ticker, clears the bar area, reprints history cleanly,
// and prints a green ✓ completion line.
func (p *progressRenderer) stop() {
	if p.ticker != nil {
		p.ticker.Stop()
	}
	close(p.stopped)

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isTTY {
		// 清除进度条所占行
		if p.linesPrinted > 0 {
			fmt.Fprintf(os.Stdout, "\033[%dA", p.linesPrinted)
			fmt.Fprint(os.Stdout, "\033[J")
			p.linesPrinted = 0
		}
		// 重新干净地输出全部历史
		for _, h := range p.history {
			fmt.Fprintln(os.Stdout, h)
			p.linesPrinted++
		}
		// 完成行
		bar := progressBarStr(100, 24)
		elapsed := formatDuration(time.Since(p.startTime))
		fmt.Fprintf(os.Stdout, "\033[32m✓ 文档生成完成\033[0m  [%s]  100%%  %d/%d  ·  %s\n",
			bar, p.total, p.total, elapsed)
	}
}

// --- 供 docgen 使用的状态报告 API ---

// done marks a task as fully complete: counter++, history++, remove from active.
func (p *progressRenderer) done(name, msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.completed++
	line := fmt.Sprintf("[%s] %s", name, msg)
	p.history = append(p.history, line)

	for i, a := range p.active {
		if a.name == name {
			p.active = append(p.active[:i], p.active[i+1:]...)
			break
		}
	}

	if !p.isTTY {
		fmt.Fprintln(os.Stdout, line)
	}
}

// tick increments the counter and adds a history line without removing the
// task from active. Useful for sub-completions like function description batches.
func (p *progressRenderer) tick(name, msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.completed++
	line := fmt.Sprintf("[%s] %s", name, msg)
	p.history = append(p.history, line)

	if !p.isTTY {
		fmt.Fprintln(os.Stdout, line)
	}
}

// update adds or refreshes an active task display entry (e.g. "开始生成…").
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

// log appends a message to the history without moving the completed counter.
func (p *progressRenderer) log(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.history = append(p.history, msg)

	if !p.isTTY {
		fmt.Fprintln(os.Stdout, msg)
	}
}

// bump adds n to the total task count.
func (p *progressRenderer) bump(n int) {
	p.mu.Lock()
	p.total += n
	p.mu.Unlock()
}

// remove deletes a task from the active list without touching the counter.
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

// --- 渲染核心 ---

func (p *progressRenderer) render() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 回退到本次渲染区域的起点并清屏
	if p.linesPrinted > 0 {
		fmt.Fprintf(os.Stdout, "\033[%dA", p.linesPrinted)
		fmt.Fprint(os.Stdout, "\033[J")
	}
	p.linesPrinted = 0

	// 重绘全部历史行
	for _, h := range p.history {
		fmt.Fprintln(os.Stdout, h)
		p.linesPrinted++
	}

	// 重绘进度条
	if p.total > 0 {
		p.drawBar()
	}
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

	fmt.Fprintf(os.Stdout, "%s 生成中  [%s] %3.0f%%  %d/%d  ·  %s   ▸ %s",
		string(sp), bar, pct, p.completed, p.total, elapsed, activeStr)
	p.linesPrinted++
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
