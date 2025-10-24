# Claude Code Max Plan Integration - Investigation Summary

## Project Intent

**Goal**: Make VC (VibeCoder v2) work with Claude Code Max subscription instead of requiring ANTHROPIC_API_KEY for every agent execution.

**Motivation**:
- VC's AI supervision uses small prompts (cheap via API)
- VC's worker agents do heavy coding work (expensive via API, better suited for Max plan's fixed pricing)
- Hybrid approach: Use API key for AI supervisor, use Max subscription for agent execution
- Avoid double-billing: Claude Code shouldn't use API key when Max plan is available

**Related**: GitHub issue #1740 documents bug where Claude Code uses API key even when Max plan exists

## Current State Analysis

### What We Discovered by Running VC Stock

Following the "try it and see what breaks" approach, we found:

#### 1. **Beads Initialization Required**
- **Error**: `no .beads/*.db found`
- **Fix**: Run `vc init` to initialize tracker
- **Status**: Not a blocker - one-time setup

#### 2. **REPL Requires ANTHROPIC_API_KEY**
- **Error**: "AI conversation requires ANTHROPIC_API_KEY environment variable"
- **Impact**: Cannot use conversational REPL without API key
- **Workaround**: None - REPL fundamentally requires AI for conversation
- **Status**: Expected behavior, not a blocker for executor mode

#### 3. **Executor Skips "Assessing" State Without AI Supervision**
- **Error**: `invalid state transition: cannot transition from claimed to executing (valid transitions: [assessing failed])`
- **Root Cause**: `internal/executor/executor.go:565-568` only transitions to "assessing" when `e.enableAISupervision && e.supervisor != nil`
- **Impact**: Executor cannot process issues without API key
- **Fix Needed**: Modify state machine to allow `claimed → executing` when AI supervision is disabled
- **Code Location**: `internal/executor/executor.go:565-578`

#### 4. **Executor Uses Amp Instead of Claude Code**
- **Error**: `exec: "amp": executable file not found in $PATH`
- **Root Cause**: `internal/executor/executor.go:778` hardcodes `AgentTypeAmp`
- **Comment in Code**: "Use Amp for structured JSON events (vc-236)"
- **Impact**: Even if state machine was fixed, executor would fail trying to spawn amp
- **Fix Needed**: Change from `AgentTypeAmp` to `AgentTypeClaudeCode`
- **Code Location**: `internal/executor/executor.go:778`
- **Note**: Claude CLI is available at `~/.claude/local/claude`

## Key Technical Learnings

### Claude CLI Session Management (Critical Discovery)

**We initially misunderstood how Claude CLI session resumption works.** By examining working implementations in `active/claude-slack` and `active/voice-code`, we learned:

#### Correct Usage Patterns:

**1. Create New Session (Auto-Generated ID):**
```bash
claude --print --output-format json "<prompt>"
# Returns session_id in JSON response for future resumption
```

**2. Create New Session (Specific ID):**
```bash
claude --print --output-format json --session-id <UUID> "<prompt>"
# Uses your specified UUID as the session ID
```

**3. Resume Existing Session:**
```bash
claude --print --output-format json --resume <session-id> "<prompt>"
# Continues conversation from previous session
```

#### Key Distinctions:

- **`--session-id`**: Creates a NEW session with your specified UUID
- **`--resume`**: Continues an EXISTING session (requires valid session ID)
- **`--print`**: Non-interactive mode, returns JSON and exits (works perfectly with `--resume`)
- **`--output-format json`**: Returns structured array of JSON objects

#### Session Persistence:

- Sessions stored in: `~/.claude/projects/<project-path>/<session-id>.jsonl`
- Each line is a JSONL object (user messages, assistant responses, system events)
- Sessions are scoped to working directory/project
- `"isSidechain"` flag distinguishes warmup calls from main conversation

#### JSON Output Structure:

```json
[
  {
    "type": "system",
    "subtype": "init",
    "session_id": "...",
    "tools": [...],
    "model": "...",
    ...
  },
  {
    "type": "assistant",
    "message": {
      "content": [{"type": "text", "text": "..."}],
      ...
    },
    "session_id": "...",
    ...
  },
  {
    "type": "result",
    "subtype": "success",
    "is_error": false,
    "result": "actual response text",
    "session_id": "...",
    "total_cost_usd": 0.024,
    "usage": {...},
    ...
  }
]
```

#### Working Examples from Existing Projects:

**claude-slack** (`active/claude-slack/src/claude_slack_bot/claude/client.clj:27`):
```clojure
(let [args (cond-> ["--dangerously-skip-permissions"
                    "--output-format" "json"
                    "--model" model]
             session-id (concat ["--resume" session-id])
             true (concat [prompt]))
      shell-opts (if working-directory
                   [:dir working-directory]
                   [])]
  (apply shell/sh claude-cli-path (concat args shell-opts)))
```

