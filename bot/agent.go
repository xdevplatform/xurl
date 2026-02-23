package bot

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// AgentResult holds the outcome of a coding agent run.
type AgentResult struct {
	Success bool
	PRLink  string
	Output  string
	Error   string
}

// Agent is the interface for coding agents that fix bugs.
type Agent interface {
	Name() string
	Run(ctx context.Context, bugDesc string, mediaFiles []string, repo string, branchName string) (*AgentResult, error)
}

// NewAgent creates an Agent based on the agent type string.
func NewAgent(agentType string, customCmd string) (Agent, error) {
	switch strings.ToLower(agentType) {
	case "claude":
		return &ClaudeAgent{}, nil
	case "codex":
		return &CodexAgent{}, nil
	case "gemini":
		return &GeminiAgent{}, nil
	case "custom":
		if customCmd == "" {
			return nil, fmt.Errorf("custom agent requires agent_cmd to be set")
		}
		return &CustomAgent{CmdTemplate: customCmd}, nil
	default:
		return nil, fmt.Errorf("unknown agent type: %s (supported: claude, codex, gemini, custom)", agentType)
	}
}

// maxSkillFileSize is the maximum size of .xurl-bot.md (10KB).
const maxSkillFileSize = 10 * 1024

// loadSkillFile reads .xurl-bot.md from the repo root if it exists.
// H3: Capped at maxSkillFileSize to prevent memory abuse.
func loadSkillFile(repo string) string {
	path := filepath.Join(repo, ".xurl-bot.md")
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return ""
	}
	if info.Size() > maxSkillFileSize {
		return "" // Silently skip oversized skill files
	}

	data := make([]byte, info.Size())
	n, err := f.Read(data)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data[:n]))
}

// buildPrompt constructs the prompt sent to the coding agent.
// If a .xurl-bot.md file exists in the repo, it's used as the base instructions.
// Otherwise, a default prompt template is used.
func buildPrompt(bugDesc string, founderNote string, mediaFiles []string, branchName string, repo string) string {
	var sb strings.Builder

	// Check for repo-specific skill file
	skill := loadSkillFile(repo)

	if skill != "" {
		sb.WriteString(skill)
		sb.WriteString("\n\n---\n\n")
	}

	sb.WriteString(fmt.Sprintf("Bug report: %s\n", bugDesc))

	if founderNote != "" && founderNote != bugDesc {
		sb.WriteString(fmt.Sprintf("\nFounder's note: %s\n", founderNote))
	}

	if len(mediaFiles) > 0 {
		sb.WriteString(fmt.Sprintf("\nScreenshots saved at: %s\n", strings.Join(mediaFiles, ", ")))
	}

	// Only add default instructions if no skill file provides them
	if skill == "" {
		sb.WriteString("\nYou are a bug fix bot. A user reported this bug on X (Twitter).\n")
	}

	sb.WriteString(fmt.Sprintf(`
Instructions:
1. Create a new git branch named '%s'
2. Investigate and fix the bug
3. Commit the fix with a clear message
4. Push the branch and create a pull request
5. Print the PR URL on the last line of output
`, branchName))

	return sb.String()
}

// prLinkRegex matches GitHub/GitLab PR URLs.
var prLinkRegex = regexp.MustCompile(`https?://[^\s]+/pull/\d+`)

// extractPRLink finds a PR URL in agent output.
func extractPRLink(output string) string {
	matches := prLinkRegex.FindAllString(output, -1)
	if len(matches) == 0 {
		return ""
	}
	// Return the last match (most likely the final PR link)
	return matches[len(matches)-1]
}

// ─── Claude Code Agent ──────────────────────────────────────────

// ClaudeAgent uses the Claude Code CLI (claude -p).
type ClaudeAgent struct{}

func (a *ClaudeAgent) Name() string { return "claude" }

func (a *ClaudeAgent) Run(ctx context.Context, bugDesc string, mediaFiles []string, repo string, branchName string) (*AgentResult, error) {
	prompt := buildPrompt(bugDesc, "", mediaFiles, branchName, repo)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude",
		"-p", prompt,
		"--allowedTools", "Edit,Write,Bash,Read,Grep,Glob",
	)
	cmd.Dir = repo

	output, err := cmd.CombinedOutput()
	outStr := string(output)

	prLink := extractPRLink(outStr)

	result := &AgentResult{
		Success: err == nil,
		PRLink:  prLink,
		Output:  outStr,
	}
	if err != nil {
		result.Error = err.Error()
	}

	return result, err
}

// ─── Codex Agent ────────────────────────────────────────────────

// CodexAgent uses the OpenAI Codex CLI.
type CodexAgent struct{}

func (a *CodexAgent) Name() string { return "codex" }

func (a *CodexAgent) Run(ctx context.Context, bugDesc string, mediaFiles []string, repo string, branchName string) (*AgentResult, error) {
	prompt := buildPrompt(bugDesc, "", mediaFiles, branchName, repo)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "codex",
		"--approval-mode", "full-auto",
		"-q", prompt,
	)
	cmd.Dir = repo

	output, err := cmd.CombinedOutput()
	outStr := string(output)
	prLink := extractPRLink(outStr)

	result := &AgentResult{
		Success: err == nil,
		PRLink:  prLink,
		Output:  outStr,
	}
	if err != nil {
		result.Error = err.Error()
	}
	return result, err
}

// ─── Gemini Agent ───────────────────────────────────────────────

// GeminiAgent uses Google's Gemini CLI.
type GeminiAgent struct{}

func (a *GeminiAgent) Name() string { return "gemini" }

func (a *GeminiAgent) Run(ctx context.Context, bugDesc string, mediaFiles []string, repo string, branchName string) (*AgentResult, error) {
	prompt := buildPrompt(bugDesc, "", mediaFiles, branchName, repo)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gemini",
		"-p", prompt,
	)
	cmd.Dir = repo

	output, err := cmd.CombinedOutput()
	outStr := string(output)
	prLink := extractPRLink(outStr)

	result := &AgentResult{
		Success: err == nil,
		PRLink:  prLink,
		Output:  outStr,
	}
	if err != nil {
		result.Error = err.Error()
	}
	return result, err
}

// ─── Custom Agent ───────────────────────────────────────────────

// CustomAgent runs a user-specified command.
type CustomAgent struct {
	CmdTemplate string
}

func (a *CustomAgent) Name() string { return "custom" }

func (a *CustomAgent) Run(ctx context.Context, bugDesc string, mediaFiles []string, repo string, branchName string) (*AgentResult, error) {
	prompt := buildPrompt(bugDesc, "", mediaFiles, branchName, repo)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	// C1: Parse command template into executable + args, pass prompt via env var
	// instead of sh -c to prevent command injection.
	parts := strings.Fields(a.CmdTemplate)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty agent command")
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "XURL_BOT_PROMPT="+prompt)
	cmd.Stdin = strings.NewReader(prompt)

	output, err := cmd.CombinedOutput()
	outStr := string(output)
	prLink := extractPRLink(outStr)

	result := &AgentResult{
		Success: err == nil,
		PRLink:  prLink,
		Output:  outStr,
	}
	if err != nil {
		result.Error = err.Error()
	}
	return result, err
}
