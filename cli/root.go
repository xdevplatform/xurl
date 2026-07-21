package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/xdevplatform/xurl/api"
	"github.com/xdevplatform/xurl/auth"
	"github.com/xdevplatform/xurl/config"
	"github.com/xdevplatform/xurl/version"
)

// Command group IDs used to organise the help output into scannable sections.
const (
	groupWrite  = "write"
	groupSocial = "social"
	groupRead   = "read"
	groupManage = "manage"
)

// CreateRootCommand creates the root command for the xurl CLI
func CreateRootCommand(cfg *config.Config, a *auth.Auth) *cobra.Command {
	var rootCmd = &cobra.Command{
		Use:     "xurl [flags] URL",
		Short:   "Auth enabled curl-like interface for the X API",
		Version: version.Version,
		Long: `A command-line tool for making authenticated requests to the X API.

Quick start:
  xurl post "Hello world!"                    Post to X (shortcut command)
  xurl /2/users/me                            Raw GET request
  xurl -X POST /2/tweets -d '{"text":"hi"}'   Raw request with a JSON body
  xurl --auth app /2/tweets/search/stream     Pick an auth type (oauth1|oauth2|app)
  xurl media upload photo.jpg                 Upload media (type auto-detected)

Authentication:
  xurl auth apps add my-app --client-id ... --client-secret ...
  xurl auth oauth2 --app my-app               Authenticate a user
  xurl auth default my-app                    Set the default app
  xurl --app my-app /2/users/me               Per-request app override

Commands are grouped by purpose below. Run 'xurl <command> --help' for details.`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Apply --app override if provided
			appOverride, _ := cmd.Flags().GetString("app")
			if appOverride != "" {
				a.WithAppName(appOverride)
			}
		},
		Args: func(cmd *cobra.Command, args []string) error {
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			headers, _ := cmd.Flags().GetStringArray("header")
			data, _ := cmd.Flags().GetString("data")

			method, _ := cmd.Flags().GetString("method")
			if method == "" {
				// Mirror curl: providing a request body (-d/--data) implies POST
				// unless -X says otherwise — even for an explicitly empty body.
				if cmd.Flags().Changed("data") {
					method = "POST"
				} else {
					method = "GET"
				}
			}

			authType, _ := cmd.Flags().GetString("auth")
			username, _ := cmd.Flags().GetString("username")
			verbose, _ := cmd.Flags().GetBool("verbose")
			trace, _ := cmd.Flags().GetBool("trace")
			forceStream, _ := cmd.Flags().GetBool("stream")
			mediaFile, _ := cmd.Flags().GetString("file")

			if len(args) == 0 {
				fmt.Fprintln(os.Stderr, "No URL provided")
				fmt.Fprintln(os.Stderr, "Usage: xurl [OPTIONS] [URL] [COMMAND]")
				fmt.Fprintln(os.Stderr, "Try 'xurl --help' for more information.")
				os.Exit(1)
			}

			url := args[0]

			client := api.NewApiClient(cfg, a)

			requestOptions := api.RequestOptions{
				Method:   method,
				Endpoint: url,
				Headers:  headers,
				Data:     data,
				AuthType: authType,
				Username: username,
				Verbose:  verbose,
				Trace:    trace,
			}
			err := api.HandleRequest(requestOptions, forceStream, mediaFile, client)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
		},
	}

	// Global persistent flag: --app
	rootCmd.PersistentFlags().String("app", "", "Use a specific registered app (overrides default)")

	rootCmd.Flags().StringP("method", "X", "", "HTTP method (GET by default, POST when -d is given)")
	rootCmd.Flags().StringArrayP("header", "H", []string{}, "Request headers")
	rootCmd.Flags().StringP("data", "d", "", "Request body data")
	rootCmd.Flags().String("auth", "", "Authentication type (oauth1, oauth2, or app)")
	rootCmd.Flags().StringP("username", "u", "", "Username for OAuth2 authentication")
	rootCmd.Flags().BoolP("verbose", "v", false, "Print verbose information")
	rootCmd.Flags().BoolP("trace", "t", false, "Add trace header to request")
	rootCmd.Flags().BoolP("stream", "s", false, "Force streaming mode for non-streaming endpoints")
	rootCmd.Flags().StringP("file", "F", "", "File to upload (for multipart requests)")

	// Organise subcommands into scannable help sections.
	rootCmd.AddGroup(
		&cobra.Group{ID: groupWrite, Title: "Posting & Engagement:"},
		&cobra.Group{ID: groupSocial, Title: "Users & Social Graph:"},
		&cobra.Group{ID: groupRead, Title: "Reading & Lists:"},
		&cobra.Group{ID: groupManage, Title: "Management:"},
	)

	chatCmd := CreateChatCommand(a)
	chatCmd.GroupID = groupWrite
	rootCmd.AddCommand(chatCmd)

	authCmd := CreateAuthCommand(a)
	mediaCmd := CreateMediaCommand(a)
	versionCmd := CreateVersionCommand()
	webhookCmd := CreateWebhookCommand(a)
	tokenCmd := CreateTokenCommand(a)
	mcpCmd := CreateMCPCommand(a)
	for _, c := range []*cobra.Command{authCmd, mediaCmd, tokenCmd, mcpCmd, versionCmd, webhookCmd} {
		c.GroupID = groupManage
		rootCmd.AddCommand(c)
	}

	// Place the auto-generated help/completion commands in the Management group
	// too, so the help screen has no ungrouped "Additional Commands" section.
	rootCmd.SetHelpCommandGroupID(groupManage)
	rootCmd.SetCompletionCommandGroupID(groupManage)

	// Register streamlined shortcut commands (post, reply, read, search, etc.)
	CreateShortcutCommands(rootCmd, a)

	return rootCmd
}
