package rag

import (
	"context"
	"errors"
	"testing"

	"github.com/splitsword/fine-codewiki/internal/chunker"
	"github.com/splitsword/fine-codewiki/internal/llm"
	"github.com/splitsword/fine-codewiki/internal/vectorstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockRAGProvider struct {
	embedVecs [][]float32
	embedErr  error
	answer    string
	answerErr error
	embedCalls int
	completeCalls int
}

func (m *mockRAGProvider) Complete(ctx context.Context, prompt string) (string, error) {
	m.completeCalls++
	if m.answerErr != nil {
		return "", m.answerErr
	}
	return m.answer, nil
}

func (m *mockRAGProvider) CompleteStream(ctx context.Context, prompt string) (<-chan string, error) {
	ch := make(chan string)
	go func() {
		defer close(ch)
		if m.answerErr != nil {
			return
		}
		ch <- m.answer
	}()
	return ch, nil
}

func (m *mockRAGProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	m.embedCalls++
	if m.embedErr != nil {
		return nil, m.embedErr
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		if i < len(m.embedVecs) {
			out[i] = m.embedVecs[i]
		} else {
			out[i] = []float32{float32(i)}
		}
	}
	return out, nil
}

func TestAskBasic(t *testing.T) {
	store := vectorstore.New()
	store.Upsert("chunk-1", []float32{1, 0, 0}, &chunker.Chunk{
		ID: "chunk-1", Type: chunker.TypeFunction, Content: "Function create_user(name) -> User", Filename: "services/user_service.py", Name: "create_user",
	})
	store.Upsert("chunk-2", []float32{0, 1, 0}, &chunker.Chunk{
		ID: "chunk-2", Type: chunker.TypeClass, Content: "Class User extends BaseModel", Filename: "models/user.py", Name: "User",
	})

	mock := &mockRAGProvider{
		embedVecs: [][]float32{{1, 0, 0}},
		answer:    "The create_user function accepts a name parameter and returns a User object. It is defined in services/user_service.py.",
	}

	engine := NewEngine(mock, store)
	engine.SetTopK(2)

	ans, err := engine.Ask(context.Background(), "How do I create a user?")
	require.NoError(t, err)
	assert.Equal(t, mock.answer, ans.Text)
	assert.Len(t, ans.Sources, 2)

	// Verify prompt contains context
	assert.Equal(t, 1, mock.embedCalls)
	assert.Equal(t, 1, mock.completeCalls)
}

func TestAskNoProvider(t *testing.T) {
	store := vectorstore.New()
	engine := NewEngine(nil, store)
	_, err := engine.Ask(context.Background(), "What?")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no LLM provider")
}

func TestAskEmptyStore(t *testing.T) {
	store := vectorstore.New()
	mock := &mockRAGProvider{}
	engine := NewEngine(mock, store)
	_, err := engine.Ask(context.Background(), "What?")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "vector store is empty")
}

func TestAskEmbedError(t *testing.T) {
	store := vectorstore.New()
	store.Upsert("chunk-1", []float32{1, 0, 0}, &chunker.Chunk{ID: "chunk-1", Content: "x", Filename: "a.py", Name: "x"})

	mock := &mockRAGProvider{embedErr: errors.New("embed fail")}
	engine := NewEngine(mock, store)
	_, err := engine.Ask(context.Background(), "What?")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "embed query")
}

func TestAskCompleteError(t *testing.T) {
	store := vectorstore.New()
	store.Upsert("chunk-1", []float32{1, 0, 0}, &chunker.Chunk{ID: "chunk-1", Content: "x", Filename: "a.py", Name: "x"})

	mock := &mockRAGProvider{
		embedVecs: [][]float32{{1, 0, 0}},
		answerErr: errors.New("llm down"),
	}
	engine := NewEngine(mock, store)
	_, err := engine.Ask(context.Background(), "What?")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "generate answer")
}

func TestAskSourceDeduplication(t *testing.T) {
	store := vectorstore.New()
	// Two chunks from the same file+name should appear once in sources
	store.Upsert("chunk-1", []float32{1, 0, 0}, &chunker.Chunk{
		ID: "chunk-1", Type: chunker.TypeFunction, Content: "func a", Filename: "a.py", Name: "a",
	})
	store.Upsert("chunk-1-detail", []float32{1, 0, 0}, &chunker.Chunk{
		ID: "chunk-1-detail", Type: chunker.TypeFunction, Content: "func a details", Filename: "a.py", Name: "a",
	})

	mock := &mockRAGProvider{
		embedVecs: [][]float32{{1, 0, 0}},
		answer:    "answer",
	}
	engine := NewEngine(mock, store)
	engine.SetTopK(5)

	ans, err := engine.Ask(context.Background(), "What?")
	require.NoError(t, err)
	assert.Len(t, ans.Sources, 1)
	assert.Equal(t, "a.py", ans.Sources[0].Filename)
	assert.Equal(t, "a", ans.Sources[0].Name)
}

func TestBuildRAGPrompt(t *testing.T) {
	results := []vectorstore.SearchResult{
		{
			Record: &vectorstore.Record{
				Chunk: &chunker.Chunk{
					Filename: "models/user.py",
					Type:     chunker.TypeClass,
					Content:  "Class User extends BaseModel",
				},
			},
			Similarity: 0.95,
		},
	}

	prompt := buildRAGPrompt("What is User?", results, nil)
	assert.Contains(t, prompt, "You are a code assistant")
	assert.Contains(t, prompt, "Class User extends BaseModel")
	assert.Contains(t, prompt, "What is User?")
	assert.Contains(t, prompt, "## Answer")
}

