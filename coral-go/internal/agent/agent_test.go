package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── Factory Tests ───────────────────────────────────────────

func TestGetAgent_Claude(t *testing.T) {
	a := GetAgent("claude")
	if a.AgentType() != "claude" {
		t.Errorf("expected claude, got %s", a.AgentType())
	}
	if _, ok := a.(*ClaudeAgent); !ok {
		t.Errorf("expected *ClaudeAgent, got %T", a)
	}
}

func TestGetAgent_Gemini(t *testing.T) {
	a := GetAgent("gemini")
	if a.AgentType() != "gemini" {
		t.Errorf("expected gemini, got %s", a.AgentType())
	}
	if _, ok := a.(*GeminiAgent); !ok {
		t.Errorf("expected *GeminiAgent, got %T", a)
	}
}

func TestGetAgent_Codex(t *testing.T) {
	a := GetAgent("codex")
	if a.AgentType() != "codex" {
		t.Errorf("expected codex, got %s", a.AgentType())
	}
	if _, ok := a.(*CodexAgent); !ok {
		t.Errorf("expected *CodexAgent, got %T", a)
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
	if strings.Contains(cmd, "--resume") {
		t.Errorf("should not have --resume without ResumeSessionID")
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
	if strings.Contains(cmd, "--session-id") {
		t.Errorf("should not have --session-id when resuming")
	}
}

func TestClaude_WithFlags(t *testing.T) {
	a := &ClaudeAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		SessionID: "s1",
		Flags:     []string{"--verbose", "--model", "opus"},
	})
	if !strings.Contains(cmd, "--verbose") {
		t.Errorf("expected --verbose flag, got %q", cmd)
	}
	if !strings.Contains(cmd, "--model opus") {
		t.Errorf("expected --model opus, got %q", cmd)
	}
}

func TestClaude_WithBoardWorker(t *testing.T) {
	a := &ClaudeAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		SessionID: "s1",
		BoardName: "test-board",
		Role:      "developer",
	})
	// Should include settings file (systemPrompt with board info)
	if !strings.Contains(cmd, "--settings") {
		t.Errorf("expected --settings flag, got %q", cmd)
	}
	// Should include prompt file reference
	if !strings.Contains(cmd, "coral_prompt_s1") {
		t.Errorf("expected prompt file reference, got %q", cmd)
	}
	// Check prompt file content contains board name
	promptFile := os.TempDir() + "/coral_prompt_s1.txt"
	data, err := os.ReadFile(promptFile)
	if err == nil {
		if !strings.Contains(string(data), "test-board") {
			t.Errorf("expected board name in prompt file, got %q", string(data))
		}
	}
}

func TestClaude_WithBoardOrchestrator(t *testing.T) {
	a := &ClaudeAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		SessionID: "s1-orch",
		BoardName: "test-board",
		Role:      "Orchestrator",
	})
	if !strings.Contains(cmd, "--settings") {
		t.Errorf("expected --settings flag, got %q", cmd)
	}
	// Check prompt file content contains orchestrator action
	promptFile := os.TempDir() + "/coral_prompt_s1-orch.txt"
	data, err := os.ReadFile(promptFile)
	if err == nil {
		if !strings.Contains(string(data), "test-board") {
			t.Errorf("expected board name in prompt file, got %q", string(data))
		}
		if !strings.Contains(string(data), "discuss your proposed plan") {
			t.Errorf("expected orchestrator action in prompt file, got %q", string(data))
		}
	}
}

func TestClaude_WithPrompt(t *testing.T) {
	a := &ClaudeAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		SessionID: "s1",
		Prompt:    "Fix the login bug",
	})
	// Prompt should be written to a file and included via cat
	if !strings.Contains(cmd, "coral_prompt_") {
		t.Errorf("expected prompt file reference, got %q", cmd)
	}
}