**voice-code** (`active/voice-code/backend/src/voice_code/claude.clj:40-41`):
```clojure
(cond-> ["--dangerously-skip-permissions"
         "--output-format" "json"
         "--model" model]
  new-session-id (concat ["--session-id" new-session-id])
  resume-session-id (concat ["--resume" resume-session-id])
  true (concat [prompt]))
```

### VC Architecture Understanding

#### Agent Type Abstraction

VC supports multiple agent types via `internal/executor/agent.go`:

```go
type AgentType string

const (
    AgentTypeAmp        AgentType = "amp"          // Sourcegraph Amp
    AgentTypeClaudeCode AgentType = "claude-code"  // Claude Code CLI
)

type AgentConfig struct {
    Type        AgentType
    WorkingDir  string
    Issue       *types.Issue
    StreamJSON  bool
    Timeout     time.Duration
    Store       storage.Storage
    ExecutorID  string
    AgentID     string
    Sandbox     *sandbox.Sandbox
}
```

#### Agent Command Builders

Each agent type has a dedicated command builder:

**Amp** (`internal/executor/agent.go:511-537`):
```go
func buildAmpCommand(cfg AgentConfig, prompt string) *exec.Cmd {
    args := []string{}
    if cfg.StreamJSON {
        args = append(args, "--stream-json")
    }
    args = append(args, "--dangerously-skip-permissions")
    args = append(args, prompt)
    return exec.Command("amp", args...)
}
```

**Claude Code** (`internal/executor/agent.go:480-491`):
```go
func buildClaudeCodeCommand(cfg AgentConfig, prompt string) *exec.Cmd {
    args := []string{}
    args = append(args, "--dangerously-skip-permissions")
    args = append(args, prompt)
    return exec.Command("claude", args...)
}
```

**Current State**:
- `buildClaudeCodeCommand` exists but is MINIMAL (only adds `--dangerously-skip-permissions`)
- Missing: `--print`, `--output-format json`, `--model`, `--resume` support
- Missing: Environment filtering to prevent API key usage

#### Type-Based Dispatch

`SpawnAgent` function (`internal/executor/agent.go:96-119`) dispatches based on `AgentConfig.Type`:

```go
func SpawnAgent(ctx context.Context, cfg AgentConfig, prompt string) (*Agent, error) {
    var cmd *exec.Cmd
    switch cfg.Type {
    case AgentTypeAmp:
        cmd = buildAmpCommand(cfg, prompt)
    case AgentTypeClaudeCode:
        cmd = buildClaudeCodeCommand(cfg, prompt)
    default:
        return nil, fmt.Errorf("unknown agent type: %s", cfg.Type)
    }

    // Set working directory, start command, create agent...
}
```

#### Where Agents Are Spawned

**Executor** (`internal/executor/executor.go:778`):
```go
agentCfg := AgentConfig{
    Type:       AgentTypeAmp, // <-- HARDCODED to Amp
    WorkingDir: workingDir,
    Issue:      issue,
    StreamJSON: true,
    Timeout:    30 * time.Minute,
    Store:      e.store,
    ExecutorID: e.instanceID,
    AgentID:    agentID,
    Sandbox:    sb,
}
```

**REPL** (`internal/repl/conversation.go:1120`):
```go
agentCfg := executor.AgentConfig{
    Type:       executor.AgentTypeClaudeCode, // <-- Uses Claude Code
    WorkingDir: rootDir,
    Issue:      issue,
    Timeout:    30 * time.Minute,
}
```

**Key Insight**: REPL already uses Claude Code, but executor uses Amp!

### AI Supervision Architecture

**Supervisor** (`internal/ai/supervisor.go:54-59`):
```go
apiKey := cfg.APIKey
if apiKey == "" {
    apiKey = os.Getenv("ANTHROPIC_API_KEY")
    if apiKey == "" {
        return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
    }
}
```

**Executor Integration** (`internal/executor/executor.go:565-578`):
```go
if e.enableAISupervision && e.supervisor != nil {
    assessmentRan = true
    // Update execution state to assessing
    if err := e.store.UpdateExecutionState(ctx, issue.ID, types.ExecutionStateAssessing); err != nil {
        // ...error handling...
    }

    // AI assesses the issue and generates execution prompt
    assessment, err := e.supervisor.AssessIssue(ctx, issue, metadata)
    // ...
}
```

**When AI Supervision is Disabled**:
- Executor skips the "assessing" state
- Tries to go directly from "claimed" to "executing"
- State machine rejects this transition
- **This is the blocker we found**

## No True Blockers Found

### What Works Out of the Box:

✅ **Claude CLI `--print` mode works as a function call**
- Returns structured JSON
- Non-interactive execution
- Can be called from Go via `exec.Command`

✅ **Session resumption works with `--resume`**
- Tested and verified working
- Sessions persist across invocations
- Working directory scoping works correctly

