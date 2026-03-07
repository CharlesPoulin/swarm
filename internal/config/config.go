package config

import (
	"github.com/spf13/viper"
)

type Config struct {
	Num               int    `mapstructure:"num"`
	Session           string `mapstructure:"session"`
	BaseBranch        string `mapstructure:"base_branch"`
	CLIType           string `mapstructure:"cli_type"`
	CLIFlags          string `mapstructure:"cli_flags"`
	AddMode           bool   `mapstructure:"add_mode"`
	ResumeBufferSec   int    `mapstructure:"resume_buffer_secs"`
	MonitorInterval   int    `mapstructure:"monitor_interval"`
	WorktreePrefix    string `mapstructure:"worktree_prefix"`
	HubMode           string `mapstructure:"hub_mode"`
	ReviewRefreshSecs int    `mapstructure:"review_refresh_secs"`
	AssignmentMode    string `mapstructure:"assignment_mode"` // parallel|sequential|manual
	TicketsDir        string `mapstructure:"tickets_dir"`     // default: .swarm/tickets
}

// SetDefaults registers viper defaults.
func SetDefaults() {
	viper.SetDefault("num", 6)
	viper.SetDefault("session", "claude-swarm")
	viper.SetDefault("base_branch", "")
	viper.SetDefault("cli_type", "codex,codex,claude,gemini:gemini-3-flash-preview,gemini:gemini-3-flash-preview,spare")
	viper.SetDefault("cli_flags", "")
	viper.SetDefault("add_mode", false)
	viper.SetDefault("resume_buffer_secs", 120)
	viper.SetDefault("monitor_interval", 30)
	viper.SetDefault("worktree_prefix", ".wt")
	viper.SetDefault("hub_mode", "review")
	viper.SetDefault("review_refresh_secs", 30)
	viper.SetDefault("assignment_mode", "parallel")
	viper.SetDefault("tickets_dir", ".swarm/tickets")
}

// Load unmarshals viper settings into a Config.
func Load() (*Config, error) {
	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
