package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"xurl/api"
	"xurl/auth"
	"xurl/config"
	"xurl/store"
)

func main() {
	// Create a new config from environment variables
	config := config.NewConfig()
	auth := auth.NewAuth(config)

	// Create the root command
	var rootCmd = &cobra.Command{
		Use:   "xurl [flags] URL",
		Short: "Auth enabled curl-like interface for the X API",
		Long:  `A command-line tool for making authenticated requests to the X API.`,
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

			// Check if URL is provided
			if len(args) == 0 {
				fmt.Println("No URL provided")
				fmt.Println("Usage: xurl [OPTIONS] [URL] [COMMAND]")
				fmt.Println("Try 'xurl --help' for more information.")
				os.Exit(1)
			}

			url := args[0]
			
			// Create API client
			client := api.NewApiClient(config, auth)

			// Handle the request
			err := api.HandleRequest(method, url, headers, data, authType, username, verbose, forceStream, mediaFile, client)
			if err != nil {
				fmt.Printf("\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
		},
	}

	// Add flags to root command
	rootCmd.Flags().StringP("method", "X", "", "HTTP method (GET by default)")
	rootCmd.Flags().StringArrayP("header", "H", []string{}, "Request headers")
	rootCmd.Flags().StringP("data", "d", "", "Request body data")
	rootCmd.Flags().String("auth", "", "Authentication type (oauth1 or oauth2)")
	rootCmd.Flags().StringP("username", "u", "", "Username for OAuth2 authentication")
	rootCmd.Flags().BoolP("verbose", "v", false, "Print verbose information")
	rootCmd.Flags().BoolP("stream", "s", false, "Force streaming mode for non-streaming endpoints")
	rootCmd.Flags().StringP("file", "F", "", "File to upload (for multipart requests)")

	// Create auth command
	var authCmd = &cobra.Command{
		Use:   "auth",
		Short: "Authentication management",
	}

	// Add auth subcommands
	authCmd.AddCommand(createAuthAppCmd(auth))
	authCmd.AddCommand(createAuthOAuth2Cmd(auth))
	authCmd.AddCommand(createAuthOAuth1Cmd(auth))
	authCmd.AddCommand(createAuthStatusCmd())
	authCmd.AddCommand(createAuthClearCmd(auth))

	// Add auth command to root
	rootCmd.AddCommand(authCmd)

	// Create media command
	var mediaCmd = &cobra.Command{
		Use:   "media",
		Short: "Media upload operations",
	}

	// Add media subcommands
	mediaCmd.AddCommand(createMediaUploadCmd(auth))
	mediaCmd.AddCommand(createMediaStatusCmd(auth))

	// Add media command to root
	rootCmd.AddCommand(mediaCmd)

	// Execute the command
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

// Create auth app subcommand
func createAuthAppCmd(auth *auth.Auth) *cobra.Command {
	var bearerToken string
	
	cmd := &cobra.Command{
		Use:   "app",
		Short: "Configure app-auth",
		Run: func(cmd *cobra.Command, args []string) {
			err := auth.TokenStore.SaveBearerToken(bearerToken)
			if err != nil {
				fmt.Println("Error saving bearer token:", err)
				os.Exit(1)
			}
			fmt.Printf("\033[32mApp authentication successful!\033[0m\n")
		},
	}
	
	cmd.Flags().StringVar(&bearerToken, "bearer-token", "", "Bearer token for app authentication")
	cmd.MarkFlagRequired("bearer-token")
	
	return cmd
}

// Create auth oauth2 subcommand
func createAuthOAuth2Cmd(auth *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "oauth2",
		Short: "Configure OAuth2 authentication",
		Run: func(cmd *cobra.Command, args []string) {
			_, err := auth.OAuth2Flow("")
			if err != nil {
				fmt.Println("OAuth2 authentication failed:", err)
				os.Exit(1)
			}
			fmt.Printf("\033[32mOAuth2 authentication successful!\033[0m\n")
		},
	}
	
	return cmd
}

// Create auth oauth1 subcommand
func createAuthOAuth1Cmd(auth *auth.Auth) *cobra.Command {
	var consumerKey, consumerSecret, accessToken, tokenSecret string
	
	cmd := &cobra.Command{
		Use:   "oauth1",
		Short: "Configure OAuth1 authentication",
		Run: func(cmd *cobra.Command, args []string) {
			err := auth.TokenStore.SaveOAuth1Tokens(accessToken, tokenSecret, consumerKey, consumerSecret)
			if err != nil {
				fmt.Println("Error saving OAuth1 tokens:", err)
				os.Exit(1)
			}
			fmt.Printf("\033[32mOAuth1 credentials saved successfully!\033[0m\n")
		},
	}
	
	cmd.Flags().StringVar(&consumerKey, "consumer-key", "", "Consumer key for OAuth1")
	cmd.Flags().StringVar(&consumerSecret, "consumer-secret", "", "Consumer secret for OAuth1")
	cmd.Flags().StringVar(&accessToken, "access-token", "", "Access token for OAuth1")
	cmd.Flags().StringVar(&tokenSecret, "token-secret", "", "Token secret for OAuth1")
	
	cmd.MarkFlagRequired("consumer-key")
	cmd.MarkFlagRequired("consumer-secret")
	cmd.MarkFlagRequired("access-token")
	cmd.MarkFlagRequired("token-secret")
	
	return cmd
}

// Create auth status subcommand
func createAuthStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show authentication status",
		Run: func(cmd *cobra.Command, args []string) {
			store := store.NewTokenStore()
			
			fmt.Println("OAuth2 Accounts:")
			if len(store.GetOAuth2Usernames()) == 0 {
				fmt.Println("No OAuth2 accounts configured")
			} else {
				for _, username := range store.GetOAuth2Usernames() {
					fmt.Println("-", username)
				}
			}
			
			hasOAuth1 := "Not configured"
			if store.HasOAuth1Tokens() {
				hasOAuth1 = "Configured"
			}
			fmt.Println("OAuth1:", hasOAuth1)
			
			hasBearer := "Not configured"
			if store.HasBearerToken() {
				hasBearer = "Configured"
			}
			fmt.Println("App Auth:", hasBearer)
		},
	}
	
	return cmd
}

