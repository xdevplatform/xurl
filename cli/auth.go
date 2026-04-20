package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/xdevplatform/xurl/auth"
	"github.com/xdevplatform/xurl/config"
	"github.com/xdevplatform/xurl/store"
)

// CreateAuthCommand creates the auth command and its subcommands
func CreateAuthCommand(a *auth.Auth) *cobra.Command {
	var authCmd = &cobra.Command{
		Use:   "auth",
		Short: "Authentication management",
	}

	authCmd.AddCommand(createAuthBearerCmd(a))
	authCmd.AddCommand(createAuthOAuth2Cmd(a))
	authCmd.AddCommand(createAuthOAuth1Cmd(a))
	authCmd.AddCommand(createAuthStatusCmd())
	authCmd.AddCommand(createAuthClearCmd(a))
	authCmd.AddCommand(createAppCmd(a))
	authCmd.AddCommand(createDefaultCmd(a))

	return authCmd
}

// ─── auth bearer ────────────────────────────────────────────────────

func createAuthBearerCmd(a *auth.Auth) *cobra.Command {
	var bearerToken string

	cmd := &cobra.Command{
		Use:   "app",
		Short: "Configure app-auth (bearer token)",
		Run: func(cmd *cobra.Command, args []string) {
			err := a.TokenStore.SaveBearerTokenForApp(a.AppName(), bearerToken)
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

// ─── auth oauth2 ────────────────────────────────────────────────────

func createAuthOAuth2Cmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "oauth2 [USERNAME]",
		Short: "Configure OAuth2 authentication",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			username := ""
			if len(args) > 0 {
				username = args[0]
			}
			_, err := a.OAuth2Flow(username)
			if err != nil {
				fmt.Println("OAuth2 authentication failed:", err)
				os.Exit(1)
			}
			fmt.Printf("\033[32mOAuth2 authentication successful!\033[0m\n")
		},
	}

	return cmd
}

// ─── auth oauth1 ────────────────────────────────────────────────────

