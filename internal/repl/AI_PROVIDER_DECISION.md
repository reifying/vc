# Why REPL Doesn't Use ai.AIProvider

## Summary

The REPL's `ConversationHandler` uses the Anthropic SDK directly instead of the `ai.AIProvider` abstraction. This is **intentional** and should not be considered a bug or inconsistency.

## Background

The `ai.AIProvider` interface was introduced to abstract AI model invocation, allowing VC to switch between:
- Anthropic API (via SDK)
- Claude CLI (via command-line tool)
- Future providers (OpenAI, local models, etc.)

This abstraction is successfully used by:
- `ai.Supervisor.AssessIssue()` - Pre-execution assessment
- `ai.Supervisor.AnalyzeExecutionResult()` - Post-execution analysis
- `ai.Supervisor.ReviewCode()` - Code quality analysis
- `ai.Supervisor.GenerateRecoveryStrategy()` - Quality gate failure recovery

These operations follow a simple pattern:
```
User Prompt → AI → Text Response
```

## Why REPL is Different

The REPL's conversational interface uses **function calling** (also called tool use), which is a fundamentally different interaction pattern:

```
User Message → AI → Tool Call(s) → Execute Tools → Return Results → AI → Final Response
```

### Function Calling Requirements

1. **Tool Definitions**: Define custom tools with JSON schemas
   ```go
   tools := []anthropic.ToolParam{
       {
           Name: "create_issue",
           Description: "Create a new issue",
           InputSchema: {...},
       },
       {
           Name: "continue_execution",
           Description: "Execute an issue",
           InputSchema: {...},
       },
       // ... 11 total tools
   }
   ```

2. **Multi-Turn Conversations**: Loop until AI returns text instead of tool calls
   ```go
   for iteration := 0; iteration < MaxConversationIterations; iteration++ {
       response := client.Messages.New(ctx, MessageParams{
           Messages: history,
           Tools: tools,
       })

       if response.StopReason == "tool_use" {
           // Execute tools and add results to history
           // Continue loop
       } else {
           // Return final text response
           break
       }
   }
   ```

3. **Tool Execution**: Process tool use blocks and return structured results
   ```go
   for _, block := range response.Content {
       if toolUse := block.AsToolUse(); toolUse != nil {
           result := executeTool(toolUse.Name, toolUse.Input)
           toolResults = append(toolResults, NewToolResultBlock(toolUse.ID, result))
       }
   }
   ```

4. **Conversation History**: Maintain state across multiple turns
   ```go
   type ConversationHandler struct {
       history []anthropic.MessageParam
       // ...
   }
   ```

### Current AIProvider Interface

```go
type AIProvider interface {
    Invoke(ctx context.Context, params InvokeParams) (*InvokeResult, error)
}

type InvokeParams struct {
    Prompt    string
    MaxTokens int
    Model     string
}

type InvokeResult struct {
    Text         string
    InputTokens  int
    OutputTokens int
}
```

This interface is designed for simple prompt/response operations and does **not** support:
- Tool definitions
- Multi-turn conversations
- Tool execution callbacks
- Response parsing for tool use blocks

## Why Claude CLI Doesn't Help

You might think: "Just add function calling to the CLI provider!" But there are fundamental issues:

1. **CLI doesn't support custom function calling**: The `--tools` flag in Claude CLI refers to built-in tools (Bash, Edit, Read, Glob, Grep, etc.), not custom business logic tools.

2. **VC tools are Go code**: The REPL's tools (`create_issue`, `continue_execution`, `search_issues`, etc.) are implemented in Go and interact with VC's storage layer. They can't be executed by the Claude CLI.

3. **Conversation state**: Claude CLI is stateless - each invocation is independent. Function calling requires maintaining conversation history across multiple turns.

## Options Considered

### Option 1: Extend AIProvider to Support Function Calling ❌

**Pros:**
- Consistent interface across all AI operations

**Cons:**
- Significant complexity - would need to abstract:
  - Tool definition schemas
  - Multi-turn conversation loops
  - Tool execution callbacks
  - Response parsing
