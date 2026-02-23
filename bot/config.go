package bot

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Validation constants.
const (
	maxHandleLen         = 15
	maxTriggerKeywordLen = 50
	maxBranchPrefixLen   = 50
	maxAgentCmdLen       = 500
)

var validHandle = regexp.MustCompile(`^[a-zA-Z0-9_]{1,15}$`)

// BotConfig holds the configuration for the bot.
type BotConfig struct {
	Handle         string        `yaml:"handle"`
	TriggerKeyword string        `yaml:"trigger_keyword"`
	Repo           string        `yaml:"repo"`
	Agent          string        `yaml:"agent"`
	AgentCmd       string        `yaml:"agent_cmd,omitempty"`
	PollInterval   time.Duration `yaml:"poll_interval"`
	BranchPrefix   string        `yaml:"branch_prefix"`
	DryRun         bool          `yaml:"dry_run"`
}

// DefaultConfigPath returns the default path for the bot config file.
func DefaultConfigPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "."
	}
	return filepath.Join(homeDir, ".xurl-bot")
}

// LoadConfig reads the bot config from ~/.xurl-bot.
func LoadConfig() (*BotConfig, error) {
	return LoadConfigFromPath(DefaultConfigPath())
}

// LoadConfigFromPath reads the bot config from the given path.
func LoadConfigFromPath(path string) (*BotConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("bot not configured – run 'xurl bot init' first")
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg BotConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	cfg.applyDefaults()
	return &cfg, nil
}

// Save writes the bot config to ~/.xurl-bot.
func (c *BotConfig) Save() error {
	return c.SaveToPath(DefaultConfigPath())
}

// SaveToPath writes the bot config to the given path.
func (c *BotConfig) SaveToPath(path string) error {
	c.applyDefaults()

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

func (c *BotConfig) applyDefaults() {
	c.Handle = strings.TrimPrefix(c.Handle, "@")

	if c.TriggerKeyword == "" {
		c.TriggerKeyword = "fix:"
	}
	if c.PollInterval == 0 {
		c.PollInterval = 60 * time.Second
	}
	if c.BranchPrefix == "" {
		c.BranchPrefix = "bot/fix-"
	}
	if c.Agent == "" {
		c.Agent = "claude"
	}
}

// Validate checks the config for security and correctness issues.
func (c *BotConfig) Validate() error {
	// M1: Validate handle format
	if c.Handle == "" {
		return fmt.Errorf("handle is required")
	}
	if !validHandle.MatchString(c.Handle) {
		return fmt.Errorf("invalid handle %q: must be 1-%d alphanumeric/underscore characters", c.Handle, maxHandleLen)
	}

	// M2: Validate trigger keyword — no shell-dangerous chars, bounded length
	if len(c.TriggerKeyword) > maxTriggerKeywordLen {
		return fmt.Errorf("trigger keyword too long (max %d characters)", maxTriggerKeywordLen)
	}
	if strings.ContainsAny(c.TriggerKeyword, "\"'`\\$") {
		return fmt.Errorf("trigger keyword contains unsafe characters (quotes, backslashes, or dollar signs)")
	}

	// M4: Validate repo path — must be absolute and exist as a directory
	if c.Repo != "" {
		if !filepath.IsAbs(c.Repo) {
			return fmt.Errorf("repo path must be absolute: %s", c.Repo)
		}
		info, err := os.Stat(c.Repo)
		if err != nil {
			return fmt.Errorf("repo path does not exist: %s", c.Repo)
		}
		if !info.IsDir() {
			return fmt.Errorf("repo path is not a directory: %s", c.Repo)
		}
	}

	// L5: Validate branch prefix length
	if len(c.BranchPrefix) > maxBranchPrefixLen {
		return fmt.Errorf("branch prefix too long (max %d characters)", maxBranchPrefixLen)
	}

	// C1: Validate custom agent command — reject shell metacharacters
	if c.Agent == "custom" {
		if c.AgentCmd == "" {
			return fmt.Errorf("custom agent requires agent_cmd to be set")
		}
		if len(c.AgentCmd) > maxAgentCmdLen {
			return fmt.Errorf("agent_cmd too long (max %d characters)", maxAgentCmdLen)
		}
		if strings.ContainsAny(c.AgentCmd, ";|&$`\\(){}") {
			return fmt.Errorf("agent_cmd contains unsafe shell metacharacters (;|&$`\\(){})")
		}
	}

	// Validate agent type
	validAgents := map[string]bool{"claude": true, "codex": true, "gemini": true, "custom": true}
	if !validAgents[strings.ToLower(c.Agent)] {
		return fmt.Errorf("unknown agent type: %s (supported: claude, codex, gemini, custom)", c.Agent)
	}

	return nil
}
