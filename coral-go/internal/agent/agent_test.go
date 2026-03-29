package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// findTempFile finds a Coral temp file matching the given prefix and session ID.
// Since writeTempFile now uses os.CreateTemp with random suffixes, we glob for matches.
func findTempFile(t *testing.T, prefix, sessionID, ext string) string {
	t.Helper()
	pattern := filepath.Join(os.TempDir(), "coral_"+prefix+"_"+sessionID+"_*."+ext)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob error: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("no temp file found matching %s", pattern)
	}
	return matches[len(matches)-1]
}

// ── Factory Tests ───────────────────────────────────────────

func TestGetAgent_Claude(t *testing.T) {
	a := GetAgent("claude")
	if a.AgentType() != "claude" {
		t.Errorf("expected claude, got %s", a.AgentType())
	}
}

func TestGetAgent_Gemini(t *testing.T) {
	a := GetAgent("gemini")
	if a.AgentType() != "gemini" {
		t.Errorf("expected gemini, got %s", a.AgentType())
	}
}

func TestGetAgent_Codex(t *testing.T) {
	a := GetAgent("codex")
	if a.AgentType() != "codex" {
		t.Errorf("expected codex, got %s", a.AgentType())
	}
}

func TestGetAgent_UnknownDefaultsToClaude(t *testing.T) {
	a := GetAgent("unknown-agent")
	if a.AgentType() != "claude" {
		t.Errorf("expected unknown to default to claude, got %s", a.AgentType())
	}
}

func TestGetAgent_EmptyDefaultsToClaude(t *testing.T) {
	a := GetAgent("")
	if a.AgentType() != "claude" {
		t.Errorf("expected empty to default to claude, got %s", a.AgentType())
	}
}

func TestGetAllAgents(t *testing.T) {
	agents := GetAllAgents()
	if len(agents) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(agents))
	}
	types := map[string]bool{}
	for _, a := range agents {
		types[a.AgentType()] = true
	}
	for _, expected := range []string{"claude", "gemini", "codex"} {
		if !types[expected] {
			t.Errorf("GetAllAgents() missing %s", expected)
		}
	}
}

// ── SupportsResume Tests ────────────────────────────────────

func TestSupportsResume(t *testing.T) {
	tests := []struct {
		agentType string
		expected  bool
	}{
		{"claude", true},
		{"codex", true},
		{"gemini", false},
	}
	for _, tt := range tests {
		a := GetAgent(tt.agentType)
		if a.SupportsResume() != tt.expected {
			t.Errorf("%s: SupportsResume() = %v, want %v", tt.agentType, a.SupportsResume(), tt.expected)
		}
	}
}

// ── HistoryGlobPattern Tests ────────────────────────────────

func TestHistoryGlobPattern(t *testing.T) {
	tests := []struct {
		agentType string
		expected  string
	}{
		{"claude", "*.jsonl"},
		{"codex", "rollout-*.jsonl"},
		{"gemini", "session-*.json"},
	}
	for _, tt := range tests {
		a := GetAgent(tt.agentType)
		if a.HistoryGlobPattern() != tt.expected {
			t.Errorf("%s: HistoryGlobPattern() = %q, want %q", tt.agentType, a.HistoryGlobPattern(), tt.expected)
		}
	}
}

// ── CLIInfo Tests ───────────────────────────────────────────

func TestGetCLIInfo(t *testing.T) {
	for _, agentType := range []string{"claude", "gemini", "codex"} {
		info := GetCLIInfo(agentType)
		if info == nil {
			t.Errorf("GetCLIInfo(%q) returned nil", agentType)
			continue
		}
		if info.Binary == "" {
			t.Errorf("GetCLIInfo(%q).Binary is empty", agentType)
		}
		if info.InstallCommand == "" {
			t.Errorf("GetCLIInfo(%q).InstallCommand is empty", agentType)
		}
	}
}

func TestGetCLIInfo_Unknown(t *testing.T) {
	info := GetCLIInfo("unknown")
	if info != nil {
		t.Errorf("GetCLIInfo(unknown) should return nil, got %+v", info)
	}
}

// ── GetCLIName Tests ────────────────────────────────────────