func createAuthOAuth1Cmd(a *auth.Auth) *cobra.Command {
	var consumerKey, consumerSecret, accessToken, tokenSecret string

	cmd := &cobra.Command{
		Use:   "oauth1",
		Short: "Configure OAuth1 authentication",
		Run: func(cmd *cobra.Command, args []string) {
			err := a.TokenStore.SaveOAuth1TokensForApp(a.AppName(), accessToken, tokenSecret, consumerKey, consumerSecret)
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

// ─── auth status ────────────────────────────────────────────────────

func createAuthStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show authentication status",
		Run: func(cmd *cobra.Command, args []string) {
			ts := store.NewTokenStore()

			apps := ts.ListApps()
			defaultApp := ts.GetDefaultApp()

			if len(apps) == 0 {
				fmt.Println("No apps registered. Use 'xurl auth apps add' to register one.")
				return
			}

			for i, name := range apps {
				app := ts.GetApp(name)

				// Header line
				marker := " "
				if name == defaultApp {
					marker = "▸"
				}
				clientHint := "(no credentials)"
				if app.ClientID != "" {
					clientHint = fmt.Sprintf("client_id: %s…", truncate(app.ClientID, 8))
				}
				fmt.Printf("%s %s  [%s]\n", marker, name, clientHint)

				redirectURI, fromEnv, source := config.ResolveRedirectURI(name)
				if fromEnv {
					fmt.Printf("      redirect_uri: %s  [effective via %s]\n", redirectURI, source)
					if app.RedirectURI != "" {
						fmt.Printf("      stored_redirect_uri: %s\n", app.RedirectURI)
					}
				} else {
					fmt.Printf("      redirect_uri: %s  [%s]\n", redirectURI, source)
				}

				// OAuth2 users
				usernames := ts.GetOAuth2UsernamesForApp(name)
				if len(usernames) > 0 {
					for _, u := range usernames {
						if u == app.DefaultUser {
							fmt.Printf("    ▸ oauth2: %s\n", displayOAuth2Username(u))
						} else {
							fmt.Printf("      oauth2: %s\n", displayOAuth2Username(u))
						}
					}
				} else {
					fmt.Println("      oauth2: (none)")
				}

				// OAuth1
				if app.OAuth1Token != nil {
					fmt.Println("      oauth1: ✓")
				} else {
					fmt.Println("      oauth1: –")
				}

				// Bearer
				if app.BearerToken != nil {
					fmt.Println("      bearer: ✓")
				} else {
					fmt.Println("      bearer: –")
				}

				if i < len(apps)-1 {
					fmt.Println()
				}
			}
		},
	}

	return cmd
}

// ─── auth clear ─────────────────────────────────────────────────────

func createAuthClearCmd(a *auth.Auth) *cobra.Command {
	var all, oauth1, bearer bool
	var oauth2Username string

	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Clear authentication tokens",
		Run: func(cmd *cobra.Command, args []string) {
			if all {
				err := a.TokenStore.ClearAllForApp(a.AppName())
				if err != nil {
					fmt.Println("Error clearing all tokens:", err)
					os.Exit(1)
				}
				fmt.Println("All authentication cleared!")
			} else if oauth1 {
				err := a.TokenStore.ClearOAuth1TokensForApp(a.AppName())
				if err != nil {
					fmt.Println("Error clearing OAuth1 tokens:", err)
					os.Exit(1)
				}
				fmt.Println("OAuth1 tokens cleared!")
			} else if oauth2Username != "" {
				err := a.TokenStore.ClearOAuth2TokenForApp(a.AppName(), oauth2Username)
				if err != nil {
					fmt.Println("Error clearing OAuth2 token:", err)
					os.Exit(1)
				}
				fmt.Println("OAuth2 token cleared for", oauth2Username+"!")
			} else if bearer {
				err := a.TokenStore.ClearBearerTokenForApp(a.AppName())
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

// ─── auth apps  (add / remove / list) ───────────────────────────────

func createAppCmd(a *auth.Auth) *cobra.Command {
	appCmd := &cobra.Command{
		Use:   "apps",
		Short: "Manage registered X API apps",
	}

	appCmd.AddCommand(createAppAddCmd(a))
	appCmd.AddCommand(createAppUpdateCmd(a))
	appCmd.AddCommand(createAppRemoveCmd(a))
	appCmd.AddCommand(createAppListCmd())
	appCmd.AddCommand(createAppRedirectURICmd(a))

	return appCmd
}

func createAppAddCmd(a *auth.Auth) *cobra.Command {
	var clientID, clientSecret, redirectURI string

	cmd := &cobra.Command{
		Use:   "add NAME",
		Short: "Register a new X API app",
		Long: `Register a new X API app with a client ID and secret.

Examples:
  xurl auth apps add my-app --client-id abc --client-secret xyz
  xurl auth apps add my-app --client-id abc --client-secret xyz --redirect-uri http://localhost:8080/callback`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]
			err := a.TokenStore.AddApp(name, clientID, clientSecret)
			if err != nil {
				fmt.Printf("\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			if redirectURI != "" {
				if err := a.TokenStore.SetAppRedirectURI(name, redirectURI); err != nil {
					fmt.Printf("\033[31mError: %v\033[0m\n", err)
					os.Exit(1)
				}
			}
			fmt.Printf("\033[32mApp %q registered!\033[0m\n", name)
			if len(a.TokenStore.ListApps()) == 1 {
				fmt.Printf("  (set as default app)\n")
			}
		},
	}

	cmd.Flags().StringVar(&clientID, "client-id", "", "OAuth2 client ID")
	cmd.Flags().StringVar(&clientSecret, "client-secret", "", "OAuth2 client secret")
	cmd.Flags().StringVar(&redirectURI, "redirect-uri", "", "Stored OAuth2 redirect URI for this app")
	cmd.MarkFlagRequired("client-id")
	cmd.MarkFlagRequired("client-secret")

	return cmd
}

func createAppUpdateCmd(a *auth.Auth) *cobra.Command {
	var clientID, clientSecret, redirectURI string

	cmd := &cobra.Command{
		Use:   "update NAME",
		Short: "Update credentials for an existing app",
		Long: `Update the client ID and/or secret for an existing registered app.

Examples:
  xurl auth apps update default --client-id abc --client-secret xyz
  xurl auth apps update my-app --client-id newid
  xurl auth apps update my-app --redirect-uri http://localhost:8080/callback`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]
			if clientID == "" && clientSecret == "" && redirectURI == "" {
				fmt.Println("Nothing to update. Provide --client-id, --client-secret, and/or --redirect-uri.")
				os.Exit(1)
			}
			if err := a.TokenStore.UpdateApp(name, clientID, clientSecret); err != nil {
				fmt.Printf("\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			if redirectURI != "" {
				if err := a.TokenStore.SetAppRedirectURI(name, redirectURI); err != nil {
					fmt.Printf("\033[31mError: %v\033[0m\n", err)
					os.Exit(1)
				}
			}
			fmt.Printf("\033[32mApp %q updated.\033[0m\n", name)
		},
	}

	cmd.Flags().StringVar(&clientID, "client-id", "", "OAuth2 client ID")
	cmd.Flags().StringVar(&clientSecret, "client-secret", "", "OAuth2 client secret")
	cmd.Flags().StringVar(&redirectURI, "redirect-uri", "", "Stored OAuth2 redirect URI for this app")

	return cmd
}

func createAppRemoveCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove NAME",
		Short: "Remove a registered app and all its tokens",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]
			err := a.TokenStore.RemoveApp(name)
			if err != nil {
				fmt.Printf("\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			fmt.Printf("\033[32mApp %q removed.\033[0m\n", name)
		},
	}
	return cmd
}

func createAppListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List registered apps",
		Run: func(cmd *cobra.Command, args []string) {
			ts := store.NewTokenStore()
			apps := ts.ListApps()
			defaultApp := ts.GetDefaultApp()

			if len(apps) == 0 {
				fmt.Println("No apps registered. Use 'xurl auth apps add' to register one.")
				return
			}

			for _, name := range apps {
				app := ts.GetApp(name)
				marker := "  "
				if name == defaultApp {
					marker = "▸ "
				}
				clientHint := ""
				if app.ClientID != "" {
					clientHint = fmt.Sprintf(" (client_id: %s…)", truncate(app.ClientID, 8))
				}
				redirectURI, fromEnv, source := config.ResolveRedirectURI(name)
				redirectHint := fmt.Sprintf(" redirect_uri: %s [%s]", redirectURI, source)
				if fromEnv && app.RedirectURI != "" {
					redirectHint = fmt.Sprintf(" redirect_uri: %s [%s, stored: %s]", redirectURI, source, app.RedirectURI)
				}
				fmt.Printf("%s%s%s%s\n", marker, name, clientHint, redirectHint)
			}
		},
	}
	return cmd
}

