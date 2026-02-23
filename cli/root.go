package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/xdevplatform/xurl/api"
	"github.com/xdevplatform/xurl/auth"
	"github.com/xdevplatform/xurl/config"
)

// CreateRootCommand creates the root command for the xurl CLI
func CreateRootCommand(cfg *config.Config, a *auth.Auth) *cobra.Command {
	var rootCmd = &cobra.Command{
		Use:   "xurl [flags] URL",
		Short: "Auth enabled curl-like interface for the X API",
		Long: `A command-line tool for making authenticated requests to the X API.

Shortcut commands (agent‑friendly):
  xurl post "Hello world!"                        Post to X
  xurl reply 1234567890 "Nice!"                   Reply to a post
  xurl read 1234567890                             Read a post
  xurl search "golang" -n 20                       Search posts
  xurl whoami                                      Show your profile
  xurl like 1234567890                             Like a post
  xurl repost 1234567890                           Repost
  xurl follow @user                                Follow a user
  xurl dm @user "Hey!"                             Send a DM
  xurl timeline                                    Home timeline
  xurl mentions                                    Your mentions

Raw API access (curl‑style):
  basic requests        xurl /2/users/me
                        xurl -X POST /2/tweets -d '{"text":"Hello world!"}'
                        xurl -H "Content-Type: application/json" /2/tweets
  authentication        xurl --auth oauth2 /2/users/me
                        xurl --auth oauth1 /2/users/me
                        xurl --auth app /2/users/me
  media and streaming   xurl media upload path/to/video.mp4
                        xurl /2/tweets/search/stream --auth app
                        xurl -s /2/users/me

Multi-app management:
  xurl auth apps add my-app --client-id ... --client-secret ...
  xurl auth apps list
  xurl auth default                                # interactive picker
  xurl auth default my-app                         # set by name
  xurl --app my-app /2/users/me                    # per-request override

Run 'xurl --help' to see all available commands.`,
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
			method, _ := cmd.Flags().GetString("method")
			if method == "" {
				method = "GET"
			}

			headers, _ := cmd.Flags().GetStringArray("header")
			data, _ := cmd.Flags().GetString("data")
			authType, _ := cmd.Flags().GetString("auth")
			username, _ := cmd.Flags().GetString("username")
			verbose, _ := cmd.Flags().GetBool("verbose")
			trace, _ := cmd.Flags().GetBool("trace")
			forceStream, _ := cmd.Flags().GetBool("stream")
			mediaFile, _ := cmd.Flags().GetString("file")

			if len(args) == 0 {
				fmt.Println("No URL provided")
				fmt.Println("Usage: xurl [OPTIONS] [URL] [COMMAND]")
				fmt.Println("Try 'xurl --help' for more information.")
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
				fmt.Printf("\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
		},
	}

	// Global persistent flag: --app
	rootCmd.PersistentFlags().String("app", "", "Use a specific registered app (overrides default)")

	rootCmd.Flags().StringP("method", "X", "", "HTTP method (GET by default)")
	rootCmd.Flags().StringArrayP("header", "H", []string{}, "Request headers")
	rootCmd.Flags().StringP("data", "d", "", "Request body data")
	rootCmd.Flags().String("auth", "", "Authentication type (oauth1 or oauth2)")
	rootCmd.Flags().StringP("username", "u", "", "Username for OAuth2 authentication")
	rootCmd.Flags().BoolP("verbose", "v", false, "Print verbose information")
	rootCmd.Flags().BoolP("trace", "t", false, "Add trace header to request")
	rootCmd.Flags().BoolP("stream", "s", false, "Force streaming mode for non-streaming endpoints")
	rootCmd.Flags().StringP("file", "F", "", "File to upload (for multipart requests)")

	rootCmd.AddCommand(CreateAuthCommand(a))
	rootCmd.AddCommand(CreateMediaCommand(a))
	rootCmd.AddCommand(CreateVersionCommand())
	rootCmd.AddCommand(CreateWebhookCommand(a))
	rootCmd.AddCommand(CreateBotCommand(a))

	// Register streamlined shortcut commands (post, reply, read, search, etc.)
	CreateShortcutCommands(rootCmd, a)

	return rootCmd
}