func TestGetCLIName(t *testing.T) {
	if name := GetCLIName(""); name != "coral-board" {
		t.Errorf("GetCLIName(\"\") = %q, want coral-board", name)
	}
	if name := GetCLIName("coral"); name != "coral-board" {
		t.Errorf("GetCLIName(coral) = %q, want coral-board", name)
	}
	if name := GetCLIName("unknown"); name != "coral-board" {
		t.Errorf("GetCLIName(unknown) = %q, want coral-board (default)", name)
	}
}

// ── Claude BuildLaunchCommand Tests ─────────────────────────

func TestClaude_BasicLaunch(t *testing.T) {
	a := &ClaudeAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		SessionID: "test-session-123",
	})
	if !strings.HasPrefix(cmd, "claude ") {
		t.Errorf("expected command to start with 'claude ', got %q", cmd)
	}
	if !strings.Contains(cmd, "--session-id test-session-123") {
		t.Errorf("expected --session-id, got %q", cmd)
	}
}

func TestClaude_Resume(t *testing.T) {
	a := &ClaudeAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		SessionID:       "test-session-123",
		ResumeSessionID: "resume-456",
	})
	if !strings.Contains(cmd, "--resume resume-456") {
		t.Errorf("expected --resume flag, got %q", cmd)
	}
}

func TestClaude_WithFlags(t *testing.T) {
	a := &ClaudeAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		SessionID: "s1",
		Flags:     []string{"--verbose", "--model", "opus"},
	})
	if !strings.Contains(cmd, "--verbose") || !strings.Contains(cmd, "--model opus") {
		t.Errorf("expected flags, got %q", cmd)
	}
}

func TestClaude_WithBoardWorker(t *testing.T) {
	a := &ClaudeAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		SessionID: "claude-board-w1",
		BoardName: "test-board",
		Role:      "developer",
	})
	if !strings.Contains(cmd, "--settings") {
		t.Errorf("expected --settings flag, got %q", cmd)
	}
	promptFile := findTempFile(t, "prompt", "claude-board-w1", "txt")
	data, _ := os.ReadFile(promptFile)
	if !strings.Contains(string(data), "test-board") {
		t.Errorf("expected board name in prompt file, got %q", string(data))
	}
}

func TestClaude_WithBoardOrchestrator(t *testing.T) {
	a := &ClaudeAgent{}
	a.BuildLaunchCommand(LaunchParams{
		SessionID: "claude-board-o1",
		BoardName: "test-board",
		Role:      "Orchestrator",
	})
	promptFile := findTempFile(t, "prompt", "claude-board-o1", "txt")
	data, _ := os.ReadFile(promptFile)
	if !strings.Contains(string(data), "discuss your proposed plan") {
		t.Errorf("expected orchestrator action in prompt file, got %q", string(data))
	}
}

func TestClaude_WithPrompt(t *testing.T) {
	a := &ClaudeAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		SessionID: "s1",
		Prompt:    "Fix the login bug",
	})
	if !strings.Contains(cmd, "coral_prompt_") {
		t.Errorf("expected prompt file reference, got %q", cmd)
	}
}

func TestClaude_WithCapabilities(t *testing.T) {
	a := &ClaudeAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		SessionID:    "s1",
		Capabilities: &Capabilities{Allow: []string{CapFileRead, CapShell}},
	})
	if !strings.Contains(cmd, "--settings") {
		t.Errorf("expected --settings with capabilities, got %q", cmd)
	}
}

// ── BuildBoardSystemPrompt Tests (shared helper) ────────────

func TestBuildBoardSystemPrompt_Worker(t *testing.T) {
	prompt := BuildBoardSystemPrompt("my-board", "developer", "Build the UI", nil, "")
	if !strings.Contains(prompt, "Build the UI") || !strings.Contains(prompt, "my-board") ||
		!strings.Contains(prompt, "coral-board") || !strings.Contains(prompt, DefaultWorkerSystemPrompt) {
		t.Error("missing expected content in worker system prompt")
	}
}

func TestBuildBoardSystemPrompt_Orchestrator(t *testing.T) {
	prompt := BuildBoardSystemPrompt("my-board", "Orchestrator", "", nil, "")
	if !strings.Contains(prompt, DefaultOrchestratorSystemPrompt) {
		t.Error("expected orchestrator system prompt")
	}
}

