package cli

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/xdevplatform/xurl/api"
	"github.com/xdevplatform/xurl/auth"
	"github.com/xdevplatform/xurl/bot"
	"github.com/xdevplatform/xurl/config"
)

// CreateBotCommand creates the top-level "bot" command.
func CreateBotCommand(a *auth.Auth) *cobra.Command {
	botCmd := &cobra.Command{
		Use:   "bot",
		Short: "AI-powered bug fix bot that monitors X for bug reports",
		Long: `Monitor X for trigger tweets and automatically fix bugs using coding agents.

How it works:
  1. A user reports a bug on X (normal tweet)
  2. You reply with "fix: <description>" to trigger the bot
  3. The bot runs a coding agent to fix the bug
  4. A PR is created and the bot replies with the link

Setup:
  xurl bot init --handle your_handle --repo /path/to/project
  xurl bot start

Testing:
  xurl bot run <tweet-url> --dry-run`,
	}

	botCmd.AddCommand(botInitCmd())
	botCmd.AddCommand(botStartCmd(a))
	botCmd.AddCommand(botRunCmd(a))
	botCmd.AddCommand(botStatusCmd())

	return botCmd
}

// ─── bot init ───────────────────────────────────────────────────

func botInitCmd() *cobra.Command {
	var (
		handle       string
		repo         string
		agent        string
		agentCmd     string
		pollInterval time.Duration
		branchPrefix string
		triggerKw    string
		dryRun       bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize bot configuration",
		Long: `Set up the bot configuration. Creates ~/.xurl-bot with your settings.

Examples:
  xurl bot init --handle shallum --repo /path/to/project --agent claude
  xurl bot init --handle shallum --repo . --agent codex --trigger "bug:"
  xurl bot init --handle shallum --repo . --trigger "bug:"`,
		Run: func(cmd *cobra.Command, args []string) {
			if handle == "" {
				fmt.Fprintf(os.Stderr, "\033[31mError: --handle is required\033[0m\n")
				os.Exit(1)
			}

			// Resolve relative repo path
			repoPath := repo
			if repoPath == "." || repoPath == "" {
				wd, err := os.Getwd()
				if err != nil {
					fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
					os.Exit(1)
				}
				repoPath = wd
			} else {
				absPath, err := filepath.Abs(repoPath)
				if err == nil {
					repoPath = absPath
				}
			}

			cfg := &bot.BotConfig{
				Handle:         handle,
				TriggerKeyword: triggerKw,
				Repo:           repoPath,
				Agent:          agent,
				AgentCmd:       agentCmd,
				PollInterval:   pollInterval,
				BranchPrefix:   branchPrefix,
				DryRun:         dryRun,
			}

			// Validate config before saving
			if err := cfg.Validate(); err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mConfig error: %v\033[0m\n", err)
				os.Exit(1)
			}

			if err := cfg.Save(); err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}

			fmt.Printf("\033[32mBot configured!\033[0m\n\n")
			fmt.Printf("  Handle:    @%s\n", cfg.Handle)
			fmt.Printf("  Trigger:   %s\n", cfg.TriggerKeyword)
			fmt.Printf("  Repo:      %s\n", cfg.Repo)
			fmt.Printf("  Agent:     %s\n", cfg.Agent)
			fmt.Printf("  Interval:  %s\n", cfg.PollInterval)

			fmt.Printf("\nRun '\033[1mxurl bot start\033[0m' to begin monitoring.\n")
		},
	}

	cmd.Flags().StringVar(&handle, "handle", "", "Your X handle to monitor for triggers (required)")
	cmd.Flags().StringVar(&repo, "repo", ".", "Path to target git repository")
	cmd.Flags().StringVar(&agent, "agent", "claude", "Coding agent: claude, codex, gemini, custom")
	cmd.Flags().StringVar(&agentCmd, "agent-cmd", "", "Custom agent command (for --agent=custom)")
	cmd.Flags().DurationVar(&pollInterval, "poll-interval", 60*time.Second, "Polling interval")
	cmd.Flags().StringVar(&branchPrefix, "branch-prefix", "bot/fix-", "Git branch prefix")
	cmd.Flags().StringVar(&triggerKw, "trigger", "fix:", "Keyword trigger (e.g. 'fix:', 'bug:')")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Log only, don't execute agent or reply")

	return cmd
}

// ─── bot start ──────────────────────────────────────────────────

