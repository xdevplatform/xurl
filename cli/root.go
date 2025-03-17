package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"xurl/api"
	"xurl/auth"
	"xurl/config"
)

// CreateRootCommand creates the root command for the xurl CLI
func CreateRootCommand(config *config.Config, auth *auth.Auth) *cobra.Command {
	var rootCmd = &cobra.Command{
		Use:   "xurl [flags] URL",
		Short: "Auth enabled curl-like interface for the X API",
		Long: `A command-line tool for making authenticated requests to the X API.

Examples:
  basic requests        xurl /2/users/me
                        xurl -X POST /2/tweets -d '{"text":"Hello world!"}'
                        xurl -H "Content-Type: application/json"/2/tweets
  authentication        xurl --auth oauth2 /2/users/me
                        xurl --auth oauth1 /2/users/me
                        xurl --auth app /2/users/me
  media and streaming   xurl media upload path/to/video.mp4
                        xurl /2/tweets/search/stream --auth app
                        xurl -s /2/users/me`,
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
			forceStream, _ := cmd.Flags().GetBool("stream")
			mediaFile, _ := cmd.Flags().GetString("file")

			if len(args) == 0 {
				fmt.Println("No URL provided")
				fmt.Println("Usage: xurl [OPTIONS] [URL] [COMMAND]")
				fmt.Println("Try 'xurl --help' for more information.")
				os.Exit(1)
			}

			url := args[0]

			client := api.NewApiClient(config, auth)

			err := api.HandleRequest(method, url, headers, data, authType, username, verbose, forceStream, mediaFile, client)
			if err != nil {
				fmt.Printf("\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
		},
	}

	rootCmd.Flags().StringP("method", "X", "", "HTTP method (GET by default)")
	rootCmd.Flags().StringArrayP("header", "H", []string{}, "Request headers")
	rootCmd.Flags().StringP("data", "d", "", "Request body data")
	rootCmd.Flags().String("auth", "", "Authentication type (oauth1 or oauth2)")
	rootCmd.Flags().StringP("username", "u", "", "Username for OAuth2 authentication")
	rootCmd.Flags().BoolP("verbose", "v", false, "Print verbose information")
	rootCmd.Flags().BoolP("trace", "t", false, "Add trace header to request")
	rootCmd.Flags().BoolP("stream", "s", false, "Force streaming mode for non-streaming endpoints")
	rootCmd.Flags().StringP("file", "F", "", "File to upload (for multipart requests)")

	rootCmd.AddCommand(CreateAuthCommand(auth))
	rootCmd.AddCommand(CreateMediaCommand(auth))
	rootCmd.AddCommand(CreateVersionCommand())

	return rootCmd
}
