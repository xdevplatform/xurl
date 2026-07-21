//go:build !cgo || !((darwin && (amd64 || arm64)) || (linux && amd64))

package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/xdevplatform/xurl/auth"
)

// chatSupported reports whether this build includes the XChat client.
const chatSupported = false

// CreateChatCommand returns a stub on platforms where the chat-xdk crypto
// binding is unavailable (it requires cgo and prebuilt static libraries for
// darwin/amd64, darwin/arm64, or linux/amd64).
func CreateChatCommand(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Send and read end-to-end encrypted XChat messages (unavailable in this build)",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(os.Stderr, "xurl chat is not available in this build: the XChat crypto library requires cgo and supports macOS (amd64/arm64) and Linux (amd64).")
			fmt.Fprintln(os.Stderr, "On a supported platform, build from source with CGO enabled: CGO_ENABLED=1 go install github.com/xdevplatform/xurl@latest")
			os.Exit(1)
		},
	}
	// Swallow any subcommand/flags so `xurl chat send ...` also reaches the stub.
	cmd.FParseErrWhitelist.UnknownFlags = true
	cmd.Args = cobra.ArbitraryArgs
	return cmd
}