func TestClaude_WithCapabilities(t *testing.T) {
	a := &ClaudeAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		SessionID: "s1",
		Capabilities: &Capabilities{
			Allow: []string{CapFileRead, CapShell},
		},
	})
	if !strings.Contains(cmd, "--settings") {
		t.Errorf("expected --settings with capabilities, got %q", cmd)
	}
}

// ── BuildBoardSystemPrompt Tests (shared helper) ────────────

func TestBuildBoardSystemPrompt_Worker(t *testing.T) {
	prompt := BuildBoardSystemPrompt("my-board", "developer", "Build the UI", nil, "")
	if !strings.Contains(prompt, "Build the UI") {
		t.Error("expected user prompt in output")
	}
	if !strings.Contains(prompt, "my-board") {
		t.Error("expected board name in output")
	}
	if !strings.Contains(prompt, "coral-board") {
		t.Error("expected CLI name in output")
	}
	if !strings.Contains(prompt, "developer") {
		t.Error("expected role in output")
	}
	if !strings.Contains(prompt, DefaultWorkerSystemPrompt) {
		t.Error("expected worker system prompt")
	}
}

func TestBuildBoardSystemPrompt_Orchestrator(t *testing.T) {
	prompt := BuildBoardSystemPrompt("my-board", "Orchestrator", "", nil, "")
	if !strings.Contains(prompt, DefaultOrchestratorSystemPrompt) {
		t.Error("expected orchestrator system prompt")
	}
	if strings.Contains(prompt, DefaultWorkerSystemPrompt) {
		t.Error("should not contain worker system prompt")
	}
}

func TestBuildBoardSystemPrompt_WithOverrides(t *testing.T) {
	overrides := map[string]string{
		"default_prompt_worker": "Custom worker instructions",
	}
	prompt := BuildBoardSystemPrompt("board1", "dev", "", overrides, "")
	if !strings.Contains(prompt, "Custom worker instructions") {
		t.Error("expected custom worker prompt override")
	}
	if strings.Contains(prompt, DefaultWorkerSystemPrompt) {
		t.Error("should not contain default worker prompt when overridden")
	}
}

func TestBuildBoardSystemPrompt_NoBoard(t *testing.T) {
	prompt := BuildBoardSystemPrompt("", "", "", nil, "")
	if prompt != "" {
		t.Errorf("expected empty prompt without board, got %q", prompt)
	}
}

func TestBuildBoardSystemPrompt_PromptOnly(t *testing.T) {
	prompt := BuildBoardSystemPrompt("", "", "Do something", nil, "")
	if prompt != "Do something" {
		t.Errorf("expected just the prompt, got %q", prompt)
	}
}

// ── Codex BuildLaunchCommand Tests ──────────────────────────

func TestCodex_BasicLaunch(t *testing.T) {
	a := &CodexAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{})
	if cmd != "codex" {
		t.Errorf("expected bare 'codex', got %q", cmd)
	}
}

func TestCodex_Resume(t *testing.T) {
	a := &CodexAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		ResumeSessionID: "resume-789",
	})
	if !strings.Contains(cmd, "codex resume --session resume-789") {
		t.Errorf("expected 'codex resume --session resume-789', got %q", cmd)
	}
}

func TestCodex_WithFlags(t *testing.T) {
	a := &CodexAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		Flags: []string{"--json", "--model", "gpt-4"},
	})
	if !strings.Contains(cmd, "--json") {
		t.Errorf("expected --json flag, got %q", cmd)
	}
	if !strings.Contains(cmd, "--model gpt-4") {
		t.Errorf("expected --model gpt-4, got %q", cmd)
	}
}

func TestCodex_WithPrompt(t *testing.T) {
	a := &CodexAgent{}
	sid := "test-prompt-sid"
	cmd := a.BuildLaunchCommand(LaunchParams{
		SessionID: sid,
		Prompt:    "Fix the bug",
	})
	// Prompt is written to a temp file and referenced via shell command
	promptFile := filepath.Join(os.TempDir(), "coral_codex_prompt_"+sid+".txt")
	content, err := os.ReadFile(promptFile)
	if err != nil {
		t.Fatalf("expected prompt file to be written: %v", err)
	}
	if !strings.Contains(string(content), "Fix the bug") {
		t.Errorf("expected prompt in file, got %q", string(content))
	}
	if !strings.Contains(cmd, "codex") {
		t.Errorf("expected codex in command, got %q", cmd)
	}
}