func botStartCmd(a *auth.Auth) *cobra.Command {
	var (
		dryRun       bool
		once         bool
		pollInterval time.Duration
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the bot polling loop",
		Long: `Start monitoring X for trigger tweets and processing bug reports.

Examples:
  xurl bot start
  xurl bot start --once
  xurl bot start --dry-run
  xurl bot start --poll-interval 30s`,
		Run: func(cmd *cobra.Command, args []string) {
			cfg, err := bot.LoadConfig()
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}

			// Apply CLI overrides
			if cmd.Flags().Changed("dry-run") {
				cfg.DryRun = dryRun
			}
			if cmd.Flags().Changed("poll-interval") {
				cfg.PollInterval = pollInterval
			}

			// Validate config
			if err := cfg.Validate(); err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mConfig error: %v\033[0m\n", err)
				os.Exit(1)
			}

			// Validate agent
			agent, err := bot.NewAgent(cfg.Agent, cfg.AgentCmd)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}

			// Create API client
			xCfg := config.NewConfig()
			client := api.NewApiClient(xCfg, a)
			opts := baseOpts(cmd)
			// H2: Force verbose off in bot mode to prevent auth header leakage
			opts.Verbose = false

			// Load state
			state := bot.LoadState()

			logger := log.New(os.Stdout, "", log.LstdFlags)

			handler := &bot.Handler{
				Config: cfg,
				State:  state,
				Client: client,
				Opts:   opts,
				Agent:  agent,
				Logger: logger,
			}

			poller := &bot.Poller{
				Config:  cfg,
				State:   state,
				Client:  client,
				Opts:    opts,
				Handler: handler,
				Logger:  logger,
			}

			if once {
				if err := poller.RunOnce(context.Background()); err != nil {
					fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
					os.Exit(1)
				}
				return
			}

			// Graceful shutdown
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			if err := poller.Run(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Log only, don't execute agent or reply")
	cmd.Flags().BoolVar(&once, "once", false, "Poll once and exit")
	cmd.Flags().DurationVar(&pollInterval, "poll-interval", 0, "Override poll interval")
	addCommonFlags(cmd)

	return cmd
}

// ─── bot run ────────────────────────────────────────────────────

func botRunCmd(a *auth.Auth) *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "run TWEET_ID_OR_URL",
		Short: "Process a single tweet through the bot pipeline",
		Long: `Process a specific tweet. Useful for testing the full pipeline.

Examples:
  xurl bot run 1234567890
  xurl bot run https://x.com/user/status/1234567890
  xurl bot run 1234567890 --dry-run`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			cfg, err := bot.LoadConfig()
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}

			if cmd.Flags().Changed("dry-run") {
				cfg.DryRun = dryRun
			}

			// Validate config
			if err := cfg.Validate(); err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mConfig error: %v\033[0m\n", err)
				os.Exit(1)
			}

			agent, err := bot.NewAgent(cfg.Agent, cfg.AgentCmd)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}

			xCfg := config.NewConfig()
			client := api.NewApiClient(xCfg, a)
			opts := baseOpts(cmd)
			// H2: Force verbose off in bot mode to prevent auth header leakage
			opts.Verbose = false

			logger := log.New(os.Stdout, "", log.LstdFlags)

			// Fetch the tweet
			tweetID := api.ResolvePostID(args[0])
			logger.Printf("[BOT] Fetching tweet %s...", tweetID)

			resp, err := api.ReadPostWithMedia(client, tweetID, opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError fetching tweet: %v\033[0m\n", err)
				os.Exit(1)
			}

			tweet, err := bot.ParseSingleTweet(resp)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError parsing tweet: %v\033[0m\n", err)
				os.Exit(1)
			}

			// Set bug description from trigger keyword
			tweet.BugDescription = bot.ExtractBugDesc(tweet.Text, cfg.TriggerKeyword)

			state := bot.LoadState()

			handler := &bot.Handler{
				Config: cfg,
				State:  state,
				Client: client,
				Opts:   opts,
				Agent:  agent,
				Logger: logger,
			}

			if err := handler.Process(context.Background(), *tweet); err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Log only, don't execute agent")
	addCommonFlags(cmd)

	return cmd
}

// ─── bot status ─────────────────────────────────────────────────

func botStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show bot configuration and state",
		Long: `Display the current bot configuration and polling state.

Examples:
  xurl bot status`,
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			cfg, err := bot.LoadConfig()
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}

			fmt.Println("\033[1mBot Configuration\033[0m")
			fmt.Printf("  Handle:         @%s\n", cfg.Handle)
			fmt.Printf("  Trigger:        %s\n", cfg.TriggerKeyword)
			fmt.Printf("  Repo:           %s\n", cfg.Repo)
			fmt.Printf("  Agent:          %s\n", cfg.Agent)
			if cfg.AgentCmd != "" {
				fmt.Printf("  Agent Cmd:      %s\n", cfg.AgentCmd)
			}
			fmt.Printf("  Poll Interval:  %s\n", cfg.PollInterval)
			fmt.Printf("  Branch Prefix:  %s\n", cfg.BranchPrefix)
			fmt.Printf("  Dry Run:        %v\n", cfg.DryRun)

			state := bot.LoadState()
			fmt.Println("\n\033[1mBot State\033[0m")
			fmt.Printf("  Since ID:       %s\n", valueOr(state.SinceID, "(none)"))
			if !state.LastPollTime.IsZero() {
				fmt.Printf("  Last Poll:      %s\n", state.LastPollTime.Format(time.RFC3339))
			} else {
				fmt.Printf("  Last Poll:      (never)\n")
			}
			fmt.Printf("  Processed:      %d tweet(s)\n", len(state.ProcessedIDs))

			// Show recent processed tweets
			if len(state.ProcessedIDs) > 0 {
				fmt.Println("\n  Recent:")
				count := 0
				for id, status := range state.ProcessedIDs {
					if count >= 5 {
						fmt.Printf("  ... and %d more\n", len(state.ProcessedIDs)-5)
						break
					}
					fmt.Printf("    %s → %s\n", id, status)
					count++
				}
			}
		},
	}

	return cmd
}

func valueOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
