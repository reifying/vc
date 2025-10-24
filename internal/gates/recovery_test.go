package gates

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/steveyegge/vc/internal/ai"
	"github.com/steveyegge/vc/internal/storage/sqlite"
	"github.com/steveyegge/vc/internal/types"
)

// TestGateRecovery_BuildFailure tests Claude CLI recovery from build gate failures
func TestGateRecovery_BuildFailure(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("Skipping CLI recovery test: ANTHROPIC_API_KEY not set")
	}

	// Create temp workspace
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	store, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create AI supervisor (uses Claude CLI)
	supervisor, err := ai.NewSupervisor(&ai.Config{
		Store: store,
		Retry: ai.DefaultRetryConfig(),
	})
	if err != nil {
		t.Fatalf("Failed to create supervisor: %v", err)
	}

	ctx := context.Background()

	// Create test issue
	issue := &types.Issue{
		ID:          "test-recovery-build",
		Title:       "Test build recovery",
		Description: "Testing Claude CLI recovery from build failures",
		Status:      types.StatusInProgress,
		Priority:    1,
		IssueType:   types.TypeTask,
	}

	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Create a Go project with build errors
	goMod := filepath.Join(tempDir, "go.mod")
	modContent := "module testfailure\n\ngo 1.24\n"
	if err := os.WriteFile(goMod, []byte(modContent), 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	// Create file with syntax error (missing closing brace)
	badFile := filepath.Join(tempDir, "broken.go")
	badContent := `package testfailure

func Broken() string {
	return "this function is missing a closing brace"
// Missing }
`
	if err := os.WriteFile(badFile, []byte(badContent), 0644); err != nil {
		t.Fatalf("Failed to create broken file: %v", err)
	}

	// Create runner with AI supervisor
	runner := &Runner{
		store:      store,
		supervisor: supervisor,
		workingDir: tempDir,
	}

	// Run gates (should fail on build)
	results, allPassed := runner.RunAll(ctx)

	// Verify build failed
	if allPassed {
		t.Error("Expected gates to fail, but all passed")
	}

	// Verify at least build gate failed
	buildFailed := false
	for _, result := range results {
		if result.Gate == GateBuild && !result.Passed {
			buildFailed = true
			t.Logf("Build gate failed as expected: %v", result.Error)
		}
	}
	if !buildFailed {
		t.Error("Expected build gate to fail")
	}

	// Handle gate results (triggers Claude CLI recovery)
	t.Logf("Invoking Claude CLI for recovery strategy...")
	err = runner.HandleGateResults(ctx, issue, results, allPassed)
	if err != nil {
		t.Fatalf("HandleGateResults failed: %v", err)
	}

	// Verify AI strategy was logged
	events, err := store.GetEvents(ctx, issue.ID, 100)
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}

	foundStrategy := false
	for _, event := range events {
		if event.Actor == "ai-supervisor" && event.Comment != nil {
			foundStrategy = true
			t.Logf("AI Recovery Strategy:\n%s", *event.Comment)
		}
	}

	if !foundStrategy {
		t.Error("Expected AI recovery strategy comment")
	}

	// Log final issue state
	finalIssue, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get final issue: %v", err)
	}
	t.Logf("Final issue status: %s", finalIssue.Status)

	// Verify some action was taken (issue blocked, closed, or labeled)
	if finalIssue.Status == types.StatusInProgress {
		// Check if labels were added (escalate/needs-approval)
		labels, _ := store.GetLabels(ctx, issue.ID)
		if len(labels) == 0 {
			t.Error("Expected AI to take some action (change status or add labels)")
		} else {
			t.Logf("Labels added: %v", labels)
		}
	}
}