func TestCodex_WithBoardWorker(t *testing.T) {
	a := &CodexAgent{}
	sid := "test-board-worker-sid"
	cmd := a.BuildLaunchCommand(LaunchParams{
		SessionID: sid,
		Prompt:    "Build frontend",
		BoardName: "dev-board",
		Role:      "developer",
	})
	promptFile := filepath.Join(os.TempDir(), "coral_codex_prompt_"+sid+".txt")
	content, err := os.ReadFile(promptFile)
	if err != nil {
		t.Fatalf("expected prompt file to be written: %v", err)
	}
	if !strings.Contains(string(content), "dev-board") {
		t.Errorf("expected board name in prompt file, got %q", string(content))
	}
	if !strings.Contains(string(content), "Build frontend") {
		t.Errorf("expected original prompt in file, got %q", string(content))
	}
	_ = cmd
}

func TestCodex_WithBoardOrchestrator(t *testing.T) {
	a := &CodexAgent{}
	sid := "test-board-orch-sid"
	cmd := a.BuildLaunchCommand(LaunchParams{
		SessionID: sid,
		Prompt:    "Coordinate team",
		BoardName: "dev-board",
		Role:      "orchestrator",
	})
	promptFile := filepath.Join(os.TempDir(), "coral_codex_prompt_"+sid+".txt")
	content, err := os.ReadFile(promptFile)
	if err != nil {
		t.Fatalf("expected prompt file to be written: %v", err)
	}
	if !strings.Contains(string(content), "dev-board") {
		t.Errorf("expected board name in prompt file, got %q", string(content))
	}
	if !strings.Contains(string(content), "discuss your proposed plan") {
		t.Errorf("expected orchestrator action in prompt file, got %q", string(content))
	}
	_ = cmd
}

func TestCodex_WithCapabilities_FullAuto(t *testing.T) {
	a := &CodexAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		Capabilities: &Capabilities{
			Allow: []string{CapShell},
		},
	})
	if !strings.Contains(cmd, "--full-auto") {
		t.Errorf("expected --full-auto from capabilities injection, got %q", cmd)
	}
}

func TestCodex_WithCapabilities_NoShell(t *testing.T) {
	a := &CodexAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		Capabilities: &Capabilities{
			Allow: []string{CapFileRead},
		},
	})
	if strings.Contains(cmd, "--full-auto") {
		t.Errorf("should not have --full-auto without shell capability, got %q", cmd)
	}
}

func TestCodex_WithCapabilities_Nil(t *testing.T) {
	a := &CodexAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{})
	if strings.Contains(cmd, "--full-auto") {
		t.Errorf("should not have --full-auto with nil capabilities, got %q", cmd)
	}
}

// ── Gemini BuildLaunchCommand Tests ─────────────────────────

func TestGemini_BasicLaunch(t *testing.T) {
	a := &GeminiAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{})
	if cmd != "gemini" {
		t.Errorf("expected bare 'gemini', got %q", cmd)
	}
}

func TestGemini_WithFlags(t *testing.T) {
	a := &GeminiAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		Flags: []string{"--verbose"},
	})
	if !strings.Contains(cmd, "--verbose") {
		t.Errorf("expected --verbose, got %q", cmd)
	}
}

func TestGemini_NoResume(t *testing.T) {
	a := &GeminiAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		ResumeSessionID: "some-id",
	})
	// Gemini doesn't support resume, so it should be ignored
	if strings.Contains(cmd, "--resume") {
		t.Error("gemini should not have --resume flag")
	}
}

