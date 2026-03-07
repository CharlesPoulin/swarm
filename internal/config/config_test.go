package config

import (
	"testing"

	"github.com/spf13/viper"
)

func TestSetDefaults_PM_BootstrapMode(t *testing.T) {
	viper.Reset()
	SetDefaults()

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		t.Fatalf("unmarshal config defaults: %v", err)
	}

	if cfg.PMBootstrapMode != "prompt" {
		t.Fatalf("PMBootstrapMode=%q, want %q", cfg.PMBootstrapMode, "prompt")
	}
}
