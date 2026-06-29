package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/xdevplatform/xurl/auth"
)

// CreateTokenCommand creates the `token` command, which prints a valid OAuth2
// access token for the active app to stdout. It refreshes (and persists) an
// expired token but never opens a browser, so it stays scriptable.
func CreateTokenCommand(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Print a valid OAuth2 access token for the active app",
		Long: `Print a valid OAuth2 access token for the active app to stdout (one line).

If the stored token has expired it is refreshed and persisted first. This
command never opens a browser, so it is safe to use in scripts. If no token is
available it exits non-zero and tells you to run 'xurl auth oauth2'.

Examples:
  xurl token
  xurl token --app my-app
  TOKEN=$(xurl token)`,
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			username, _ := cmd.Flags().GetString("username")

			token, err := a.GetValidOAuth2Token(username)
			if err != nil {
				appName := a.TokenStore.GetActiveAppName(a.AppName())
				target := fmt.Sprintf("app %q", appName)
				if username != "" {
					target = fmt.Sprintf("app %q (user %q)", appName, username)
				}
				fprintError(os.Stderr, "Error: no valid oauth2 token for %s: %v", target, err)
				fmt.Fprintf(os.Stderr, "Run: xurl auth oauth2%s\n", appFlagHint(a.AppName()))
				os.Exit(1)
			}

			fmt.Println(token)
		},
	}

	cmd.Flags().StringP("username", "u", "", "OAuth2 username to act as")
	return cmd
}

// appFlagHint returns a " --app NAME" suffix for help messages when an explicit
// app override is active, or an empty string otherwise.
func appFlagHint(appName string) string {
	if appName == "" {
		return ""
	}
	return " --app " + appName
}

// isTerminal reports whether f is attached to a terminal, used to decide whether
// ANSI color is appropriate.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && (info.Mode()&os.ModeCharDevice) != 0
}

// fprintError writes a red error line to w, omitting the ANSI color codes when w
// is not a terminal so redirected/piped output stays clean for scripts.
func fprintError(w *os.File, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if isTerminal(w) {
		fmt.Fprintf(w, "\033[31m%s\033[0m\n", msg)
	} else {
		fmt.Fprintln(w, msg)
	}
}