// ── Permission Translation Tests ────────────────────────────

func TestTranslateToClaudePermissions_Nil(t *testing.T) {
	result := TranslateToClaudePermissions(nil)
	if result != nil {
		t.Errorf("expected nil for nil capabilities, got %+v", result)
	}
}

func TestTranslateToClaudePermissions_Empty(t *testing.T) {
	result := TranslateToClaudePermissions(&Capabilities{})
	if result != nil {
		t.Errorf("expected nil for empty capabilities, got %+v", result)
	}
}

func TestTranslateToClaudePermissions_FileRead(t *testing.T) {
	result := TranslateToClaudePermissions(&Capabilities{
		Allow: []string{CapFileRead},
	})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Should include Read, Glob, Grep + coral-board
	allowStr := strings.Join(result.Allow, ",")
	for _, tool := range []string{"Read", "Glob", "Grep", "Bash(coral-board *)"} {
		if !strings.Contains(allowStr, tool) {
			t.Errorf("expected %q in allow list, got %v", tool, result.Allow)
		}
	}
}

func TestTranslateToClaudePermissions_FileWrite(t *testing.T) {
	result := TranslateToClaudePermissions(&Capabilities{
		Allow: []string{CapFileWrite},
	})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	allowStr := strings.Join(result.Allow, ",")
	for _, tool := range []string{"Write", "Edit"} {
		if !strings.Contains(allowStr, tool) {
			t.Errorf("expected %q in allow list, got %v", tool, result.Allow)
		}
	}
}

func TestTranslateToClaudePermissions_Shell(t *testing.T) {
	result := TranslateToClaudePermissions(&Capabilities{
		Allow: []string{CapShell},
	})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	found := false
	for _, a := range result.Allow {
		if a == "Bash" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected Bash in allow list, got %v", result.Allow)
	}
}

func TestTranslateToClaudePermissions_ShellPattern(t *testing.T) {
	result := TranslateToClaudePermissions(&Capabilities{
		Allow: []string{"shell:npm *"},
	})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	found := false
	for _, a := range result.Allow {
		if a == "Bash(npm *)" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected Bash(npm *) in allow list, got %v", result.Allow)
	}
}

func TestTranslateToClaudePermissions_CoralBoardAlwaysAllowed(t *testing.T) {
	result := TranslateToClaudePermissions(&Capabilities{
		Allow: []string{CapFileRead},
	})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	found := false
	for _, a := range result.Allow {
		if a == "Bash(coral-board *)" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("coral-board should always be in allow list, got %v", result.Allow)
	}
}

func TestTranslateToClaudePermissions_DenyList(t *testing.T) {
	result := TranslateToClaudePermissions(&Capabilities{
		Allow: []string{CapFileRead},
		Deny:  []string{CapShell},
	})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	found := false
	for _, d := range result.Deny {
		if d == "Bash" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected Bash in deny list, got %v", result.Deny)
	}
}

func TestTranslateToClaudePermissions_AllCapabilities(t *testing.T) {
	result := TranslateToClaudePermissions(&Capabilities{
		Allow: []string{CapFileRead, CapFileWrite, CapShell, CapWebAccess, CapGitWrite, CapAgentSpawn, CapNotebook},
	})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	allowStr := strings.Join(result.Allow, ",")
	for _, tool := range []string{"Read", "Glob", "Grep", "Write", "Edit", "Bash", "WebFetch", "WebSearch", "Agent", "NotebookEdit"} {
		if !strings.Contains(allowStr, tool) {
			t.Errorf("expected %q in allow list for full capabilities", tool)
		}
	}
	// Git write should produce specific patterns
	for _, pattern := range []string{"Bash(git push *)", "Bash(git commit *)"} {
		if !strings.Contains(allowStr, pattern) {
			t.Errorf("expected %q in allow list for git_write", pattern)
		}
	}
}

