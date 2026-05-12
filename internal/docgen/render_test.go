package docgen

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarkdownToHTMLHeaders(t *testing.T) {
	src := []byte("# H1\n## H2\n### H3\n#### H4\n##### H5\n###### H6\n")
	html := MarkdownToHTML(src)
	assert.Contains(t, string(html), "<h1>H1</h1>")
	assert.Contains(t, string(html), "<h2>H2</h2>")
	assert.Contains(t, string(html), "<h3>H3</h3>")
	assert.Contains(t, string(html), "<h4>H4</h4>")
	assert.Contains(t, string(html), "<h5>H5</h5>")
	assert.Contains(t, string(html), "<h6>H6</h6>")
}

func TestMarkdownToHTMLCodeBlock(t *testing.T) {
	src := []byte("```go\nfmt.Println(\"hello\")\n```\n")
	html := MarkdownToHTML(src)
	assert.Contains(t, string(html), "<pre><code")
	assert.Contains(t, string(html), "fmt.Println")
}

func TestMarkdownToHTMLMermaidBlock(t *testing.T) {
	src := []byte("```mermaid\ngraph TD\n```\n")
	html := MarkdownToHTML(src)
	assert.Contains(t, string(html), `<div class="mermaid">`)
	assert.Contains(t, string(html), "graph TD")
}

func TestMarkdownToHTMLUnorderedList(t *testing.T) {
	src := []byte("- Item 1\n- Item 2\n")
	html := MarkdownToHTML(src)
	assert.Contains(t, string(html), "<ul>")
	assert.Contains(t, string(html), "<li>Item 1</li>")
	assert.Contains(t, string(html), "<li>Item 2</li>")
	assert.Contains(t, string(html), "</ul>")
}

func TestMarkdownToHTMLOrderedList(t *testing.T) {
	src := []byte("1. First\n2. Second\n")
	html := MarkdownToHTML(src)
	assert.Contains(t, string(html), "<ol>")
	assert.Contains(t, string(html), "<li>First</li>")
	assert.Contains(t, string(html), "<li>Second</li>")
	assert.Contains(t, string(html), "</ol>")
}

func TestMarkdownToHTMLBlockquote(t *testing.T) {
	src := []byte("> Quote text\n")
	html := MarkdownToHTML(src)
	assert.Contains(t, string(html), "<blockquote>")
	assert.Contains(t, string(html), "Quote text")
}

func TestMarkdownToHTMLHorizontalRule(t *testing.T) {
	src := []byte("---\n")
	html := MarkdownToHTML(src)
	assert.Contains(t, string(html), "<hr>")
}

func TestMarkdownToHTMLTable(t *testing.T) {
	src := []byte("| A | B |\n|---|---|\n| 1 | 2 |\n")
	html := MarkdownToHTML(src)
	assert.Contains(t, string(html), "<table>")
	assert.Contains(t, string(html), "<th>A</th>")
	assert.Contains(t, string(html), "<th>B</th>")
	assert.Contains(t, string(html), "<td>1</td>")
	assert.Contains(t, string(html), "<td>2</td>")
}

func TestMarkdownToHTMLParagraph(t *testing.T) {
	src := []byte("Hello world\n")
	html := MarkdownToHTML(src)
	assert.Contains(t, string(html), "<p>Hello world</p>")
}

func TestMarkdownToHTMLInlineBold(t *testing.T) {
	src := []byte("**bold**\n")
	html := MarkdownToHTML(src)
	assert.Contains(t, string(html), "<strong>bold</strong>")
}

func TestMarkdownToHTMLInlineItalic(t *testing.T) {
	src := []byte("*italic*\n")
	html := MarkdownToHTML(src)
	assert.Contains(t, string(html), "<em>italic</em>")
}

func TestMarkdownToHTMLInlineLink(t *testing.T) {
	src := []byte("[link](https://example.com)\n")
	html := MarkdownToHTML(src)
	assert.Contains(t, string(html), `<a href="https://example.com">link</a>`)
}

func TestMarkdownToHTMLInlineCode(t *testing.T) {
	src := []byte("`code`\n")
	html := MarkdownToHTML(src)
	assert.Contains(t, string(html), "<code>code</code>")
}

func TestMarkdownToHTMLEmpty(t *testing.T) {
	html := MarkdownToHTML([]byte(""))
	assert.Contains(t, string(html), "<body>")
}

func TestMarkdownToHTMLCombined(t *testing.T) {
	src := []byte("# Title\n\nParagraph with **bold** and *italic*.\n\n```\ncode block\n```\n\n")
	html := MarkdownToHTML(src)
	assert.Contains(t, string(html), "<h1>Title</h1>")
	assert.Contains(t, string(html), "<strong>bold</strong>")
	assert.Contains(t, string(html), "<em>italic</em>")
	assert.Contains(t, string(html), "<pre><code")
}

func TestRenderInlineLinkBoldItalicCode(t *testing.T) {
	result := RenderInline("[a](http://b) **c** *d* `e`")
	assert.Contains(t, result, `<a href="http://b">a</a>`)
	assert.Contains(t, result, "<strong>c</strong>")
	assert.Contains(t, result, "<em>d</em>")
	assert.Contains(t, result, "<code>e</code>")
}

func TestHTMLEscape(t *testing.T) {
	assert.Equal(t, "&lt;div&gt;", HTMLEscape("<div>"))
	assert.Equal(t, "&amp;", HTMLEscape("&"))
	assert.Equal(t, "&quot;", HTMLEscape("\""))
}

func TestOrderedListMatch(t *testing.T) {
	assert.True(t, orderedListMatch("1. item"))
	assert.True(t, orderedListMatch("99. item"))
	assert.False(t, orderedListMatch("abc"))
	assert.False(t, orderedListMatch("1 item"))
}

func TestOrderedListItem(t *testing.T) {
	assert.Equal(t, "item", orderedListItem("1. item"))
	assert.Equal(t, "item", orderedListItem("99. item"))
}

func TestSplitTableCells(t *testing.T) {
	cells := splitTableCells("| a | b |")
	require.Len(t, cells, 2)
	assert.Equal(t, " a ", cells[0])
	assert.Equal(t, " b ", cells[1])
}

func TestIsTableSeparator(t *testing.T) {
	assert.True(t, isTableSeparator("|---|---|"))
	assert.True(t, isTableSeparator("| :-- | --: |"))
	assert.False(t, isTableSeparator("| a | b |"))
}

func TestMarkdownToHTMLMultipleParagraphs(t *testing.T) {
	src := []byte("First para.\n\nSecond para.\n")
	html := MarkdownToHTML(src)
	assert.Contains(t, string(html), "<p>First para.</p>")
	assert.Contains(t, string(html), "<p>Second para.</p>")
}
