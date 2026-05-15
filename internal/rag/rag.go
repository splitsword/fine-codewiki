package rag

import (
	"context"
	"fmt"
	"strings"

	"github.com/splitsword/fine-codewiki/internal/chunker"
	"github.com/splitsword/fine-codewiki/internal/llm"
	"github.com/splitsword/fine-codewiki/internal/vectorstore"
)

// Answer holds the LLM response with source citations.
type Answer struct {
	Text    string
	Sources []Source
}

// Source references a code chunk used in the answer.
type Source struct {
	Filename  string
	Name      string
	Type      chunker.ChunkType
	StartLine int
}

// Turn represents a single Q&A exchange in a conversation.
type Turn struct {
	Question string
	Answer   string
}

// Session holds conversation history for multi-turn RAG.
type Session struct {
	turns []Turn
}

// NewSession creates an empty conversation session.
func NewSession() *Session {
	return &Session{}
}

// AddTurn appends a Q&A pair to the session history.
func (s *Session) AddTurn(question, answer string) {
	s.turns = append(s.turns, Turn{Question: question, Answer: answer})
}

// Turns returns the conversation history.
func (s *Session) Turns() []Turn {
	return append([]Turn(nil), s.turns...)
}

// Engine performs RAG retrieval and generation.
type Engine struct {
	provider       llm.Provider
	store          *vectorstore.VectorStore
	topK           int
	projectName    string
	projectContext string
	minSimilarity  float64
}

// NewEngine creates a RAG engine.
func NewEngine(provider llm.Provider, store *vectorstore.VectorStore) *Engine {
	return &Engine{
		provider:      provider,
		store:         store,
		topK:          5,
		minSimilarity: 0.3,
	}
}

// Close releases resources held by the engine (e.g. the vector store).
func (e *Engine) Close() error {
	if e.store != nil {
		return e.store.Close()
	}
	return nil
}

// SetProjectContext provides project-level metadata used in RAG prompts.
func (e *Engine) SetProjectContext(name, contextSummary string) {
	e.projectName = name
	e.projectContext = contextSummary
}

// SetTopK configures how many chunks to retrieve.
func (e *Engine) SetTopK(k int) {
	if k > 0 {
		e.topK = k
	}
}

// SetMinSimilarity sets the minimum cosine similarity threshold for search results.
func (e *Engine) SetMinSimilarity(s float64) {
	if s >= 0 {
		e.minSimilarity = s
	}
}

// Ask answers a question using RAG (stateless, no session history).
func (e *Engine) Ask(ctx context.Context, question string) (*Answer, error) {
	return e.AskWithSession(ctx, question, nil)
}

// AskWithSession answers a question using RAG with optional conversation history.
func (e *Engine) AskWithSession(ctx context.Context, question string, session *Session) (*Answer, error) {
	if e.provider == nil {
		return nil, fmt.Errorf("no LLM provider configured")
	}
	if e.store.Count() == 0 {
		return nil, fmt.Errorf("vector store is empty: run 'codewiki generate' first")
	}

	// 1. Embed the query
	queryVecs, err := e.provider.Embed(ctx, []string{question})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(queryVecs) == 0 {
		return nil, fmt.Errorf("empty embedding returned for query")
	}

	// 2. Retrieve top-K chunks
	results := e.store.Search(queryVecs[0], e.topK, e.minSimilarity)
	if len(results) == 0 {
		return nil, fmt.Errorf("no relevant code found for the question")
	}

	// 3. Build context prompt
	prompt := e.buildRAGPrompt(question, results, session)

	// 4. Generate answer
	text, err := e.provider.Complete(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("generate answer: %w", err)
	}

	// 5. Collect sources
	sources := collectSources(results)

	return &Answer{
		Text:    text,
		Sources: sources,
	}, nil
}

// AskStream returns a channel that streams answer tokens. The returned *Answer
// contains sources and is safe to read once the channel is closed.
func (e *Engine) AskStream(ctx context.Context, question string) (<-chan string, *Answer, error) {
	return e.AskStreamWithSession(ctx, question, nil)
}

// AskStreamWithSession returns a channel that streams answer tokens with
// optional conversation history.
func (e *Engine) AskStreamWithSession(ctx context.Context, question string, session *Session) (<-chan string, *Answer, error) {
	if e.provider == nil {
		return nil, nil, fmt.Errorf("no LLM provider configured")
	}
	if e.store.Count() == 0 {
		return nil, nil, fmt.Errorf("vector store is empty: run 'codewiki generate' first")
	}

	// 1. Embed the query
	queryVecs, err := e.provider.Embed(ctx, []string{question})
	if err != nil {
		return nil, nil, fmt.Errorf("embed query: %w", err)
	}
	if len(queryVecs) == 0 {
		return nil, nil, fmt.Errorf("empty embedding returned for query")
	}

	// 2. Retrieve top-K chunks
	results := e.store.Search(queryVecs[0], e.topK, e.minSimilarity)
	if len(results) == 0 {
		return nil, nil, fmt.Errorf("no relevant code found for the question")
	}

	// 3. Build context prompt
	prompt := e.buildRAGPrompt(question, results, session)

	// 4. Start streaming
	textCh, err := e.provider.CompleteStream(ctx, prompt)
	if err != nil {
		return nil, nil, fmt.Errorf("generate answer: %w", err)
	}

	// 5. Collect sources (available immediately)
	sources := collectSources(results)
	ans := &Answer{Sources: sources}

	return textCh, ans, nil
}

func collectSources(results []vectorstore.SearchResult) []Source {
	var sources []Source
	seen := make(map[string]bool)
	for _, r := range results {
		if r.Record.Chunk == nil {
			continue
		}
		key := r.Record.Chunk.Filename + "#" + r.Record.Chunk.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		sources = append(sources, Source{
			Filename:  r.Record.Chunk.Filename,
			Name:      r.Record.Chunk.Name,
			Type:      r.Record.Chunk.Type,
			StartLine: r.Record.Chunk.StartLine,
		})
	}
	return sources
}

func (e *Engine) buildRAGPrompt(question string, results []vectorstore.SearchResult, session *Session) string {
	var b strings.Builder

	// System persona with project context
	if e.projectName != "" {
		b.WriteString(fmt.Sprintf("你是 %s 项目的代码助手。", e.projectName))
	} else {
		b.WriteString("你是项目代码助手。")
	}
	b.WriteString("基于下面的代码上下文回答用户的问题。")
	b.WriteString("如果上下文不足以回答问题，诚实说明缺少哪些信息。")
	b.WriteString("引用代码时请标注源文件路径。")
	b.WriteString("用提问者使用的语言回答。\n")

	if e.projectContext != "" {
		b.WriteString(fmt.Sprintf("\n## 项目背景\n%s\n", e.projectContext))
	}

	b.WriteString("\n## 代码上下文\n\n")
	for i, r := range results {
		ch := r.Record.Chunk
		if ch == nil {
			continue
		}
		b.WriteString(fmt.Sprintf("### 上下文 %d (%s - %s)\n", i+1, ch.Filename, ch.Type))
		b.WriteString("```\n")
		b.WriteString(ch.Content)
		b.WriteString("\n```\n\n")
	}

	// Include conversation history if present
	if session != nil && len(session.turns) > 0 {
		b.WriteString("## 对话历史\n\n")
		for _, turn := range session.turns {
			b.WriteString(fmt.Sprintf("问：%s\n", turn.Question))
			b.WriteString(fmt.Sprintf("答：%s\n\n", turn.Answer))
		}
	}

	b.WriteString("## 问题\n")
	b.WriteString(question)
	b.WriteString("\n")

	return b.String()
}