func TestBuildBoardSystemPrompt_WithOverrides(t *testing.T) {
	overrides := map[string]string{"default_prompt_worker": "Custom worker instructions"}
	prompt := BuildBoardSystemPrompt("board1", "dev", "", overrides, "")
	if !strings.Contains(prompt, "Custom worker instructions") {
		t.Error("expected custom worker prompt override")
	}
}

func TestBuildBoardSystemPrompt_NoBoard(t *testing.T) {
	if prompt := BuildBoardSystemPrompt("", "", "", nil, ""); prompt != "" {
		t.Errorf("expected empty prompt without board, got %q", prompt)
	}
}

func TestBuildBoardSystemPrompt_PromptOnly(t *testing.T) {
	if prompt := BuildBoardSystemPrompt("", "", "Do something", nil, ""); prompt != "Do something" {
		t.Errorf("expected just the prompt, got %q", prompt)
	}
}

// ── Codex BuildLaunchCommand Tests ──────────────────────────

func TestCodex_BasicLaunch(t *testing.T) {
	a := &CodexAgent{}
	if cmd := a.BuildLaunchCommand(LaunchParams{}); cmd != "codex" {
		t.Errorf("expected bare 'codex', got %q", cmd)
	}
}

func TestCodex_Resume(t *testing.T) {
	a := &CodexAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{ResumeSessionID: "resume-789"})
	if !strings.Contains(cmd, "codex resume resume-789") {
		t.Errorf("expected 'codex resume resume-789', got %q", cmd)
	}
}

func TestCodex_WithFlags(t *testing.T) {
	a := &CodexAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{Flags: []string{"--json", "--model", "gpt-4"}})
	if !strings.Contains(cmd, "--json") || !strings.Contains(cmd, "--model gpt-4") {
		t.Errorf("expected flags, got %q", cmd)
	}
}

func TestCodex_WithPrompt(t *testing.T) {
	a := &CodexAgent{}
	sid := "codex-prompt-test1"
	a.BuildLaunchCommand(LaunchParams{SessionID: sid, Prompt: "Fix the bug"})
	promptFile := findTempFile(t, "codex_prompt", sid, "txt")
	content, _ := os.ReadFile(promptFile)
	if !strings.Contains(string(content), "Fix the bug") {
		t.Errorf("expected prompt in file, got %q", string(content))
	}
}

func TestCodex_WithBoardWorker(t *testing.T) {
	a := &CodexAgent{}
	sid := "codex-board-w1"
	cmd := a.BuildLaunchCommand(LaunchParams{
		SessionID: sid, Prompt: "Build frontend", BoardName: "dev-board", Role: "developer",
	})
	if !strings.Contains(cmd, "-c developer_instructions=") {
		t.Errorf("expected -c developer_instructions flag, got %q", cmd)
	}
	sysFile := findTempFile(t, "codex_instructions", sid, "md")
	sysContent, _ := os.ReadFile(sysFile)
	if !strings.Contains(string(sysContent), "dev-board") {
		t.Errorf("expected board name in system instructions, got %q", string(sysContent))
	}
	promptFile := findTempFile(t, "codex_prompt", sid, "txt")
	content, _ := os.ReadFile(promptFile)
	if !strings.Contains(string(content), "Build frontend") {
		t.Errorf("expected prompt in action file, got %q", string(content))
	}
}

func TestCodex_WithBoardOrchestrator(t *testing.T) {
	a := &CodexAgent{}
	sid := "codex-board-o1"
	cmd := a.BuildLaunchCommand(LaunchParams{
		SessionID: sid, Prompt: "Coordinate team", BoardName: "dev-board", Role: "orchestrator",
	})
	if !strings.Contains(cmd, "-c developer_instructions=") {
		t.Errorf("expected -c developer_instructions flag, got %q", cmd)
	}
	promptFile := findTempFile(t, "codex_prompt", sid, "txt")
	content, _ := os.ReadFile(promptFile)
	if !strings.Contains(string(content), "discuss your proposed plan") {
		t.Errorf("expected orchestrator action in prompt, got %q", string(content))
	}
}

