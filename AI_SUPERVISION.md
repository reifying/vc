# AI Supervision & Gate Recovery Investigation

## What We Currently Have (Without AI Supervision)

### Quality Gates - Fallback Behavior
**Location**: `internal/gates/gates.go:380-410`

**Current behavior** (without `ANTHROPIC_API_KEY`):
1. Each failed gate → Creates a blocking issue
2. Original issue → Marked as `blocked`
3. Blocking dependencies → Added automatically
4. Comment added → "Quality gates failed. Created N blocking issue(s)"

**Example** (from our test with vc-5):
- vc-5 fails test & lint gates
- Creates: `vc-5-gate-test` and `vc-5-gate-lint`
- vc-5 status → `blocked`
- vc-5 depends on both gate issues

This is **hardcoded, one-size-fits-all logic** - always creates blocking issues.

---

## What AI Supervision Adds

### AI-Powered Recovery Strategy
**Location**: `internal/ai/recovery.go`

**How it works**:
1. **Analyzes context** using Claude API:
   - Issue priority, type, description
   - Which gates failed and why
   - Failure severity
   - Output/error details

2. **AI determines action** (not hardcoded):
   - `fix_in_place` - Create blocking issues (like fallback)
   - `acceptable_failure` - Close anyway if non-critical
   - `split_work` - Create separate issues, close original
   - `escalate` - Flag for human review
   - `retry` - Suggest retry for flaky tests

3. **Provides reasoning**:
   - Detailed explanation of decision
   - Confidence score (0.0-1.0)
   - Specific comment to add to issue

4. **Nuanced decisions**:
   - Flaky test failures → `acceptable_failure` or `retry`
   - Lint warnings on chore → `acceptable_failure`
   - Build failures → `fix_in_place`
   - Critical P0 bugs → `fix_in_place`

### Comparison Table

| Scenario | Without AI (Fallback) | With AI Supervision |
|----------|----------------------|---------------------|
| Test fails on P2 chore | Creates blocking issue | Might accept failure if non-critical |
| Flaky test intermittent | Creates blocking issue | Suggests retry or acceptable |
| Lint warning in comment | Creates blocking issue | Likely accepts if minor |
| Build failure | Creates blocking issue | Creates blocking issue (critical) |
| Test fails on P0 feature | Creates blocking issue | Creates blocking issue (critical) |

**Key Difference**: AI **considers context and severity**, not just pass/fail.

---

## Requirements for AI Supervision

### Minimal Setup
```bash
export ANTHROPIC_API_KEY=your-key-here
./vc execute
```

**That's it!** The executor automatically:
1. Detects API key at startup
2. Initializes AI supervisor
3. Uses AI for recovery decisions

### What Changes with AI Enabled

**Before gate execution** (line 565-599 in executor.go):
- Without AI: Skips assessment, goes to execution
- With AI: Calls `supervisor.AssessIssue()` to analyze and plan

**After gate failures** (line 300-308 in gates.go):
- Without AI: `handleGateResultsFallback()` - creates blocking issues
- With AI: `handleGateResultsWithAI()` - asks Claude what to do

**Example AI Recovery Decision**:
```json
{
  "action": "acceptable_failure",
  "reasoning": "This is a P2 documentation task. The lint warnings are about comment formatting, which doesn't affect functionality. The core documentation was successfully added.",
  "confidence": 0.9,
  "create_issues": [],
  "mark_as_blocked": false,
  "close_original": true,
  "add_comment": "Closing as complete despite minor lint warnings. Formatting can be addressed separately if needed.",
  "requires_approval": false
}
```

vs. Fallback behavior:
```
Quality gates failed. Created 1 blocking issue(s): vc-5-gate-lint
```

---

## Cost Considerations

### API Usage for Recovery

**Per gate failure analysis**:
- Input tokens: ~500-1000 (issue context + gate output)
- Output tokens: ~200-500 (JSON strategy)
- Cost: ~$0.01-0.02 per failure
- Model: claude-sonnet-4-5-20250929

**Frequency**:
- Only called when gates fail
- Typical: 0-5 times per day for small projects
- Cost: $0-0.10/day

### Hybrid Billing Approach

**Recommended setup**:
```bash
# Supervisor uses API (small, infrequent prompts)
export ANTHROPIC_API_KEY=sk-ant-...

# Agents use Max plan (large, frequent prompts)
# Filter API key from agent environment
```

**Why this works**:
- AI supervision: Small prompts, smart decisions → API billing
- Agent execution: Large prompts, heavy work → Max plan (unlimited)
- Total cost: API key for supervision only (~$1-5/month)

---

## Testing Without API Key

We've already tested the fallback behavior successfully:

✅ **Test vc-5**: "Create hello.txt file"
- Gates ran: build PASS, test FAIL, lint FAIL
- Fallback behavior: Created `vc-5-gate-test` and `vc-5-gate-lint`
- Original issue: Marked `blocked`
- Result: Works correctly, but creates blocking issues for everything

**With AI**, the same test might have:
- Recognized it's a simple file creation task
- Determined lint/test failures are acceptable for this context
- Closed vc-5 as complete
- Created optional cleanup issues instead of blockers

---

## Implementation Status

### What's Already Implemented

✅ **AI Supervisor** (`internal/ai/supervisor.go`)
- Full recovery strategy generation
- Retry logic with circuit breaker
- Concurrency limiting
- Token usage logging

✅ **Integration Points** (`internal/gates/gates.go`)
- `handleGateResultsWithAI()` - AI path
- `handleGateResultsFallback()` - Non-AI path
- Graceful fallback if AI fails

✅ **Recovery Actions** (`internal/gates/gates.go:412-490+`)
- `executeFixInPlace()` - Create blocking issues
- `executeAcceptableFailure()` - Close anyway
- `executeSplitWork()` - Separate fix issues
- `executeEscalate()` - Human review
- `executeRetry()` - Retry logic

### What's Required to Enable

**Nothing!** Just set the environment variable:
```bash
export ANTHROPIC_API_KEY=sk-ant-...
./vc execute
```

The code is **already fully implemented and ready to use**.

---

## Next Steps

### Option 1: Test with API Key (if available)
1. Set `ANTHROPIC_API_KEY`
2. Create test issue that fails gates
3. Observe AI recovery decision
4. Compare with fallback behavior

### Option 2: Document for Future Use
1. Add to CLAUDE_CODE_MAX_INTEGRATION.md
2. Explain hybrid billing setup
3. Provide examples of AI decisions
4. Note cost estimates

### Option 3: Environment Filtering (Max Plan Hybrid)
1. Add environment filtering to agent spawn
2. Ensure agents don't see ANTHROPIC_API_KEY
3. Test that supervisor uses API, agents use Max
4. Document hybrid setup

---

## Recommendation

**Start without API key** (current state):
- Agents work perfectly with Max plan
- Fallback gate recovery is functional
- No additional cost

**Add API key later** when:
- You want smarter gate recovery decisions
- You want to test AI supervision features
- You're comfortable with ~$1-5/month API cost

The system is designed to **work great both ways** - with AI for smarter decisions, or without AI using solid fallback logic.
