package guardrails

// filterTools applies allowlist/blocklist policies to a set of tool names.
//
// Logic:
//   - If Allowlist is non-empty, only tools in the allowlist are kept.
//   - Otherwise, if Blocklist is non-empty, tools in the blocklist are removed.
//   - If neither is set, all tools pass through unchanged.
func filterTools(cfg ToolFilterConfig, tools []string) []string {
	if !cfg.Enabled || len(tools) == 0 {
		return tools
	}

	// Allowlist mode: only explicitly permitted tools survive.
	if len(cfg.Allowlist) > 0 {
		allowed := make(map[string]bool, len(cfg.Allowlist))
		for _, t := range cfg.Allowlist {
			allowed[t] = true
		}
		var result []string
		for _, tool := range tools {
			if allowed[tool] {
				result = append(result, tool)
			}
		}
		return result
	}

	// Blocklist mode: remove forbidden tools.
	if len(cfg.Blocklist) > 0 {
		blocked := make(map[string]bool, len(cfg.Blocklist))
		for _, t := range cfg.Blocklist {
			blocked[t] = true
		}
		var result []string
		for _, tool := range tools {
			if !blocked[tool] {
				result = append(result, tool)
			}
		}
		return result
	}

	return tools
}