func TestCodex_SystemPromptSeparation(t *testing.T) {
	a := &CodexAgent{}
	sid := "codex-sep-test1"
	cmd := a.BuildLaunchCommand(LaunchParams{
		SessionID: sid, Prompt: "Do work", BoardName: "board1", Role: "dev",
	})
	findTempFile(t, "codex_instructions", sid, "md") // verifies existence
	findTempFile(t, "codex_prompt", sid, "txt")
	if !strings.Contains(cmd, "developer_instructions") || !strings.Contains(cmd, "codex_prompt") {
		t.Errorf("expected both file references in command, got %q", cmd)
	}
}

// ── Codex Permission Tests ─────────────────────────────────

func TestCodex_WithCapabilities_FullAuto(t *testing.T) {
	a := &CodexAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{Capabilities: &Capabilities{Allow: []string{CapShell}}})
	if !strings.Contains(cmd, "--full-auto") {
		t.Errorf("expected --full-auto, got %q", cmd)
	}
}

func TestCodex_WithCapabilities_BypassSandbox(t *testing.T) {
	a := &CodexAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		Capabilities: &Capabilities{Allow: []string{CapShell, CapFileRead, CapFileWrite, CapGitWrite}},
	})
	if !strings.Contains(cmd, "--dangerously-bypass-approvals-and-sandbox") {
		t.Errorf("expected bypass sandbox, got %q", cmd)
	}
}

func TestCodex_WithCapabilities_ReadOnly(t *testing.T) {
	a := &CodexAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{Capabilities: &Capabilities{Allow: []string{CapFileRead}}})
	if !strings.Contains(cmd, "--sandbox read-only") || !strings.Contains(cmd, "-a untrusted") {
		t.Errorf("expected read-only sandbox, got %q", cmd)
	}
}

func TestCodex_WithCapabilities_ReadWrite(t *testing.T) {
	a := &CodexAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		Capabilities: &Capabilities{Allow: []string{CapFileRead, CapFileWrite}},
	})
	if !strings.Contains(cmd, "--sandbox workspace-write") || !strings.Contains(cmd, "-a untrusted") {
		t.Errorf("expected workspace-write sandbox, got %q", cmd)
	}
}

func TestCodex_WithCapabilities_ShellWithDeny(t *testing.T) {
	a := &CodexAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		Capabilities: &Capabilities{Allow: []string{CapShell}, Deny: []string{CapGitWrite}},
	})
	if strings.Contains(cmd, "--full-auto") {
		t.Errorf("should not have --full-auto with deny list, got %q", cmd)
	}
	if !strings.Contains(cmd, "--sandbox workspace-write") {
		t.Errorf("expected workspace-write sandbox, got %q", cmd)
	}
}

func TestCodex_WithCapabilities_WebSearch(t *testing.T) {
	a := &CodexAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		Capabilities: &Capabilities{Allow: []string{CapFileRead, CapWebAccess}},
	})
	if !strings.Contains(cmd, "--search") {
		t.Errorf("expected --search, got %q", cmd)
	}
}

func TestCodex_WithCapabilities_Nil(t *testing.T) {
	a := &CodexAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{})
	if strings.Contains(cmd, "--full-auto") {
		t.Errorf("should not have --full-auto with nil capabilities, got %q", cmd)
	}
}

func TestCodex_ResumeWithPromptAndInstructions(t *testing.T) {
	a := &CodexAgent{}
	sid := "codex-resume-test1"
	cmd := a.BuildLaunchCommand(LaunchParams{
		SessionID: sid, ResumeSessionID: "resume-abc", Prompt: "Continue work",
		BoardName: "board1", Role: "dev",
	})
	if !strings.Contains(cmd, "codex resume resume-abc") {
		t.Errorf("expected resume, got %q", cmd)
	}
	if !strings.Contains(cmd, "-c developer_instructions=") {
		t.Errorf("expected developer_instructions on resume, got %q", cmd)
	}
}

func TestCodex_FlagTranslation(t *testing.T) {
	a := &CodexAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{Flags: []string{"--dangerously-skip-permissions"}})
	if strings.Contains(cmd, "--dangerously-skip-permissions") || !strings.Contains(cmd, "--full-auto") {
		t.Errorf("expected flag translation, got %q", cmd)
	}
}

// ── Gemini BuildLaunchCommand Tests ─────────────────────────

