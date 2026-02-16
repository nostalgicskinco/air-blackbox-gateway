package guardrails

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds all guardrail thresholds and alert settings.
// If nil, guardrails are disabled and the gateway operates normally.
type Config struct {
	Budgets         BudgetConfig  `yaml:"budgets"`
	LoopDetection   LoopConfig    `yaml:"loop_detection"`
	ToolProtection  ToolConfig    `yaml:"tool_protection"`
	RetryProtection RetryConfig   `yaml:"retry_protection"`
	Alerts          AlertConfig   `yaml:"alerts"`
	Actions         ActionsConfig `yaml:"actions"`
}

// BudgetConfig sets token and cost limits per session.
type BudgetConfig struct {
	MaxSessionTokens  int     `yaml:"max_session_tokens"`
	MaxSessionCostUSD float64 `yaml:"max_session_cost_usd"`
}

// LoopConfig controls prompt loop detection.
type LoopConfig struct {
	SimilarPromptThreshold float64 `yaml:"similar_prompt_threshold"`
	MaxSimilarPrompts      int     `yaml:"max_similar_prompts"`
	WindowSeconds          int     `yaml:"window_seconds"`
}

// ToolConfig controls tool retry storm detection.
type ToolConfig struct {
	MaxRepeatCalls      int `yaml:"max_repeat_calls"`
	RepeatWindowSeconds int `yaml:"repeat_window_seconds"`
}

// RetryConfig controls error retry spiral detection.
type RetryConfig struct {
	MaxConsecutiveErrors int `yaml:"max_consecutive_errors"`
}

// AlertConfig controls where alerts are sent.
type AlertConfig struct {
	WebhookURL string `yaml:"webhook_url"`
}

// ActionsConfig controls what happens when a guardrail triggers.
type ActionsConfig struct {
	OnTrigger []string `yaml:"on_trigger"`
}

// LoadConfig reads a guardrails YAML file. Returns nil if path is empty
// (guardrails disabled). Returns an error if the file exists but is invalid.
func LoadConfig(path string) (*Config, error) {
	if path == "" {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	// Apply defaults for any unset values
	applyDefaults(cfg)

	return cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Budgets.MaxSessionTokens == 0 {
		cfg.Budgets.MaxSessionTokens = 80000
	}
	if cfg.LoopDetection.SimilarPromptThreshold == 0 {
		cfg.LoopDetection.SimilarPromptThreshold = 0.80
	}
	if cfg.LoopDetection.MaxSimilarPrompts == 0 {
		cfg.LoopDetection.MaxSimilarPrompts = 5
	}
	if cfg.LoopDetection.WindowSeconds == 0 {
		cfg.LoopDetection.WindowSeconds = 60
	}
	if cfg.ToolProtection.MaxRepeatCalls == 0 {
		cfg.ToolProtection.MaxRepeatCalls = 3
	}
	if cfg.ToolProtection.RepeatWindowSeconds == 0 {
		cfg.ToolProtection.RepeatWindowSeconds = 30
	}
	if cfg.RetryProtection.MaxConsecutiveErrors == 0 {
		cfg.RetryProtection.MaxConsecutiveErrors = 3
	}
}
