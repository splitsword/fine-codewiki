package docgen

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/signintech/gopdf"
)

// GeneratePDF renders the Wiki as a PDF document.
// It detects system CJK fonts automatically to ensure proper Chinese rendering.
// If no CJK font is found, it returns an error so callers can decide whether
// to warn the user or fall back to HTML/Markdown exports.
func GeneratePDF(wiki *Wiki) ([]byte, error) {
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{
		PageSize: *gopdf.PageSizeA4,
		Unit:     gopdf.UnitPT,
	})

	fontPath, fontErr := findCJKFont()
	fontLoaded := false
	if fontErr == nil {
		if err := pdf.AddTTFFontWithOption("cn", fontPath, gopdf.TtfOption{}); err == nil {
			fontLoaded = true
		} else if err := pdf.AddTTFFont("cn", fontPath); err == nil {
			fontLoaded = true
		}
	}

	if !fontLoaded {
		return nil, fmt.Errorf("无法加载中文字体: %v", fontErr)
	}

	const (
		marginX = 40.0
		marginY = 50.0
		width   = 515.0 // A4 width 595pt minus 2*marginX
	)

	// Cover page
	pdf.AddPage()
	pdf.SetFont("cn", "", 24)
	pdf.SetXY(marginX, marginY+120)
	_ = pdf.MultiCell(&gopdf.Rect{W: width, H: 200}, wiki.ProjectName+" Wiki")
	pdf.SetXY(marginX, marginY+180)
	pdf.SetFont("cn", "", 12)
	_ = pdf.MultiCell(&gopdf.Rect{W: width, H: 100}, "本文档由 CodeWiki 自动生成")

	// Content sections
	sections := []struct {
		title   string
		content string
	}{
		{"项目概述", wiki.Overview},
		{"架构说明", wiki.Architecture},
		{"API 参考", wiki.APIReference},
	}

	for _, sec := range sections {
		renderMarkdownSection(pdf, sec.title, sec.content, marginX, marginY, width)
	}

	renderDiagramAppendix(pdf, wiki, marginX, marginY, width)

	var buf bytes.Buffer
	if err := pdf.Write(&buf); err != nil {
		return nil, fmt.Errorf("write pdf: %w", err)
	}
	return buf.Bytes(), nil
}

func renderMarkdownSection(pdf *gopdf.GoPdf, title, content string, marginX, marginY, width float64) {
	pdf.AddPage()
	pdf.SetFont("cn", "", 18)
	pdf.SetXY(marginX, marginY)
	_ = pdf.MultiCell(&gopdf.Rect{W: width, H: 100}, title)
	y := pdf.GetY() + 16

	lines := strings.Split(content, "\n")
	inCode := false
	var codeLines []string

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		if strings.HasPrefix(line, "```") {
			if inCode {
				y = renderCodeBlock(pdf, codeLines, marginX, y, width, marginY)
				codeLines = nil
			}
			inCode = !inCode
			continue
		}

		if inCode {
			codeLines = append(codeLines, line)
			continue
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			y += 8
			if y > 842-marginY {
				pdf.AddPage()
				y = marginY
			}
			continue
		}

		switch {
		case strings.HasPrefix(trimmed, "# "):
			y = renderHeading(pdf, trimmed[2:], 16, marginX, y, width, marginY)
		case strings.HasPrefix(trimmed, "## "):
			y = renderHeading(pdf, trimmed[3:], 14, marginX, y, width, marginY)
		case strings.HasPrefix(trimmed, "### "):
			y = renderHeading(pdf, trimmed[4:], 12, marginX, y, width, marginY)
		case strings.HasPrefix(trimmed, "#### ") || strings.HasPrefix(trimmed, "##### ") || strings.HasPrefix(trimmed, "###### "):
			level := 0
			for _, c := range trimmed {
				if c == '#' {
					level++
				} else {
					break
				}
			}
			y = renderHeading(pdf, strings.TrimSpace(trimmed[level:]), 11, marginX, y, width, marginY)
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
			text := stripInlineMarkdown(trimmed[2:])
			y = renderParagraph(pdf, "• "+text, 10, marginX+15, y, width-15, marginY)
		case strings.HasPrefix(trimmed, "> "):
			text := stripInlineMarkdown(trimmed[2:])
			y = renderParagraph(pdf, text, 10, marginX+15, y, width-15, marginY)
		case strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|"):
			// Collect consecutive table rows
			var tableRows []string
			tableRows = append(tableRows, trimmed)
			for i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "|") {
				i++
				tableRows = append(tableRows, strings.TrimSpace(lines[i]))
			}
			for _, row := range tableRows {
				inner := strings.Trim(row, "|")
				sepOnly := true
				for _, c := range inner {
					if c != '-' && c != '|' && c != ':' && c != ' ' {
						sepOnly = false
						break
					}
				}
				if sepOnly {
					continue
				}
				y = renderParagraph(pdf, row, 9, marginX, y, width, marginY)
			}
			y += 4
		case trimmed == "---" || trimmed == "***" || trimmed == "___":
			y = renderHorizontalRule(pdf, marginX, y, width, marginY)
		default:
			text := stripInlineMarkdown(trimmed)
			y = renderParagraph(pdf, text, 10, marginX, y, width, marginY)
		}
	}

	if inCode && len(codeLines) > 0 {
		renderCodeBlock(pdf, codeLines, marginX, y, width, marginY)
	}
}