func TestGemini_BasicLaunch(t *testing.T) {
	a := &GeminiAgent{}
	if cmd := a.BuildLaunchCommand(LaunchParams{}); cmd != "gemini" {
		t.Errorf("expected bare 'gemini', got %q", cmd)
	}
}

func TestGemini_WithFlags(t *testing.T) {
	a := &GeminiAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{Flags: []string{"--verbose"}})
	if !strings.Contains(cmd, "--verbose") {
		t.Errorf("expected --verbose, got %q", cmd)
	}
}

func TestGemini_ResumeDisabled(t *testing.T) {
	a := &GeminiAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{ResumeSessionID: "some-id"})
	// Gemini resume is disabled — --resume should NOT appear
	if strings.Contains(cmd, "--resume") {
		t.Errorf("gemini should not have --resume flag (disabled), got %q", cmd)
	}
}

func TestGemini_WithPromptTempFile(t *testing.T) {
	a := &GeminiAgent{}
	sid := "gemini-prompt-test1"
	cmd := a.BuildLaunchCommand(LaunchParams{SessionID: sid, Prompt: "Analyze the codebase"})
	promptFile := findTempFile(t, "gemini_prompt", sid, "txt")
	content, _ := os.ReadFile(promptFile)
	if !strings.Contains(string(content), "Analyze the codebase") {
		t.Errorf("expected prompt in temp file, got %q", string(content))
	}
	if !strings.Contains(cmd, "gemini_prompt") {
		t.Errorf("expected gemini_prompt reference, got %q", cmd)
	}
}

func TestGemini_WithBoardWorker(t *testing.T) {
	a := &GeminiAgent{}
	sid := "gemini-board-w1"
	cmd := a.BuildLaunchCommand(LaunchParams{
		SessionID: sid, Prompt: "Build API", BoardName: "team-board", Role: "developer",
	})
	if !strings.Contains(cmd, "GEMINI_SYSTEM_MD=") {
		t.Errorf("expected GEMINI_SYSTEM_MD, got %q", cmd)
	}
	promptFile := findTempFile(t, "gemini_prompt", sid, "txt")
	content, _ := os.ReadFile(promptFile)
	if !strings.Contains(string(content), "team-board") {
		t.Errorf("expected board name in prompt, got %q", string(content))
	}
}

// ── Gemini Permission Tests ─────────────────────────────────

func TestGemini_WithCapabilities_Yolo(t *testing.T) {
	a := &GeminiAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		Capabilities: &Capabilities{Allow: []string{CapShell, CapFileWrite}},
	})
	if !strings.Contains(cmd, "--approval-mode yolo") {
		t.Errorf("expected yolo, got %q", cmd)
	}
}

func TestGemini_WithCapabilities_AutoEdit(t *testing.T) {
	a := &GeminiAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		Capabilities: &Capabilities{Allow: []string{CapShell, CapFileWrite}, Deny: []string{CapGitWrite}},
	})
	if !strings.Contains(cmd, "--approval-mode auto_edit") {
		t.Errorf("expected auto_edit, got %q", cmd)
	}
}

func TestGemini_WithCapabilities_Plan(t *testing.T) {
	a := &GeminiAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		Capabilities: &Capabilities{Allow: []string{CapFileRead}},
	})
	if !strings.Contains(cmd, "--approval-mode plan") {
		t.Errorf("expected plan, got %q", cmd)
	}
}

func TestGemini_WithCapabilities_ReadWrite(t *testing.T) {
	a := &GeminiAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		Capabilities: &Capabilities{Allow: []string{CapFileRead, CapFileWrite}},
	})
	if !strings.Contains(cmd, "--approval-mode auto_edit") {
		t.Errorf("expected auto_edit, got %q", cmd)
	}
}

func TestGemini_WithCapabilities_Nil(t *testing.T) {
	a := &GeminiAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{})
	if strings.Contains(cmd, "--approval-mode") {
		t.Errorf("should not have --approval-mode, got %q", cmd)
	}
}

func TestGemini_ResumeWithPermissions(t *testing.T) {
	a := &GeminiAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		ResumeSessionID: "resume-xyz",
		Capabilities:    &Capabilities{Allow: []string{CapShell, CapFileWrite}},
	})
	// Resume is disabled, but permissions should still be emitted
	if strings.Contains(cmd, "--resume") {
		t.Errorf("gemini should not have --resume flag (disabled), got %q", cmd)
	}
	if !strings.Contains(cmd, "--approval-mode yolo") {
		t.Errorf("expected yolo even without resume, got %q", cmd)
	}
}