✅ **Claude CLI available on system**
- Located at `~/.claude/local/claude`
- Accessible without API key (using Max plan)

✅ **VC architecture already supports Claude Code**
- `AgentTypeClaudeCode` exists
- `buildClaudeCodeCommand` exists (though minimal)
- REPL successfully uses Claude Code

### What Needs Changes:

1. **Executor must use `AgentTypeClaudeCode` instead of `AgentTypeAmp`**
   - One-line change at `internal/executor/executor.go:778`

2. **Enhance `buildClaudeCodeCommand` for Max plan usage**
   - Add `--print` flag
   - Add `--output-format json`
   - Add `--model` selection support
   - Filter environment to remove `ANTHROPIC_API_KEY` (force Max plan usage)

3. **Fix state machine to handle disabled AI supervision**
   - Allow `claimed → executing` transition when AI supervision is disabled
   - Or: Provide a default/no-op assessment when supervisor is nil

4. **Optional: Support session resumption in AgentConfig**
   - Add `SessionID` field to `AgentConfig`
   - Pass to `buildClaudeCodeCommand` for `--resume` flag
   - Useful for multi-turn agent conversations (not critical for MVP)

## Methodology: "Try It and Break It"

**Approach**: Instead of implementing changes based on code analysis, we:

1. ✅ Built VC from source (`go build`)
2. ✅ Ran `vc init` to initialize tracker
3. ✅ Tried REPL mode without API key → Found it requires API (expected)
4. ✅ Created test issue and ran `vc execute` → Found two real errors:
   - State machine transition error
   - Missing `amp` executable
5. ✅ Examined working projects (claude-slack, voice-code) for Claude CLI patterns
6. ✅ Tested Claude CLI session resumption ourselves
7. ✅ Verified `--print` + `--resume` compatibility

**Key Learning**: This pragmatic approach revealed:
- What actually breaks (vs. what we thought might break)
- Working examples from sibling projects
- That `--resume` works fine with `--print` (we initially got this wrong)

## Next Steps

### Minimal Changes Required:

1. **Change executor to use Claude Code**
   ```go
   // internal/executor/executor.go:778
   Type: AgentTypeClaudeCode, // was: AgentTypeAmp
   ```

2. **Enhance buildClaudeCodeCommand**
   ```go
   func buildClaudeCodeCommand(cfg AgentConfig, prompt string) *exec.Cmd {
       args := []string{
           "--print",
           "--output-format", "json",
           "--dangerously-skip-permissions",
       }

       // Add model selection if specified
       if cfg.Model != "" {
           args = append(args, "--model", cfg.Model)
       }

       args = append(args, prompt)

       cmd := exec.Command("claude", args...)

       // Filter environment to force Max plan usage
       cmd.Env = filterEnv(os.Environ(), []string{
           "ANTHROPIC_API_KEY",
       })

       return cmd
   }
   ```

3. **Fix state machine for no-AI-supervision mode**
   - Option A: Allow `claimed → executing` when supervisor is nil
   - Option B: Provide minimal assessment when supervisor is disabled

### Testing Plan:

1. Make minimal changes above
2. Run `vc execute` with test issue
3. Verify Claude Code is spawned (not amp)
4. Verify it completes without errors
5. Verify Max plan is used (not API key billing)

### Future Enhancements (Not Required for MVP):

- Add `SessionID` to `AgentConfig` for multi-turn conversations
- Add `--resume` support in `buildClaudeCodeCommand`
- Add environment variable to choose agent type (amp vs claude-code)
- Metrics/logging to track Max plan vs API usage

## Beads Issues Created

Epic `mono-20` with 11 child tasks (`mono-21` through `mono-31`) were created during initial planning phase. These provide comprehensive context for each change location.

**Status**: Issues created but implementation should wait until we verify minimal changes work first (following "try it and break it" methodology).

## References

- GitHub Issue #1740: Claude Code API key usage when Max plan exists
- VC Repository: `/Users/travisbrown/code/mono/active/vibe-code-reference/vc`
- Working Examples:
  - `active/claude-slack/src/claude_slack_bot/claude/client.clj`
  - `active/voice-code/backend/src/voice_code/claude.clj`
  - `active/voice-code/docs/claude-compact-testing.md`

## Conclusion

**No true blockers exist.** Claude CLI supports everything VC needs:
- Non-interactive execution via `--print`
- Structured JSON output via `--output-format json`
- Session resumption via `--resume`
- Works with Max plan when API key is not in environment

**Minimal changes required:**
- Switch executor from Amp to Claude Code (1 line)
- Enhance Claude Code command builder (~20 lines)
- Fix state machine for no-AI mode (~10 lines)

**Total estimated changes: ~30 lines across 2 files**

This is significantly simpler than our original analysis suggested, thanks to the pragmatic "try it and break it" approach revealing what actually matters vs. what we thought might matter.
