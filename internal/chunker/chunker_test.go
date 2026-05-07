package chunker

import (
	"testing"

	"github.com/splitsword/fine-codewiki/internal/analyzer"
	"github.com/stretchr/testify/assert"
)

func TestChunkerBasicFile(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "models/user.py",
			Classes: []analyzer.ClassInfo{
				{
					Name:    "User",
					Bases:   []string{"BaseModel"},
					Methods: []analyzer.FunctionInfo{{Name: "greet", Params: []string{"self"}, ReturnType: "str"}},
				},
			},
			Functions: []analyzer.FunctionInfo{
				{Name: "create_user", Params: []string{"name"}, ReturnType: "User"},
			},
			Imports: []analyzer.ImportInfo{
				{Module: "dataclasses", Name: "dataclass"},
				{Module: "typing", Name: "Optional"},
			},
		},
	}

	c := New()
	chunks := c.ChunkFiles(files)

	assert.Len(t, chunks, 3)

	// Import chunk
	impChunk := chunks[0]
	assert.Equal(t, TypeImport, impChunk.Type)
	assert.Equal(t, "models/user.py#imports", impChunk.ID)
	assert.Contains(t, impChunk.Content, "dataclasses")
	assert.Contains(t, impChunk.Content, "typing")

	// Class chunk
	clsChunk := chunks[1]
	assert.Equal(t, TypeClass, clsChunk.Type)
	assert.Equal(t, "models/user.py#User", clsChunk.ID)
	assert.Equal(t, "User", clsChunk.Name)
	assert.Equal(t, []string{"BaseModel"}, clsChunk.Bases)
	assert.Contains(t, clsChunk.Content, "Class User extends BaseModel")
	assert.Contains(t, clsChunk.Content, "greet(self) -> str")

	// Function chunk
	fnChunk := chunks[2]
	assert.Equal(t, TypeFunction, fnChunk.Type)
	assert.Equal(t, "models/user.py#create_user", fnChunk.ID)
	assert.Equal(t, "create_user", fnChunk.Name)
	assert.Contains(t, fnChunk.Content, "Function create_user(name) -> User")
}

func TestChunkerEmptyFile(t *testing.T) {
	files := []*analyzer.FileResult{
		{Filename: "empty.py"},
	}

	c := New()
	chunks := c.ChunkFiles(files)

	assert.Len(t, chunks, 1)
	assert.Equal(t, TypeModule, chunks[0].Type)
	assert.Equal(t, "empty.py#module", chunks[0].ID)
}

func TestChunkerMultipleFiles(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename:  "a.py",
			Classes:   []analyzer.ClassInfo{{Name: "A"}},
			Functions: []analyzer.FunctionInfo{{Name: "fa"}},
			Imports:   []analyzer.ImportInfo{{Module: "os"}},
		},
		{
			Filename:  "b.py",
			Classes:   []analyzer.ClassInfo{{Name: "B"}},
			Functions: []analyzer.FunctionInfo{{Name: "fb"}},
			Imports:   []analyzer.ImportInfo{{Module: "sys"}},
		},
	}

	c := New()
	chunks := c.ChunkFiles(files)

	assert.Len(t, chunks, 6)

	ids := make([]string, len(chunks))
	for i, ch := range chunks {
		ids[i] = ch.ID
	}
	assert.Contains(t, ids, "a.py#imports")
	assert.Contains(t, ids, "a.py#A")
	assert.Contains(t, ids, "a.py#fa")
	assert.Contains(t, ids, "b.py#imports")
	assert.Contains(t, ids, "b.py#B")
	assert.Contains(t, ids, "b.py#fb")
}

func TestChunkerClassWithMultipleMethods(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "service.py",
			Classes: []analyzer.ClassInfo{
				{
					Name:    "UserService",
					Bases:   []string{"BaseService"},
					Methods: []analyzer.FunctionInfo{
						{Name: "get_user", Params: []string{"self", "id"}, ReturnType: "User"},
						{Name: "list_users", Params: []string{"self"}, ReturnType: "List[User]"},
						{Name: "delete_user", Params: []string{"self", "id"}, ReturnType: "bool"},
					},
					Decorators: []string{"singleton"},
				},
			},
		},
	}

	c := New()
	chunks := c.ChunkFiles(files)

	assert.Len(t, chunks, 1)
	clsChunk := chunks[0]
	assert.Equal(t, TypeClass, clsChunk.Type)
	assert.Contains(t, clsChunk.Content, "Class UserService extends BaseService [decorators: singleton]")
	assert.Contains(t, clsChunk.Content, "get_user(self, id) -> User")
	assert.Contains(t, clsChunk.Content, "list_users(self) -> List[User]")
	assert.Contains(t, clsChunk.Content, "delete_user(self, id) -> bool")
}

func TestChunkerFunctionWithDecorators(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "utils.py",
			Functions: []analyzer.FunctionInfo{
				{Name: "cached_add", Params: []string{"a", "b"}, ReturnType: "int", Decorators: []string{"lru_cache"}},
			},
		},
	}

	c := New()
	chunks := c.ChunkFiles(files)

	assert.Len(t, chunks, 1)
	fnChunk := chunks[0]
	assert.Equal(t, TypeFunction, fnChunk.Type)
	assert.Contains(t, fnChunk.Content, "Function cached_add(a, b) -> int [decorators: lru_cache]")
}

func TestChunkerOnlyImports(t *testing.T) {
	files := []*analyzer.FileResult{
		{
			Filename: "__init__.py",
			Imports: []analyzer.ImportInfo{
				{Module: "models.user", Name: "User"},
				{Module: "models.order", Name: "Order"},
			},
		},
	}

	c := New()
	chunks := c.ChunkFiles(files)

	assert.Len(t, chunks, 1)
	assert.Equal(t, TypeImport, chunks[0].Type)
	assert.Contains(t, chunks[0].Content, "User from models.user")
	assert.Contains(t, chunks[0].Content, "Order from models.order")
}