// ── Permission Translation Tests ────────────────────────────

func TestTranslateToClaudePermissions_Nil(t *testing.T) {
	if TranslateToClaudePermissions(nil) != nil {
		t.Error("expected nil")
	}
}

func TestTranslateToClaudePermissions_Empty(t *testing.T) {
	if TranslateToClaudePermissions(&Capabilities{}) != nil {
		t.Error("expected nil")
	}
}

func TestTranslateToClaudePermissions_FileRead(t *testing.T) {
	result := TranslateToClaudePermissions(&Capabilities{Allow: []string{CapFileRead}})
	if result == nil {
		t.Fatal("expected non-nil")
	}
	allowStr := strings.Join(result.Allow, ",")
	for _, tool := range []string{"Read", "Glob", "Grep", "Bash(coral-board *)"} {
		if !strings.Contains(allowStr, tool) {
			t.Errorf("missing %q in allow list", tool)
		}
	}
}

func TestTranslateToClaudePermissions_Shell(t *testing.T) {
	result := TranslateToClaudePermissions(&Capabilities{Allow: []string{CapShell}})
	if result == nil {
		t.Fatal("expected non-nil")
	}
	found := false
	for _, a := range result.Allow {
		if a == "Bash" {
			found = true
		}
	}
	if !found {
		t.Error("expected Bash in allow list")
	}
}

func TestTranslateToClaudePermissions_DenyList(t *testing.T) {
	result := TranslateToClaudePermissions(&Capabilities{Allow: []string{CapFileRead}, Deny: []string{CapShell}})
	if result == nil {
		t.Fatal("expected non-nil")
	}
	found := false
	for _, d := range result.Deny {
		if d == "Bash" {
			found = true
		}
	}
	if !found {
		t.Error("expected Bash in deny list")
	}
}

func TestTranslateToClaudePermissions_AllCapabilities(t *testing.T) {
	result := TranslateToClaudePermissions(&Capabilities{
		Allow: []string{CapFileRead, CapFileWrite, CapShell, CapWebAccess, CapGitWrite, CapAgentSpawn, CapNotebook},
	})
	if result == nil {
		t.Fatal("expected non-nil")
	}
	allowStr := strings.Join(result.Allow, ",")
	for _, tool := range []string{"Read", "Write", "Bash", "WebFetch", "Agent", "NotebookEdit", "Bash(git push *)"} {
		if !strings.Contains(allowStr, tool) {
			t.Errorf("missing %q", tool)
		}
	}
}

// ── Codex Permission Translation Tests ──────────────────────

func TestTranslateToCodexPermissions_Nil(t *testing.T) {
	if TranslateToCodexPermissions(nil) != nil {
		t.Error("expected nil")
	}
}

func TestTranslateToCodexPermissions_Empty(t *testing.T) {
	if TranslateToCodexPermissions(&Capabilities{}) != nil {
		t.Error("expected nil")
	}
}

func TestTranslateToCodexPermissions_ShellFullAuto(t *testing.T) {
	result := TranslateToCodexPermissions(&Capabilities{Allow: []string{CapShell}})
	if result == nil || !result.FullAuto {
		t.Error("expected FullAuto=true")
	}
}

func TestTranslateToCodexPermissions_ShellWithDenyNotFullAuto(t *testing.T) {
	result := TranslateToCodexPermissions(&Capabilities{Allow: []string{CapShell}, Deny: []string{CapGitWrite}})
	if result == nil {
		t.Fatal("expected non-nil")
	}
	if result.FullAuto || result.SandboxMode != "workspace-write" || result.ApprovalPolicy != "untrusted" {
		t.Errorf("unexpected: %+v", result)
	}
}

func TestTranslateToCodexPermissions_ReadOnly(t *testing.T) {
	result := TranslateToCodexPermissions(&Capabilities{Allow: []string{CapFileRead}})
	if result == nil || result.SandboxMode != "read-only" || result.ApprovalPolicy != "untrusted" {
		t.Errorf("unexpected: %+v", result)
	}
}

