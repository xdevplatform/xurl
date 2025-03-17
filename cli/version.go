package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"xurl/version"
)

// CreateVersionCommand creates the version command
func CreateVersionCommand() *cobra.Command {
	var versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Show xurl version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("xurl %s\n", version.Version)
		},
	}

	return versionCmd
}