func createAppRedirectURICmd(a *auth.Auth) *cobra.Command {
	redirectCmd := &cobra.Command{
		Use:   "redirect-uri",
		Short: "Get or set the stored OAuth2 redirect URI for an app",
	}

	redirectCmd.AddCommand(createAppRedirectURIGetCmd(a))
	redirectCmd.AddCommand(createAppRedirectURISetCmd(a))

	return redirectCmd
}

func createAppRedirectURIGetCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get [NAME]",
		Short: "Show the effective and stored redirect URI for an app",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ts := a.TokenStore
			appName := resolveAppNameArg(ts, args)
			if appName == "" {
				fmt.Println("No apps registered. Use 'xurl auth apps add' to register one.")
				os.Exit(1)
			}
			app := ts.GetApp(appName)
			if app == nil {
				fmt.Printf("\033[31mError: app %q not found\033[0m\n", appName)
				os.Exit(1)
			}

			effective, _, source := config.ResolveRedirectURI(appName)
			stored := app.RedirectURI
			if stored == "" {
				stored = "(not set)"
			}

			fmt.Printf("app: %s\n", appName)
			fmt.Printf("effective_redirect_uri: %s\n", effective)
			fmt.Printf("effective_source: %s\n", source)
			fmt.Printf("stored_redirect_uri: %s\n", stored)
		},
	}

	return cmd
}

func createAppRedirectURISetCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set NAME URI",
		Short: "Set the stored OAuth2 redirect URI for an app",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]
			redirectURI := args[1]
			if err := a.TokenStore.SetAppRedirectURI(name, redirectURI); err != nil {
				fmt.Printf("\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			fmt.Printf("\033[32mRedirect URI set for app %q.\033[0m\n", name)
		},
	}

	return cmd
}

// ─── auth default ───────────────────────────────────────────────────

func createDefaultCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "default [APP_NAME [USERNAME]]",
		Short: "Set default app and/or user (interactive or by argument)",
		Long: `Set the default app and/or OAuth2 user.

Without arguments: launches an interactive picker (Bubble Tea).
With one argument:  sets the default app.
With two arguments: sets the default app and default OAuth2 user.

Examples:
  xurl auth default                     # interactive picker
  xurl auth default my-app              # set default app
  xurl auth default my-app alice        # set default app + user`,
		Args: cobra.MaximumNArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			ts := a.TokenStore

			if len(args) >= 1 {
				// Non-interactive: set default app by name
				appName := args[0]
				if err := ts.SetDefaultApp(appName); err != nil {
					fmt.Printf("\033[31mError: %v\033[0m\n", err)
					os.Exit(1)
				}
				fmt.Printf("\033[32mDefault app set to %q\033[0m\n", appName)

				if len(args) == 2 {
					userName := args[1]
					if err := ts.SetDefaultUser(appName, userName); err != nil {
						fmt.Printf("\033[31mError: %v\033[0m\n", err)
						os.Exit(1)
					}
					fmt.Printf("\033[32mDefault user set to %q\033[0m\n", userName)
				}
				return
			}

			// Interactive: pick app
			apps := ts.ListApps()
			if len(apps) == 0 {
				fmt.Println("No apps registered. Use 'xurl auth apps add' to register one.")
				return
			}

			appChoice, err := RunPicker("Select default app", apps)
			if err != nil {
				fmt.Printf("\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			if appChoice == "" {
				return // user cancelled
			}

			if err := ts.SetDefaultApp(appChoice); err != nil {
				fmt.Printf("\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			fmt.Printf("\033[32mDefault app set to %q\033[0m\n", appChoice)

			// Pick a default user within the app
			users := ts.GetOAuth2UsernamesForApp(appChoice)
			if len(users) > 0 {
				userChoice, err := RunPicker("Select default OAuth2 user", users)
				if err != nil {
					fmt.Printf("\033[31mError: %v\033[0m\n", err)
					os.Exit(1)
				}
				if userChoice != "" {
					if err := ts.SetDefaultUser(appChoice, userChoice); err != nil {
						fmt.Printf("\033[31mError: %v\033[0m\n", err)
						os.Exit(1)
					}
					fmt.Printf("\033[32mDefault user set to %q\033[0m\n", userChoice)
				}
			}
		},
	}
	return cmd
}

// ─── helpers ────────────────────────────────────────────────────────

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

func displayOAuth2Username(username string) string {
	if username == "" {
		return "(unknown user)"
	}
	return username
}

func resolveAppNameArg(ts *store.TokenStore, args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ts.GetDefaultApp()
}