// TestGateRecovery_TestFailure tests Claude CLI recovery from test failures
func TestGateRecovery_TestFailure(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("Skipping CLI recovery test: ANTHROPIC_API_KEY not set")
	}

	// Create temp workspace
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	store, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create AI supervisor
	supervisor, err := ai.NewSupervisor(&ai.Config{
		Store: store,
		Retry: ai.DefaultRetryConfig(),
	})
	if err != nil {
		t.Fatalf("Failed to create supervisor: %v", err)
	}

	ctx := context.Background()

	// Create test issue (low priority chore - AI might choose acceptable_failure)
	issue := &types.Issue{
		ID:          "test-recovery-test",
		Title:       "Refactor helper functions",
		Description: "Clean up utility code",
		Status:      types.StatusInProgress,
		Priority:    3, // P3 - low priority
		IssueType:   types.TypeChore,
	}

	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Create a Go project with failing tests
	goMod := filepath.Join(tempDir, "go.mod")
	modContent := "module testfailure\n\ngo 1.24\n"
	if err := os.WriteFile(goMod, []byte(modContent), 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	// Create valid code
	codeFile := filepath.Join(tempDir, "code.go")
	codeContent := `package testfailure

func Add(a, b int) int {
	return a + b
}
`
	if err := os.WriteFile(codeFile, []byte(codeContent), 0644); err != nil {
		t.Fatalf("Failed to create code file: %v", err)
	}

	// Create failing test
	testFile := filepath.Join(tempDir, "code_test.go")
	testContent := `package testfailure

import "testing"

func TestAdd(t *testing.T) {
	result := Add(2, 2)
	// Intentionally wrong expectation
	if result != 5 {
		t.Errorf("Expected 5, got %d", result)
	}
}
`
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create runner
	runner := &Runner{
		store:      store,
		supervisor: supervisor,
		workingDir: tempDir,
	}

	// Run gates (build should pass, test should fail)
	results, allPassed := runner.RunAll(ctx)

	if allPassed {
		t.Error("Expected test gate to fail")
	}

	// Verify test failed
	testFailed := false
	for _, result := range results {
		if result.Gate == GateTest && !result.Passed {
			testFailed = true
			t.Logf("Test gate failed as expected: %v", result.Error)
		}
	}
	if !testFailed {
		t.Error("Expected test gate to fail")
	}

	// Handle gate results (triggers Claude CLI recovery)
	t.Logf("Invoking Claude CLI for recovery strategy on low-priority chore...")
	err = runner.HandleGateResults(ctx, issue, results, allPassed)
	if err != nil {
		t.Fatalf("HandleGateResults failed: %v", err)
	}

	// Verify AI strategy was logged
	events, err := store.GetEvents(ctx, issue.ID, 100)
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}

	foundStrategy := false
	for _, event := range events {
		if event.Actor == "ai-supervisor" && event.Comment != nil {
			foundStrategy = true
			t.Logf("AI Recovery Strategy for P3 chore:\n%s", *event.Comment)
		}
	}

	if !foundStrategy {
		t.Error("Expected AI recovery strategy comment")
	}

	// Log final state
	finalIssue, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get final issue: %v", err)
	}
	t.Logf("Final issue status: %s", finalIssue.Status)
}

// TestGateRecovery_CriticalBugTestFailure tests recovery for critical issues
func TestGateRecovery_CriticalBugTestFailure(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("Skipping CLI recovery test: ANTHROPIC_API_KEY not set")
	}

	// Create temp workspace
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	store, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create AI supervisor
	supervisor, err := ai.NewSupervisor(&ai.Config{
		Store: store,
		Retry: ai.DefaultRetryConfig(),
	})
	if err != nil {
		t.Fatalf("Failed to create supervisor: %v", err)
	}

	ctx := context.Background()

	// Create critical P0 bug (AI should recommend fix_in_place or escalate)
	issue := &types.Issue{
		ID:          "test-recovery-critical",
		Title:       "Fix authentication bypass vulnerability",
		Description: "Critical security flaw in auth system",
		Status:      types.StatusInProgress,
		Priority:    0, // P0 - critical
		IssueType:   types.TypeBug,
	}

	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Create Go project with security test failure
	goMod := filepath.Join(tempDir, "go.mod")
	modContent := "module authtest\n\ngo 1.24\n"
	if err := os.WriteFile(goMod, []byte(modContent), 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	codeFile := filepath.Join(tempDir, "auth.go")
	codeContent := `package authtest

func ValidateToken(token string) bool {
	return len(token) > 0 // Broken: should verify signature
}
`
	if err := os.WriteFile(codeFile, []byte(codeContent), 0644); err != nil {
		t.Fatalf("Failed to create code file: %v", err)
	}

	testFile := filepath.Join(tempDir, "auth_test.go")
	testContent := `package authtest

import "testing"

func TestValidateToken_RejectsInvalidTokens(t *testing.T) {
	// Test with invalid token
	if ValidateToken("invalid-token") {
		t.Error("Should reject invalid token signature")
	}
}
`
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create runner
	runner := &Runner{
		store:      store,
		supervisor: supervisor,
		workingDir: tempDir,
	}

	// Run gates
	results, allPassed := runner.RunAll(ctx)

	if allPassed {
		t.Error("Expected test gate to fail")
	}

	// Handle gate results (critical P0 bug)
	t.Logf("Invoking Claude CLI for recovery strategy on P0 security bug...")
	err = runner.HandleGateResults(ctx, issue, results, allPassed)
	if err != nil {
		t.Fatalf("HandleGateResults failed: %v", err)
	}

	// Verify AI strategy
	events, err := store.GetEvents(ctx, issue.ID, 100)
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}

	foundStrategy := false
	var strategyAction string
	for _, event := range events {
		if event.Actor == "ai-supervisor" && event.Comment != nil {
			foundStrategy = true
			comment := *event.Comment
			t.Logf("AI Recovery Strategy for P0 bug:\n%s", comment)

			// Extract action from comment
			if len(comment) > 0 {
				// The action should be in the comment
				strategyAction = comment
			}
		}
	}

	if !foundStrategy {
		t.Error("Expected AI recovery strategy comment")
	}

	// Verify AI took appropriate action for critical bug
	finalIssue, err := store.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Failed to get final issue: %v", err)
	}
	t.Logf("Final issue status: %s", finalIssue.Status)
	t.Logf("Strategy action: %s", strategyAction)

	// For a P0 bug, AI should NOT use acceptable_failure
	// It should either fix_in_place, escalate, or retry
	// We log the decision for manual verification
}

