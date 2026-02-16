package guardrails

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds all guardrail thresholds and alert settings.
// If nil, guardrails are disabled and the gateway operates normally.
type Config struct {
	Budgets         BudgetConfig     `yaml:"budgets"`
	LoopDetection   LoopConfig       `yaml:"loop_detection"`
	ToolProtection  ToolConfig       `yaml:"tool_protection"`
	RetryProtection RetryConfig      `yaml:"retry_protection"`
	Alerts          AlertConfig      `yaml:"alerts"`
	Actions         ActionsConfig    `yaml:"actions"`
	Prevention      PreventionConfig   `yaml:"prevention"`
	Optimization    OptimizationConfig `yaml:"optimization"`
}

// OptimizationConfig holds performance analytics and model routing settings.
type OptimizationConfig struct {
	Analytics AnalyticsSubConfig `yaml:"analytics"`
	Router    RouterConfig       `yaml:"router"`
}

// AnalyticsSubConfig controls the performance analytics tracker.
type AnalyticsSubConfig struct {
	Enabled bool `yaml:"enabled"`
}

// RouterConfig controls automatic model routing based on analytics data.
type RouterConfig struct {
	Enabled bool          `yaml:"enabled"`
	Rules   []RoutingRule `yaml:"rules"`
}

// PreventionConfig holds policy enforcement settings.
// Prevention runs before detection and can modify or block requests.
type PreventionConfig struct {
	Tools       ToolFilterConfig `yaml:"tools"`
	PII         PIIConfig        `yaml:"pii"`
	ModelLimits ModelLimitConfig `yaml:"model_limits"`
	Approval    ApprovalConfig   `yaml:"approval"`
}

// ToolFilterConfig controls which tools agents can use.
type ToolFilterConfig struct {
	Enabled   bool     `yaml:"enabled"`
	Allowlist []string `yaml:"allowlist"` // if set, only these tools allowed
	Blocklist []string `yaml:"blocklist"` // if allowlist empty, block these
}

// PIIConfig controls PII detection and handling in prompts.
type PIIConfig struct {
	Enabled    bool   `yaml:"enabled"`
	BlockSSN   bool   `yaml:"block_ssn"`
	BlockCC    bool   `yaml:"block_cc"`
	BlockEmail bool   `yaml:"block_email"`
	BlockPhone bool   `yaml:"block_phone"`
	RedactMode string `yaml:"redact_mode"` // "block" or "redact"
}

// ModelLimitConfig controls cost-based model downgrading.
type ModelLimitConfig struct {
	Enabled          bool               `yaml:"enabled"`
	CostPerMToken    map[string]float64 `yaml:"cost_per_mtoken"`
	CostThresholdUSD float64            `yaml:"cost_threshold_usd"`
	DowngradeMap     map[string]string  `yaml:"downgrade_map"`
}

// ApprovalConfig controls human-in-the-loop approval for violations.
type ApprovalConfig struct {
	Enabled        bool     `yaml:"enabled"`
	WebhookURL     string   `yaml:"webhook_url"`
	TimeoutSeconds int      `yaml:"timeout_seconds"`
	Rules          []string `yaml:"rules"`         // which rules require approval
	FallbackAllow  bool     `yaml:"fallback_allow"` // true = allow on timeout
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

	// Prevention defaults
	if cfg.Prevention.PII.RedactMode == "" {
		cfg.Prevention.PII.RedactMode = "redact"
	}
	if cfg.Prevention.Approval.TimeoutSeconds == 0 {
		cfg.Prevention.Approval.TimeoutSeconds = 30
	}
}
