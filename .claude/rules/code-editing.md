## Code editing instructions

### Rule 201 : Code must be clean, concise, and well-structured to stay maintainable

#### Extracting functions from Update()
- **Strict limit**: If a case in `Update(msg tea.Msg)` exceeds **5 lines**, extract it into a separate function
- **Naming**: `handle[MessageType]` (e.g. `handleContextSwitch()`, `handleKeyPress()`)
- **Pattern**: The function must return `(tea.Model, tea.Cmd)`

**Before (bad):**
```go
case ContextSwitchCompleteMsg:
    a.config = msg.Config
    a.currentContext = msg.ContextName
    a.reinitializeViews(msg.Config)
    return a, func() tea.Msg {
        return tea.WindowSizeMsg{Width: a.width, Height: a.height}
    }
```

**After (good):**
```go
case ContextSwitchCompleteMsg:
    return a.handleContextSwitch(msg)

// Further down in the file
func (a *App) handleContextSwitch(msg ContextSwitchCompleteMsg) (tea.Model, tea.Cmd) {
    a.config = msg.Config
    a.currentContext = msg.ContextName
    a.reinitializeViews(msg.Config)
    return a, func() tea.Msg {
        return tea.WindowSizeMsg{Width: a.width, Height: a.height}
    }
}
```

#### Code reuse
- **Zero duplication**: If a block of code appears 2+ times, create a function
- **Modules**: Group related functions together (e.g. all context switching in a single file)
- **Constants**: Extract magic values into named constants

#### Error handling
- **Bubble Tea pattern**: Errors = messages (e.g. `ContextSwitchErrorMsg`)
- **Always log** in DEBUG mode: `log.Printf("ERROR: %v", err)`
- **Never ignore**: If the error is non-critical, document why

#### Comments
- **Mandatory for**:
  - Exported (public) functions
  - Complex business logic
  - Workarounds or non-obvious decisions
- **Format**: `// functionName does X and returns Y`
- **Avoid**: Comments that repeat the code

#### Complexity
- **Maximum 3 levels of indentation** in a function
- **Functions > 50 lines**: refactor into smaller functions
- **Switch > 10 cases**: consider a table-driven pattern
