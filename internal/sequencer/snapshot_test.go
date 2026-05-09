package sequencer

import (
	"path/filepath"
	"testing"

	"github.com/splitsword/fine-codewiki/internal/testutil"
	"github.com/stretchr/testify/require"
)

// TestPythonBasicSequenceDiagramSnapshot generates a sequence diagram for the
// python-basic test repo and compares it against a stored snapshot.
// Run with -update to refresh the snapshot after intentional changes.
func TestPythonBasicSequenceDiagramSnapshot(t *testing.T) {
	// BuildCallGraph needs a source dir, but for this synthetic test we don't have real source files.
	// We'll construct edges manually to match the expected call patterns.
	edges := []CallEdge{
		{From: FunctionRef{Module: "main", Name: "main"}, To: FunctionRef{Module: "services/user_service", Name: "UserService.register"}},
		{From: FunctionRef{Module: "services/user_service", Name: "UserService.register"}, To: FunctionRef{Module: "models/user", Name: "User.create"}},
		{From: FunctionRef{Module: "services/user_service", Name: "UserService.authenticate"}, To: FunctionRef{Module: "models/user", Name: "User.authenticate"}},
		{From: FunctionRef{Module: "services/user_service", Name: "UserService.list_users"}, To: FunctionRef{Module: "repositories/user_repository", Name: "UserRepository.find_all"}},
		{From: FunctionRef{Module: "models/user", Name: "User.create"}, To: FunctionRef{Module: "utils/crypto", Name: "hash_password"}},
	}

	sequences := FindSequences(edges, 2)
	require.NotEmpty(t, sequences, "should find at least one sequence")

	seqDSL := GenerateSequenceDiagram(sequences[0])
	testutil.SnapshotCompare(t, seqDSL, filepath.Join("..", "..", "testdata", "expected", "diagrams", "python-basic", "sequence-diagram.mmd"))
}