// Create auth clear subcommand
func createAuthClearCmd(auth *auth.Auth) *cobra.Command {
	var all, oauth1, bearer bool
	var oauth2Username string
	
	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Clear authentication tokens",
		Run: func(cmd *cobra.Command, args []string) {
			if all {
				err := auth.TokenStore.ClearAll()
				if err != nil {
					fmt.Println("Error clearing all tokens:", err)
					os.Exit(1)
				}
				fmt.Println("All authentication cleared!")
			} else if oauth1 {
				err := auth.TokenStore.ClearOAuth1Tokens()
				if err != nil {
					fmt.Println("Error clearing OAuth1 tokens:", err)
					os.Exit(1)
				}
				fmt.Println("OAuth1 tokens cleared!")
			} else if oauth2Username != "" {
				err := auth.TokenStore.ClearOAuth2Token(oauth2Username)
				if err != nil {
					fmt.Println("Error clearing OAuth2 token:", err)
					os.Exit(1)
				}
				fmt.Println("OAuth2 token cleared for", oauth2Username + "!")
			} else if bearer {
				err := auth.TokenStore.ClearBearerToken()
				if err != nil {
					fmt.Println("Error clearing bearer token:", err)
					os.Exit(1)
				}
				fmt.Println("Bearer token cleared!")
			} else {
				fmt.Println("No authentication cleared! Use --all to clear all authentication.")
				os.Exit(1)
			}
		},
	}
	
	cmd.Flags().BoolVar(&all, "all", false, "Clear all authentication")
	cmd.Flags().BoolVar(&oauth1, "oauth1", false, "Clear OAuth1 tokens")
	cmd.Flags().StringVar(&oauth2Username, "oauth2-username", "", "Clear OAuth2 token for username")
	cmd.Flags().BoolVar(&bearer, "bearer", false, "Clear bearer token")
	
	return cmd
}

// Create media upload subcommand
func createMediaUploadCmd(auth *auth.Auth) *cobra.Command {
	var mediaType, mediaCategory string
	var waitForProcessing bool
	
	cmd := &cobra.Command{
		Use:   "upload [flags] FILE",
		Short: "Upload media file",
		Long:  `Upload a media file to X API. Supports images, GIFs, and videos.`,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			filePath := args[0]
			authType, _ := cmd.Flags().GetString("auth")
			username, _ := cmd.Flags().GetString("username")
			verbose, _ := cmd.Flags().GetBool("verbose")
			headers, _ := cmd.Flags().GetStringArray("header")
			config := config.NewConfig()
			client := api.NewApiClient(config, auth)
			
			err := api.ExecuteMediaUpload(filePath, mediaType, mediaCategory, authType, username, verbose, waitForProcessing, headers, client)
			if err != nil {
				fmt.Printf("\033[31m%v\033[0m\n", err)
				os.Exit(1)
			}
		},
	}
	
	cmd.Flags().StringVar(&mediaType, "media-type", "video/mp4", "Media type (e.g., image/jpeg, image/png, video/mp4)")
	cmd.Flags().StringVar(&mediaCategory, "category", "amplify_video", "Media category (e.g., tweet_image, tweet_video, amplify_video)")
	cmd.Flags().BoolVar(&waitForProcessing, "wait", true, "Wait for media processing to complete")
	cmd.Flags().String("auth", "", "Authentication type (oauth1 or oauth2)")
	cmd.Flags().StringP("username", "u", "", "Username for OAuth2 authentication")
	cmd.Flags().BoolP("verbose", "v", false, "Print verbose information")
	cmd.Flags().StringArrayP("header", "H", []string{}, "Request headers")
	
	return cmd
}

// Create media status subcommand
func createMediaStatusCmd(auth *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status [flags] MEDIA_ID",
		Short: "Check media upload status",
		Long:  `Check the status of a media upload by media ID.`,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			mediaID := args[0]
			authType, _ := cmd.Flags().GetString("auth")
			username, _ := cmd.Flags().GetString("username")
			verbose, _ := cmd.Flags().GetBool("verbose")
			wait, _ := cmd.Flags().GetBool("wait")
			headers, _ := cmd.Flags().GetStringArray("header")
			config := config.NewConfig()
			client := api.NewApiClient(config, auth)
			
			err := api.ExecuteMediaStatus(mediaID, authType, username, verbose, wait, headers, client)
			if err != nil {
				fmt.Printf("\033[31m%v\033[0m\n", err)
				os.Exit(1)
			}
		},
	}
	
	cmd.Flags().String("auth", "", "Authentication type (oauth1 or oauth2)")
	cmd.Flags().StringP("username", "u", "", "Username for OAuth2 authentication")
	cmd.Flags().BoolP("verbose", "v", false, "Print verbose information")
	cmd.Flags().BoolP("wait", "w", false, "Wait for media processing to complete")
	cmd.Flags().StringArrayP("header", "H", []string{}, "Request headers")
	return cmd
} 