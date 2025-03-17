package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"xurl/auth"
	"xurl/store"
)

// CreateAuthCommand creates the auth command and its subcommands
func CreateAuthCommand(auth *auth.Auth) *cobra.Command {
	var authCmd = &cobra.Command{
		Use:   "auth",
		Short: "Authentication management",
	}

	authCmd.AddCommand(createAuthAppCmd(auth))
	authCmd.AddCommand(createAuthOAuth2Cmd(auth))
	authCmd.AddCommand(createAuthOAuth1Cmd(auth))
	authCmd.AddCommand(createAuthStatusCmd())
	authCmd.AddCommand(createAuthClearCmd(auth))

	return authCmd
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
				fmt.Println("OAuth2 token cleared for", oauth2Username+"!")
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
