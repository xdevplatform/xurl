package xurl

import (
	"github.com/spf13/cobra"

	"github.com/xdevplatform/xurl/auth"
	"github.com/xdevplatform/xurl/cli"
	"github.com/xdevplatform/xurl/config"
)

// NewRootCommand creates the root Cobra command with default configuration.
func NewRootCommand() *cobra.Command {
	cfg := config.NewConfig()
	a := auth.NewAuth(cfg)

	return cli.CreateRootCommand(cfg, a)
}

// CreateRootCommand creates the root Cobra command using caller-provided dependencies.
func CreateRootCommand(cfg *config.Config, a *auth.Auth) *cobra.Command {
	return cli.CreateRootCommand(cfg, a)
}

// Execute runs the root command.
func Execute() error {
	return NewRootCommand().Execute()
}