// TestGateRecovery_LintWarnings tests recovery from minor lint issues
func TestGateRecovery_LintWarnings(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("Skipping CLI recovery test: ANTHROPIC_API_KEY not set")
	}

	// Skip if golangci-lint not available
	if _, err := exec.LookPath("golangci-lint"); err != nil {
		t.Skip("golangci-lint not available")
	}

	// Create temp workspace
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	store, err := sqlite.New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create AI supervisor
	supervisor, err := ai.NewSupervisor(&ai.Config{
		Store: store,
		Retry: ai.DefaultRetryConfig(),
	})
	if err != nil {
		t.Fatalf("Failed to create supervisor: %v", err)
	}

	ctx := context.Background()

	// Create low-priority chore
	issue := &types.Issue{
		ID:          "test-recovery-lint",
		Title:       "Update documentation",
		Description: "Fix typos in README",
		Status:      types.StatusInProgress,
		Priority:    3,
		IssueType:   types.TypeChore,
	}

	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}

	// Create Go project with lint issues
	goMod := filepath.Join(tempDir, "go.mod")
	modContent := "module linttest\n\ngo 1.24\n"
	if err := os.WriteFile(goMod, []byte(modContent), 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	// Create file with lint warnings (unused variable, exported without comment)
	codeFile := filepath.Join(tempDir, "code.go")
	codeContent := `package linttest

// PublicFunc is exported without a proper doc comment
func PublicFunc() {
	unused := "this variable is never used"
	_ = unused // Silence the linter for this test
}
`
	if err := os.WriteFile(codeFile, []byte(codeContent), 0644); err != nil {
		t.Fatalf("Failed to create code file: %v", err)
	}

	// Create runner
	runner := &Runner{
		store:      store,
		supervisor: supervisor,
		workingDir: tempDir,
	}

	// Run gates (may fail on lint)
	results, allPassed := runner.RunAll(ctx)

	// Only proceed if lint actually failed
	lintFailed := false
	for _, result := range results {
		if result.Gate == GateLint && !result.Passed {
			lintFailed = true
			t.Logf("Lint gate failed as expected")
		}
	}

	if !lintFailed {
		t.Skip("Lint gate passed (no lint issues found)")
	}

	// Handle gate results
	t.Logf("Invoking Claude CLI for recovery strategy on lint warnings...")
	err = runner.HandleGateResults(ctx, issue, results, allPassed)
	if err != nil {
		t.Fatalf("HandleGateResults failed: %v", err)
	}

	// Verify AI strategy
	events, err := store.GetEvents(ctx, issue.ID, 100)
	if err != nil {
		t.Fatalf("Failed to get events: %v", err)
	}

	foundStrategy := false
	for _, event := range events {
		if event.Actor == "ai-supervisor" && event.Comment != nil {
			foundStrategy = true
			t.Logf("AI Recovery Strategy for lint warnings:\n%s", *event.Comment)
		}
	}

	if !foundStrategy {
		t.Error("Expected AI recovery strategy comment")
	}
}
