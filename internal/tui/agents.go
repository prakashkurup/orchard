package tui

// The action keys (C resume, H history, R search, f files, A cross-repo, M
// commit message) drive whichever assistant is resolved (Claude Code or Codex);
// these helpers translate each action into that assistant's CLI.

// agentSupportsSessions reports whether the resolved assistant has session
// history orchard can read and resume (Claude Code or Codex).
func (m model) agentSupportsSessions() bool {
	return m.assistantIsClaude() || m.assistantIsCodex()
}

// agentResumeLastArgs are the CLI args that continue the most recent session.
func (m model) agentResumeLastArgs() []string {
	if m.assistantIsCodex() {
		return []string{"resume", "--last"}
	}
	return []string{"--continue"}
}

// agentResumeArgs are the CLI args that resume a specific session by id.
func (m model) agentResumeArgs(id string) []string {
	if m.assistantIsCodex() {
		return []string{"resume", id}
	}
	return []string{"--resume", id}
}

// claudeHeadlessArgs runs Claude Code non-interactively, result as JSON on stdout.
func claudeHeadlessArgs(prompt string) []string {
	return []string{"-p", prompt, "--output-format", "json"}
}

// codexHeadlessArgs runs Codex non-interactively in a read-only sandbox (a
// drafting task must not edit the tree), final message written to outFile.
func codexHeadlessArgs(prompt, outFile string) []string {
	return []string{"exec", "--sandbox", "read-only", "--output-last-message", outFile, prompt}
}