func TestTranslateToCodexPermissions_FullAccess(t *testing.T) {
	result := TranslateToCodexPermissions(&Capabilities{
		Allow: []string{CapShell, CapFileRead, CapFileWrite, CapGitWrite},
	})
	if result == nil || !result.BypassSandbox {
		t.Error("expected BypassSandbox=true")
	}
}

func TestTranslateToCodexPermissions_WebSearch(t *testing.T) {
	result := TranslateToCodexPermissions(&Capabilities{Allow: []string{CapFileRead, CapWebAccess}})
	if result == nil || !result.Search {
		t.Error("expected Search=true")
	}
}

// ── Gemini Permission Translation Tests ─────────────────────

func TestTranslateToGeminiPermissions_Nil(t *testing.T) {
	if TranslateToGeminiPermissions(nil) != nil {
		t.Error("expected nil")
	}
}

func TestTranslateToGeminiPermissions_Empty(t *testing.T) {
	if TranslateToGeminiPermissions(&Capabilities{}) != nil {
		t.Error("expected nil")
	}
}

func TestTranslateToGeminiPermissions_Yolo(t *testing.T) {
	result := TranslateToGeminiPermissions(&Capabilities{Allow: []string{CapShell, CapFileWrite}})
	if result == nil || result.ApprovalMode != "yolo" {
		t.Error("expected yolo")
	}
}

func TestTranslateToGeminiPermissions_AutoEdit(t *testing.T) {
	result := TranslateToGeminiPermissions(&Capabilities{
		Allow: []string{CapShell, CapFileWrite}, Deny: []string{CapGitWrite},
	})
	if result == nil || result.ApprovalMode != "auto_edit" {
		t.Error("expected auto_edit")
	}
}

func TestTranslateToGeminiPermissions_Plan(t *testing.T) {
	result := TranslateToGeminiPermissions(&Capabilities{Allow: []string{CapFileRead}})
	if result == nil || result.ApprovalMode != "plan" {
		t.Error("expected plan")
	}
}

func TestTranslateToGeminiPermissions_Default(t *testing.T) {
	result := TranslateToGeminiPermissions(&Capabilities{Allow: []string{CapWebAccess}})
	if result == nil || result.ApprovalMode != "default" {
		t.Error("expected default")
	}
}

// ── TranslatePermissions Dispatcher Tests ───────────────────

func TestTranslatePermissions_Claude(t *testing.T) {
	if _, ok := TranslatePermissions("claude", &Capabilities{Allow: []string{CapFileRead}}).(*ClaudePermissions); !ok {
		t.Error("expected *ClaudePermissions")
	}
}

func TestTranslatePermissions_Codex(t *testing.T) {
	cp, ok := TranslatePermissions("codex", &Capabilities{Allow: []string{CapShell}}).(*CodexPermissions)
	if !ok || !cp.FullAuto {
		t.Error("expected CodexPermissions with FullAuto")
	}
}

func TestTranslatePermissions_Gemini(t *testing.T) {
	if _, ok := TranslatePermissions("gemini", &Capabilities{Allow: []string{CapFileRead}}).(*GeminiPermissions); !ok {
		t.Error("expected *GeminiPermissions")
	}
}

func TestTranslatePermissions_NilCaps(t *testing.T) {
	if cp, ok := TranslatePermissions("claude", nil).(*ClaudePermissions); ok && cp != nil {
		t.Error("expected nil inner value")
	}
}

// ── Capabilities.IsEmpty Tests ──────────────────────────────

func TestCapabilities_IsEmpty(t *testing.T) {
	if !((*Capabilities)(nil)).IsEmpty() {
		t.Error("nil should be empty")
	}
	if !(&Capabilities{}).IsEmpty() {
		t.Error("zero-value should be empty")
	}
	if (&Capabilities{Allow: []string{"x"}}).IsEmpty() {
		t.Error("with Allow should not be empty")
	}
}

// ── Presets Tests ───────────────────────────────────────────

func TestPresets_Exist(t *testing.T) {
	for _, name := range []string{"lead_dev", "qa", "frontend_dev", "orchestrator", "devops", "read_only", "full_access"} {
		if _, ok := Presets[name]; !ok {
			t.Errorf("missing preset %q", name)
		}
	}
}