func TestBuildRAGPromptWithSession(t *testing.T) {
	results := []vectorstore.SearchResult{
		{
			Record: &vectorstore.Record{
				Chunk: &chunker.Chunk{
					Filename: "models/user.py",
					Type:     chunker.TypeClass,
					Content:  "Class User extends BaseModel",
				},
			},
			Similarity: 0.95,
		},
	}

	session := NewSession()
	session.AddTurn("What is BaseModel?", "It is the base class.")

	prompt := buildRAGPrompt("What is User?", results, session)
	assert.Contains(t, prompt, "Conversation History")
	assert.Contains(t, prompt, "Q: What is BaseModel?")
	assert.Contains(t, prompt, "A: It is the base class.")
	assert.Contains(t, prompt, "Class User extends BaseModel")
}

func TestAskWithSession(t *testing.T) {
	store := vectorstore.New()
	store.Upsert("chunk-1", []float32{1, 0, 0}, &chunker.Chunk{
		ID: "chunk-1", Type: chunker.TypeFunction, Content: "func create_user(name)", Filename: "services.py", Name: "create_user",
	})

	var capturedPrompt string
	mock := &mockRAGProvider{
		embedVecs: [][]float32{{1, 0, 0}},
		answer:    "answer",
	}
	provider := &promptCapture{Provider: mock, prompt: &capturedPrompt}

	engine := NewEngine(provider, store)
	session := NewSession()
	session.AddTurn("Previous question?", "Previous answer.")

	_, err := engine.AskWithSession(context.Background(), "How do I create a user?", session)
	require.NoError(t, err)

	assert.Contains(t, capturedPrompt, "Previous question?")
	assert.Contains(t, capturedPrompt, "Previous answer.")
	assert.Contains(t, capturedPrompt, "func create_user(name)")
}

func TestSessionTurns(t *testing.T) {
	session := NewSession()
	assert.Empty(t, session.Turns())

	session.AddTurn("Q1", "A1")
	session.AddTurn("Q2", "A2")

	turns := session.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, "Q1", turns[0].Question)
	assert.Equal(t, "A1", turns[0].Answer)
	assert.Equal(t, "Q2", turns[1].Question)
	assert.Equal(t, "A2", turns[1].Answer)

	// Verify defensive copy
	turns[0].Question = "modified"
	assert.Equal(t, "Q1", session.Turns()[0].Question)
}

func TestAskPromptContainsSources(t *testing.T) {
	store := vectorstore.New()
	store.Upsert("chunk-1", []float32{1, 0, 0}, &chunker.Chunk{
		ID: "chunk-1", Type: chunker.TypeFunction, Content: "func hello()", Filename: "hello.py", Name: "hello",
	})

	var capturedPrompt string
	mock := &mockRAGProvider{
		embedVecs: [][]float32{{1, 0, 0}},
		answer:    "answer",
	}
	// Wrap to capture prompt
	provider := &promptCapture{Provider: mock, prompt: &capturedPrompt}

	engine := NewEngine(provider, store)
	_, err := engine.Ask(context.Background(), "What?")
	require.NoError(t, err)

	assert.Contains(t, capturedPrompt, "func hello()")
	assert.Contains(t, capturedPrompt, "hello.py")
}

func TestAskStreamBasic(t *testing.T) {
	store := vectorstore.New()
	store.Upsert("chunk-1", []float32{1, 0, 0}, &chunker.Chunk{
		ID: "chunk-1", Type: chunker.TypeFunction, Content: "func hello()", Filename: "hello.py", Name: "hello",
	})

	mock := &mockRAGProvider{
		embedVecs: [][]float32{{1, 0, 0}},
		answer:    "Hello world",
	}

	engine := NewEngine(mock, store)
	textCh, ans, err := engine.AskStream(context.Background(), "What?")
	require.NoError(t, err)
	require.NotNil(t, ans)

	var tokens []string
	for token := range textCh {
		tokens = append(tokens, token)
	}
	assert.Equal(t, []string{"Hello world"}, tokens)
	assert.Len(t, ans.Sources, 1)
	assert.Equal(t, "hello.py", ans.Sources[0].Filename)
}

func TestAskStreamNoProvider(t *testing.T) {
	store := vectorstore.New()
	engine := NewEngine(nil, store)
	_, _, err := engine.AskStream(context.Background(), "What?")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no LLM provider")
}

func TestAskStreamEmptyStore(t *testing.T) {
	store := vectorstore.New()
	mock := &mockRAGProvider{}
	engine := NewEngine(mock, store)
	_, _, err := engine.AskStream(context.Background(), "What?")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "vector store is empty")
}

type promptCapture struct {
	llm.Provider
	prompt *string
}

func (p *promptCapture) Complete(ctx context.Context, prompt string) (string, error) {
	*p.prompt = prompt
	return p.Provider.Complete(ctx, prompt)
}

func (p *promptCapture) CompleteStream(ctx context.Context, prompt string) (<-chan string, error) {
	*p.prompt = prompt
	return p.Provider.CompleteStream(ctx, prompt)
}