func renderHeading(pdf *gopdf.GoPdf, text string, size, x, y, width, marginY float64) float64 {
	pdf.SetFont("cn", "", size)
	lines, _ := pdf.SplitText(text, width)
	lineHeight := size * 1.4
	height := float64(len(lines)) * lineHeight
	if y+height > 842-marginY {
		pdf.AddPage()
		y = marginY
	}
	pdf.SetXY(x, y)
	_ = pdf.MultiCell(&gopdf.Rect{W: width, H: height + 10}, text)
	return pdf.GetY() + size*0.4
}

func renderParagraph(pdf *gopdf.GoPdf, text string, size, x, y, width, marginY float64) float64 {
	pdf.SetFont("cn", "", size)
	lines, _ := pdf.SplitText(text, width)
	lineHeight := size * 1.6
	height := float64(len(lines)) * lineHeight
	if y+height > 842-marginY {
		pdf.AddPage()
		y = marginY
	}
	pdf.SetXY(x, y)
	_ = pdf.MultiCell(&gopdf.Rect{W: width, H: height + 10}, text)
	return pdf.GetY() + 4
}

func renderCodeBlock(pdf *gopdf.GoPdf, lines []string, x, y, width, marginY float64) float64 {
	size := 8.0
	lineHeight := size * 1.5
	pdf.SetFont("cn", "", size)
	for _, line := range lines {
		spaces := 0
		for _, c := range line {
			if c == ' ' {
				spaces++
			} else if c == '\t' {
				spaces += 4
			} else {
				break
			}
		}
		display := ""
		if spaces < len(line) {
			display = line[spaces:]
		}
		offset := float64(spaces) * size * 0.35
		avail := width - 20 - offset
		split, _ := pdf.SplitText(display, avail)
		for j, seg := range split {
			if y+lineHeight > 842-marginY {
				pdf.AddPage()
				y = marginY
			}
			ox := x + 10
			if j == 0 {
				ox += offset
			}
			pdf.SetXY(ox, y)
			_ = pdf.Cell(&gopdf.Rect{W: avail, H: lineHeight}, seg)
			y += lineHeight
		}
	}
	return y + 6
}

func renderHorizontalRule(pdf *gopdf.GoPdf, x, y, width, marginY float64) float64 {
	if y+10 > 842-marginY {
		pdf.AddPage()
		y = marginY
	}
	pdf.Line(x, y+5, x+width, y+5)
	return y + 12
}