func TestTranslateToClaudePermissions_UnknownCapPassthrough(t *testing.T) {
	result := TranslateToClaudePermissions(&Capabilities{
		Allow: []string{"custom_tool"},
	})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	found := false
	for _, a := range result.Allow {
		if a == "custom_tool" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected unknown cap to pass through, got %v", result.Allow)
	}
}

// ── Codex Permission Translation Tests ──────────────────────

func TestTranslateToCodexPermissions_Nil(t *testing.T) {
	result := TranslateToCodexPermissions(nil)
	if result != nil {
		t.Errorf("expected nil for nil capabilities, got %+v", result)
	}
}

func TestTranslateToCodexPermissions_Empty(t *testing.T) {
	result := TranslateToCodexPermissions(&Capabilities{})
	if result != nil {
		t.Errorf("expected nil for empty capabilities, got %+v", result)
	}
}

func TestTranslateToCodexPermissions_ShellFullAuto(t *testing.T) {
	result := TranslateToCodexPermissions(&Capabilities{
		Allow: []string{CapShell},
	})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.FullAuto {
		t.Error("expected FullAuto=true when shell allowed with no denies")
	}
}

func TestTranslateToCodexPermissions_ShellWithDenyNotFullAuto(t *testing.T) {
	result := TranslateToCodexPermissions(&Capabilities{
		Allow: []string{CapShell},
		Deny:  []string{CapGitWrite},
	})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.FullAuto {
		t.Error("expected FullAuto=false when shell allowed with denies")
	}
}

func TestTranslateToCodexPermissions_NoShellReturnsAllowList(t *testing.T) {
	result := TranslateToCodexPermissions(&Capabilities{
		Allow: []string{CapFileRead, CapFileWrite},
	})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.FullAuto {
		t.Error("expected FullAuto=false without shell capability")
	}
	if len(result.Allow) != 2 {
		t.Errorf("expected 2 allow entries, got %d: %v", len(result.Allow), result.Allow)
	}
}

// ── Codex Flag Translation Tests ────────────────────────────

func TestCodex_FlagTranslation(t *testing.T) {
	a := &CodexAgent{}
	cmd := a.BuildLaunchCommand(LaunchParams{
		Flags: []string{"--dangerously-skip-permissions"},
	})
	if strings.Contains(cmd, "--dangerously-skip-permissions") {
		t.Error("Claude flag should be translated, not passed through")
	}
	if !strings.Contains(cmd, "--full-auto") {
		t.Errorf("expected --full-auto (translated from Claude flag), got %q", cmd)
	}
}

// ── Gemini Permission Translation Tests ─────────────────────

func TestTranslateToGeminiPermissions_Nil(t *testing.T) {
	result := TranslateToGeminiPermissions(nil)
	if result != nil {
		t.Errorf("expected nil for nil capabilities, got %+v", result)
	}
}

func TestTranslateToGeminiPermissions_Empty(t *testing.T) {
	result := TranslateToGeminiPermissions(&Capabilities{})
	if result != nil {
		t.Errorf("expected nil for empty capabilities, got %+v", result)
	}
}

func TestTranslateToGeminiPermissions_PassThrough(t *testing.T) {
	result := TranslateToGeminiPermissions(&Capabilities{
		Allow: []string{CapFileRead, CapShell},
	})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Allow) != 2 {
		t.Errorf("expected 2 allow entries, got %d", len(result.Allow))
	}
}

// ── TranslatePermissions Dispatcher Tests ───────────────────

func TestTranslatePermissions_Claude(t *testing.T) {
	result := TranslatePermissions("claude", &Capabilities{Allow: []string{CapFileRead}})
	if _, ok := result.(*ClaudePermissions); !ok {
		t.Errorf("expected *ClaudePermissions for claude, got %T", result)
	}
}

func TestTranslatePermissions_Codex(t *testing.T) {
	result := TranslatePermissions("codex", &Capabilities{Allow: []string{CapShell}})
	cp, ok := result.(*CodexPermissions)
	if !ok {
		t.Errorf("expected *CodexPermissions for codex, got %T", result)
	}
	if cp != nil && !cp.FullAuto {
		t.Error("expected FullAuto for codex with shell capability")
	}
}