func TestPresets_FullAccessHasAll(t *testing.T) {
	fa := Presets["full_access"]
	allowSet := map[string]bool{}
	for _, a := range fa.Allow {
		allowSet[a] = true
	}
	for _, cap := range []string{CapFileRead, CapFileWrite, CapShell, CapGitWrite, CapAgentSpawn, CapWebAccess, CapNotebook} {
		if !allowSet[cap] {
			t.Errorf("full_access missing %s", cap)
		}
	}
}

// ── SanitizeShellValue Tests ────────────────────────────────

func TestSanitizeShellValue_Clean(t *testing.T) {
	if got := SanitizeShellValue("my-session_123"); got != "my-session_123" {
		t.Errorf("got %q", got)
	}
}

func TestSanitizeShellValue_StripsDangerousChars(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{`$(whoami)`, "whoami"},
		{"`id`", "id"},
		{`role"; rm -rf /; echo "`, "role rm -rf  echo "},
		{"a'b", "ab"},
		{"a;b", "ab"},
		{"a|b", "ab"},
		{"a&b", "ab"},
	}
	for _, tt := range tests {
		if got := SanitizeShellValue(tt.input); got != tt.expected {
			t.Errorf("SanitizeShellValue(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestCodex_EnvVarsSingleQuoted(t *testing.T) {
	a := &CodexAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{SessionName: "codex-abc123", Role: "developer"})
	if !strings.Contains(cmd, "CORAL_SESSION_NAME='codex-abc123'") ||
		!strings.Contains(cmd, "CORAL_SUBSCRIBER_ID='developer'") {
		t.Errorf("expected single-quoted env vars, got %q", cmd)
	}
}

func TestGemini_EnvVarsSingleQuoted(t *testing.T) {
	a := &GeminiAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{SessionName: "gemini-xyz789", Role: "qa"})
	if !strings.Contains(cmd, "CORAL_SESSION_NAME='gemini-xyz789'") ||
		!strings.Contains(cmd, "CORAL_SUBSCRIBER_ID='qa'") {
		t.Errorf("expected single-quoted env vars, got %q", cmd)
	}
}

func TestCodex_EnvVarsSanitized(t *testing.T) {
	a := &CodexAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{SessionName: `$(evil)`, Role: "`whoami`"})
	if strings.Contains(cmd, "$") || strings.Contains(cmd, "`") {
		t.Errorf("expected sanitized, got %q", cmd)
	}
}

// ── Temp File Tests ─────────────────────────────────────────

func TestWriteTempFile_CreatesFile(t *testing.T) {
	path := writeTempFile("test", "session123", "txt", []byte("hello"))
	defer os.Remove(path)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}
	if string(content) != "hello" {
		t.Errorf("got %q", string(content))
	}
	if !strings.Contains(path, "session123") {
		t.Errorf("expected session ID in path, got %q", path)
	}
}

func TestCleanupTempFiles(t *testing.T) {
	sid := "cleanup-test-sid"
	p1 := writeTempFile("prompt", sid, "txt", []byte("prompt"))
	p2 := writeTempFile("instructions", sid, "md", []byte("instructions"))
	CleanupTempFiles(sid)
	if _, err := os.Stat(p1); !os.IsNotExist(err) {
		t.Error("prompt file should be removed")
	}
	if _, err := os.Stat(p2); !os.IsNotExist(err) {
		t.Error("instructions file should be removed")
	}
}

func TestCleanupTempFiles_EmptySessionID(t *testing.T) {
	CleanupTempFiles("") // should not panic
}

// ── Shell Detection Tests ───────────────────────────────────

func TestClassifyShell(t *testing.T) {
	tests := []struct {
		input    string
		expected ShellType
	}{
		{"/bin/bash", ShellBash}, {"/bin/zsh", ShellZsh}, {"zsh", ShellZsh},
		{"pwsh", ShellPowerShell}, {"powershell.exe", ShellPowerShell},
		{"cmd", ShellCmd}, {"cmd.exe", ShellCmd}, {"sh", ShellBash},
	}
	for _, tt := range tests {
		if got := classifyShell(tt.input); got != tt.expected {
			t.Errorf("classifyShell(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