- Only works with Anthropic API (CLI doesn't support custom function calling)
- Would add complexity for a single use case (REPL)
- Assessment/analysis don't need this complexity

**Verdict:** Over-engineering. The abstraction would be API-specific anyway.

### Option 2: Keep REPL Using Direct Anthropic Client ✅ (CHOSEN)

**Pros:**
- Simple and clear
- No abstraction overhead for a specialized use case
- REPL is user-facing and interactive (caching benefits of CLI less critical)
- Function calling is inherently API-specific
- Doesn't complicate the AIProvider interface for other use cases

**Cons:**
- REPL can't use Claude CLI provider
- Some code duplication (but minimal - just the client initialization)

**Verdict:** Pragmatic choice. REPL needs API anyway for function calling.

### Option 3: Create Separate ToolCallProvider Interface ❌

**Pros:**
- Abstraction specifically for function calling
- Could theoretically support multiple backends

**Cons:**
- Same issues as Option 1 (CLI doesn't support custom function calling)
- Adds interface complexity for no practical benefit
- Would need to design the abstraction speculatively

**Verdict:** YAGNI (You Aren't Gonna Need It). Build it when there's a second implementation.

## Decision

**Keep ConversationHandler using Anthropic SDK directly.**

Rationale:
1. Function calling is API-specific (not available in Claude CLI)
2. REPL is user-facing and interactive (API latency acceptable, caching less critical)
3. Abstracting function calling adds significant complexity for limited benefit
4. Other Supervisor operations (assessment, analysis) already benefit from AIProvider
5. Pragmatic over perfect - solve real problems, not theoretical ones

## Future Considerations

If we need to support multiple backends for function calling in the future:

1. **Wait for a real use case**: Don't design the abstraction until we have a second implementation to learn from

2. **Possible approaches**:
   - **ToolCallProvider interface**: Separate interface specifically for function calling
   - **Extended AIProvider**: Add optional methods for tool calling (Go interfaces can be extended)
   - **Hybrid approach**: Keep simple prompt/response in AIProvider, create ToolCallProvider for advanced use cases

3. **Alternative backends**:
   - OpenAI API (supports function calling)
   - Anthropic API (current)
   - Local models via LiteLLM or similar (some support function calling)
   - Claude CLI + custom tool bridge (would require significant engineering)

## Examples

### Simple Prompt/Response (uses AIProvider)

```go
// Assessment operation
result, err := provider.Invoke(ctx, InvokeParams{
    Prompt: "Analyze this issue and create a plan...",
    MaxTokens: 4096,
})
// Returns: text, tokens, error
```

### Function Calling (uses Anthropic SDK directly)

```go
// REPL conversation
response, err := client.Messages.New(ctx, MessageParams{
    Messages: history,
    Tools: []ToolParam{
        {Name: "create_issue", InputSchema: {...}},
        {Name: "continue_execution", InputSchema: {...}},
    },
})

if response.StopReason == "tool_use" {
    // Process tool calls
    for _, block := range response.Content {
        if toolUse := block.AsToolUse(); toolUse != nil {
            result := executeTool(toolUse.Name, toolUse.Input)
            toolResults = append(toolResults, result)
        }
    }
    // Add tool results to history and continue conversation
}
```

## Related Files

- `internal/repl/conversation.go` - ConversationHandler implementation
- `internal/ai/provider.go` - AIProvider interface definition
- `internal/ai/supervisor.go` - Supervisor using AIProvider
- `internal/ai/assessment.go` - Assessment using provider.Invoke()
- `internal/ai/analysis.go` - Analysis using provider.Invoke()

## Conclusion

The REPL's use of the Anthropic SDK directly is **intentional and correct**. It reflects a pragmatic engineering decision that:

1. Keeps the codebase simple
2. Uses the right tool for the job (API for function calling, provider abstraction for simple operations)
3. Avoids premature abstraction
4. Solves real problems (not theoretical ones)

Don't "fix" this unless there's a concrete requirement for multiple function calling backends.