func TestTranslatePermissions_Gemini(t *testing.T) {
	result := TranslatePermissions("gemini", &Capabilities{Allow: []string{CapFileRead}})
	if _, ok := result.(*GeminiPermissions); !ok {
		t.Errorf("expected *GeminiPermissions for gemini, got %T", result)
	}
}

func TestTranslatePermissions_UnknownDefaultsToClaude(t *testing.T) {
	result := TranslatePermissions("unknown", &Capabilities{Allow: []string{CapFileRead}})
	if _, ok := result.(*ClaudePermissions); !ok {
		t.Errorf("expected *ClaudePermissions for unknown agent, got %T", result)
	}
}

func TestTranslatePermissions_NilCaps(t *testing.T) {
	// TranslateToClaudePermissions(nil) returns (*ClaudePermissions)(nil),
	// which wraps to a non-nil interface. Verify the inner value is nil.
	result := TranslatePermissions("claude", nil)
	if cp, ok := result.(*ClaudePermissions); ok && cp != nil {
		t.Errorf("expected nil *ClaudePermissions for nil capabilities, got %+v", cp)
	}
}

// ── Capabilities.IsEmpty Tests ──────────────────────────────

func TestCapabilities_IsEmpty(t *testing.T) {
	if !((*Capabilities)(nil)).IsEmpty() {
		t.Error("nil Capabilities should be empty")
	}
	if !(&Capabilities{}).IsEmpty() {
		t.Error("zero-value Capabilities should be empty")
	}
	if (&Capabilities{Allow: []string{"x"}}).IsEmpty() {
		t.Error("Capabilities with Allow should not be empty")
	}
	if (&Capabilities{Deny: []string{"x"}}).IsEmpty() {
		t.Error("Capabilities with Deny should not be empty")
	}
}

// ── Presets Tests ───────────────────────────────────────────

func TestPresets_Exist(t *testing.T) {
	expectedPresets := []string{"lead_dev", "qa", "frontend_dev", "orchestrator", "devops", "read_only", "full_access"}
	for _, name := range expectedPresets {
		if _, ok := Presets[name]; !ok {
			t.Errorf("expected preset %q to exist", name)
		}
	}
}

func TestPresets_QAHasDenies(t *testing.T) {
	qa := Presets["qa"]
	if len(qa.Deny) == 0 {
		t.Error("qa preset should have deny rules")
	}
}

func TestPresets_FullAccessHasAll(t *testing.T) {
	fa := Presets["full_access"]
	allCaps := []string{CapFileRead, CapFileWrite, CapShell, CapGitWrite, CapAgentSpawn, CapWebAccess, CapNotebook}
	allowSet := map[string]bool{}
	for _, a := range fa.Allow {
		allowSet[a] = true
	}
	for _, cap := range allCaps {
		if !allowSet[cap] {
			t.Errorf("full_access should include %s", cap)
		}
	}
}

// ── Shell Detection Tests ───────────────────────────────────

func TestClassifyShell(t *testing.T) {
	tests := []struct {
		input    string
		expected ShellType
	}{
		{"/bin/bash", ShellBash},
		{"/bin/zsh", ShellZsh},
		{"/usr/bin/zsh", ShellZsh},
		{"bash", ShellBash},
		{"zsh", ShellZsh},
		{"pwsh", ShellPowerShell},
		{"powershell", ShellPowerShell},
		{"powershell.exe", ShellPowerShell},
		{"cmd", ShellCmd},
		{"cmd.exe", ShellCmd},
		{"sh", ShellBash},
		{"/usr/local/bin/fish", ShellBash}, // unknown defaults to bash
	}
	for _, tt := range tests {
		got := classifyShell(tt.input)
		if got != tt.expected {
			t.Errorf("classifyShell(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