func renderDiagramAppendix(pdf *gopdf.GoPdf, wiki *Wiki, marginX, marginY, width float64) {
	hasAny := wiki.ArchitectureDiagram != "" || wiki.ClassDiagram != "" || wiki.SequenceDiagram != ""
	if !hasAny {
		return
	}
	pdf.AddPage()
	pdf.SetFont("cn", "", 18)
	pdf.SetXY(marginX, marginY)
	_ = pdf.MultiCell(&gopdf.Rect{W: width, H: 100}, "图表附录")
	y := pdf.GetY() + 12

	sections := []struct {
		title       string
		dsl         string
		description string
	}{
		{"架构图", wiki.ArchitectureDiagram, ""},
		{"类图", wiki.ClassDiagram, ""},
		{"时序图", wiki.SequenceDiagram, wiki.SequenceDescription},
	}
	for _, sec := range sections {
		if sec.dsl == "" {
			continue
		}
		if y > 842-marginY-80 {
			pdf.AddPage()
			y = marginY
		}
		pdf.SetFont("cn", "", 12)
		pdf.SetXY(marginX, y)
		_ = pdf.MultiCell(&gopdf.Rect{W: width, H: 30}, sec.title)
		y = pdf.GetY() + 4
		if sec.description != "" {
			pdf.SetFont("cn", "", 9)
			pdf.SetXY(marginX, y)
			_ = pdf.MultiCell(&gopdf.Rect{W: width, H: 30}, "场景描述："+sec.description)
			y = pdf.GetY() + 4
		}
		pdf.SetFont("cn", "", 8)
		y = renderCodeBlock(pdf, strings.Split(sec.dsl, "\n"), marginX, y, width, marginY)
		y += 8
	}
}

func stripInlineMarkdown(s string) string {
	// Bold **text**
	for {
		start := strings.Index(s, "**")
		if start == -1 {
			break
		}
		end := strings.Index(s[start+2:], "**")
		if end == -1 {
			break
		}
		end += start + 2
		s = s[:start] + s[start+2:end] + s[end+2:]
	}
	// Bold __text__
	for {
		start := strings.Index(s, "__")
		if start == -1 {
			break
		}
		end := strings.Index(s[start+2:], "__")
		if end == -1 {
			break
		}
		end += start + 2
		s = s[:start] + s[start+2:end] + s[end+2:]
	}
	// Inline code `code`
	for {
		i := strings.Index(s, "`")
		if i == -1 {
			break
		}
		j := strings.Index(s[i+1:], "`")
		if j == -1 {
			break
		}
		j += i + 1
		s = s[:i] + s[i+1:j] + s[j+1:]
	}
	// Links [text](url) -> text
	for {
		start := strings.Index(s, "[")
		if start == -1 {
			break
		}
		end := strings.Index(s[start:], "]")
		if end == -1 {
			break
		}
		end += start
		urlStart := strings.Index(s[end:], "(")
		if urlStart == -1 {
			break
		}
		urlStart += end
		urlEnd := strings.Index(s[urlStart:], ")")
		if urlEnd == -1 {
			break
		}
		urlEnd += urlStart
		s = s[:start] + s[start+1:end] + s[urlEnd+1:]
	}
	return s
}

func findCJKFont() (string, error) {
	var candidates []string
	switch runtime.GOOS {
	case "windows":
		candidates = []string{
			`C:\Windows\Fonts\simhei.ttf`,
			`C:\Windows\Fonts\simfang.ttf`,
			`C:\Windows\Fonts\msyh.ttc`,
			`C:\Windows\Fonts\msyhbd.ttc`,
			`C:\Windows\Fonts\simsun.ttc`,
		}
	case "darwin":
		candidates = []string{
			"/System/Library/Fonts/PingFang.ttc",
			"/System/Library/Fonts/STHeiti Light.ttc",
			"/Library/Fonts/Arial Unicode.ttf",
			"/System/Library/Fonts/Hiragino Sans GB.ttc",
		}
	default: // linux and others
		candidates = []string{
			"/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc",
			"/usr/share/fonts/truetype/wqy/wqy-microhei.ttc",
			"/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf",
		}
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("未找到系统 CJK 字体，请安装中文字体（如文泉驿正黑、Noto Sans CJK、微软雅黑等）")
}
