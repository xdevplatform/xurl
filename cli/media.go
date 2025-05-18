package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/xdevplatform/xurl/api"
	"github.com/xdevplatform/xurl/auth"
	"github.com/xdevplatform/xurl/config"
)

// CreateMediaCommand creates the media command and its subcommands
func CreateMediaCommand(auth *auth.Auth) *cobra.Command {
	// Create media command
	var mediaCmd = &cobra.Command{
		Use:   "media",
		Short: "Media upload operations",
	}

	mediaCmd.AddCommand(createMediaUploadCmd(auth))
	mediaCmd.AddCommand(createMediaStatusCmd(auth))

	return mediaCmd
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
			trace, _ := cmd.Flags().GetBool("trace")
			config := config.NewConfig()
			client := api.NewApiClient(config, auth)

			err := api.ExecuteMediaUpload(filePath, mediaType, mediaCategory, authType, username, verbose, trace, waitForProcessing, headers, client)
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
	cmd.Flags().BoolP("trace", "t", false, "Add trace header to request")
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
			trace, _ := cmd.Flags().GetBool("trace")
			headers, _ := cmd.Flags().GetStringArray("header")
			config := config.NewConfig()
			client := api.NewApiClient(config, auth)

			err := api.ExecuteMediaStatus(mediaID, authType, username, verbose, wait, trace, headers, client)
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
	cmd.Flags().BoolP("trace", "t", false, "Add trace header to request")
	cmd.Flags().StringArrayP("header", "H", []string{}, "Request headers")
	return cmd
}
