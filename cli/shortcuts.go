package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/xdevplatform/xurl/api"
	"github.com/xdevplatform/xurl/auth"
	"github.com/xdevplatform/xurl/config"
	"github.com/xdevplatform/xurl/utils"
)

// -----------------------------------------------------------------
// Helpers shared by every shortcut command
// -----------------------------------------------------------------

// baseOpts builds a RequestOptions from the common persistent flags.
func baseOpts(cmd *cobra.Command) api.RequestOptions {
	authType, _ := cmd.Flags().GetString("auth")
	username, _ := cmd.Flags().GetString("username")
	verbose, _ := cmd.Flags().GetBool("verbose")
	trace, _ := cmd.Flags().GetBool("trace")

	return api.RequestOptions{
		AuthType: authType,
		Username: username,
		Verbose:  verbose,
		Trace:    trace,
	}
}

// newClient creates an ApiClient from the auth object.
func newClient(a *auth.Auth) *api.ApiClient {
	cfg := config.NewConfig()
	return api.NewApiClient(cfg, a)
}

// printResult pretty‑prints a JSON response or exits on error.
func printResult(resp json.RawMessage, err error) {
	if err != nil {
		// Try to pretty‑print API error bodies
		var raw json.RawMessage
		if json.Unmarshal([]byte(err.Error()), &raw) == nil {
			utils.FormatAndPrintResponse(raw)
		} else {
			fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
		}
		os.Exit(1)
	}
	utils.FormatAndPrintResponse(resp)
}

// resolveMyUserID calls /2/users/me and returns the authenticated user's ID.
func resolveMyUserID(client api.Client, opts api.RequestOptions) (string, error) {
	resp, err := api.GetMe(client, opts)
	if err != nil {
		return "", fmt.Errorf("could not resolve your user ID (are you authenticated?): %w", err)
	}
	var me struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp, &me); err != nil {
		return "", fmt.Errorf("could not parse /2/users/me response: %w", err)
	}
	if me.Data.ID == "" {
		return "", fmt.Errorf("user ID was empty – check your auth tokens")
	}
	return me.Data.ID, nil
}

// resolveUserID looks up a username and returns its user ID.
func resolveUserID(client api.Client, username string, opts api.RequestOptions) (string, error) {
	resp, err := api.LookupUser(client, username, opts)
	if err != nil {
		return "", fmt.Errorf("could not look up user @%s: %w", username, err)
	}
	var user struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp, &user); err != nil {
		return "", fmt.Errorf("could not parse user lookup response: %w", err)
	}
	if user.Data.ID == "" {
		return "", fmt.Errorf("user @%s not found", username)
	}
	return user.Data.ID, nil
}

// addCommonFlags adds --auth, --username, --verbose, --trace to a command.
func addCommonFlags(cmd *cobra.Command) {
	cmd.Flags().String("auth", "", "Authentication type (oauth1, oauth2, app)")
	cmd.Flags().StringP("username", "u", "", "OAuth2 username to act as")
	cmd.Flags().BoolP("verbose", "v", false, "Print verbose request/response info")
	cmd.Flags().BoolP("trace", "t", false, "Add X-B3-Flags trace header")
}

// -----------------------------------------------------------------
// CreateShortcutCommands registers all the shorthand subcommands
// on the given root command.
// -----------------------------------------------------------------

func CreateShortcutCommands(rootCmd *cobra.Command, a *auth.Auth) {
	rootCmd.AddCommand(
		postCmd(a),
		replyCmd(a),
		quoteCmd(a),
		deleteCmd(a),
		readCmd(a),
		searchCmd(a),
		whoamiCmd(a),
		userCmd(a),
		timelineCmd(a),
		mentionsCmd(a),
		likeCmd(a),
		unlikeCmd(a),
		repostCmd(a),
		unrepostCmd(a),
		bookmarkCmd(a),
		unbookmarkCmd(a),
		bookmarksCmd(a),
		followCmd(a),
		unfollowCmd(a),
		followingCmd(a),
		followersCmd(a),
		likesCmd(a),
		dmCmd(a),
		dmsCmd(a),
		blockCmd(a),
		unblockCmd(a),
		muteCmd(a),
		unmuteCmd(a),
	)
}

// =================================================================
//  POSTING
// =================================================================

func postCmd(a *auth.Auth) *cobra.Command {
	var mediaIDs []string
	cmd := &cobra.Command{
		Use:   `post "TEXT"`,
		Short: "Post to X",
		Long: `Post a new post to X.

Examples:
  xurl post "Hello world!"
  xurl post "Check this out" --media-id 12345
  xurl post "Multiple images" --media-id 111 --media-id 222`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			printResult(api.CreatePost(client, args[0], mediaIDs, opts))
		},
	}
	cmd.Flags().StringArrayVar(&mediaIDs, "media-id", nil, "Media ID(s) to attach (repeatable)")
	addCommonFlags(cmd)
	return cmd
}

func replyCmd(a *auth.Auth) *cobra.Command {
	var mediaIDs []string
	cmd := &cobra.Command{
		Use:   `reply POST_ID_OR_URL "TEXT"`,
		Short: "Reply to a post",
		Long: `Reply to an existing post. Accepts a post ID or full URL.

Examples:
  xurl reply 1234567890 "Great thread!"
  xurl reply https://x.com/user/status/1234567890 "Nice post!"`,
		Args: cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			printResult(api.ReplyToPost(client, args[0], args[1], mediaIDs, opts))
		},
	}
	cmd.Flags().StringArrayVar(&mediaIDs, "media-id", nil, "Media ID(s) to attach (repeatable)")
	addCommonFlags(cmd)
	return cmd
}

func quoteCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   `quote POST_ID_OR_URL "TEXT"`,
		Short: "Quote a post",
		Long: `Quote an existing post with your own commentary.

Examples:
  xurl quote 1234567890 "This is so true"
  xurl quote https://x.com/user/status/1234567890 "Interesting take"`,
		Args: cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			printResult(api.QuotePost(client, args[0], args[1], opts))
		},
	}
	addCommonFlags(cmd)
	return cmd
}

func deleteCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete POST_ID_OR_URL",
		Short: "Delete a post",
		Long: `Delete one of your posts. Accepts a post ID or full URL.

Examples:
  xurl delete 1234567890
  xurl delete https://x.com/user/status/1234567890`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			printResult(api.DeletePost(client, args[0], opts))
		},
	}
	addCommonFlags(cmd)
	return cmd
}

// =================================================================
//  READING
// =================================================================

func readCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "read POST_ID_OR_URL",
		Short: "Read a post",
		Long: `Fetch and display a single post with author info and metrics.

Examples:
  xurl read 1234567890
  xurl read https://x.com/user/status/1234567890`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			printResult(api.ReadPost(client, args[0], opts))
		},
	}
	addCommonFlags(cmd)
	return cmd
}

func searchCmd(a *auth.Auth) *cobra.Command {
	var maxResults int
	var sinceID, untilID, startTime, endTime, nextToken string
	var tweetFields, userFields, expansions string
	cmd := &cobra.Command{
		Use:   `search "QUERY"`,
		Short: "Search recent posts",
		Long: `Search recent posts matching a query.

Examples:
  xurl search "golang"
  xurl search "from:elonmusk" -n 20
  xurl search "#buildinpublic" -n 15
  xurl search "from:user1 OR from:user2" -n 100 --since-id 1234567890
  xurl search "crypto" --start-time 2025-01-01T00:00:00Z -n 50`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			pag := api.PaginationOptions{
				SinceID:     sinceID,
				UntilID:     untilID,
				StartTime:   startTime,
				EndTime:     endTime,
				NextToken:   nextToken,
				TweetFields: tweetFields,
				UserFields:  userFields,
				Expansions:  expansions,
			}
			printResult(api.SearchPosts(client, args[0], maxResults, pag, opts))
		},
	}
	cmd.Flags().IntVarP(&maxResults, "max-results", "n", 10, "Number of results (min 10, max 100)")
	cmd.Flags().StringVar(&sinceID, "since-id", "", "Only return results with an ID greater than this")
	cmd.Flags().StringVar(&untilID, "until-id", "", "Only return results with an ID less than this")
	cmd.Flags().StringVar(&startTime, "start-time", "", "Oldest UTC datetime (YYYY-MM-DDTHH:mm:ssZ)")
	cmd.Flags().StringVar(&endTime, "end-time", "", "Newest UTC datetime (YYYY-MM-DDTHH:mm:ssZ)")
	cmd.Flags().StringVar(&nextToken, "next-token", "", "Pagination token for next page of results")
	cmd.Flags().StringVar(&tweetFields, "tweet-fields", "", "Comma-separated tweet fields (overrides defaults)")
	cmd.Flags().StringVar(&userFields, "user-fields", "", "Comma-separated user fields (overrides defaults)")
	cmd.Flags().StringVar(&expansions, "expansions", "", "Comma-separated expansions (overrides defaults)")
	addCommonFlags(cmd)
	return cmd
}

// =================================================================
//  USER INFO
// =================================================================

func whoamiCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Show the authenticated user's profile",
		Long: `Fetch profile information for the currently authenticated user.

Examples:
  xurl whoami`,
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			printResult(api.GetMe(client, opts))
		},
	}
	addCommonFlags(cmd)
	return cmd
}

func userCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user USERNAME",
		Short: "Look up a user by username",
		Long: `Fetch profile information for any user by their @username.

Examples:
  xurl user elonmusk
  xurl user @XDevelopers`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			printResult(api.LookupUser(client, args[0], opts))
		},
	}
	addCommonFlags(cmd)
	return cmd
}

// =================================================================
//  TIMELINE & MENTIONS
// =================================================================

func timelineCmd(a *auth.Auth) *cobra.Command {
	var maxResults int
	var sinceID, untilID, startTime, endTime, nextToken string
	var tweetFields, userFields, expansions, exclude string
	cmd := &cobra.Command{
		Use:   "timeline",
		Short: "Show your home timeline",
		Long: `Fetch your reverse‑chronological home timeline.

Examples:
  xurl timeline
  xurl timeline -n 25
  xurl timeline --since-id 1234567890 -n 50
  xurl timeline --exclude replies,retweets`,
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			userID, err := resolveMyUserID(client, opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			pag := api.PaginationOptions{
				SinceID:     sinceID,
				UntilID:     untilID,
				StartTime:   startTime,
				EndTime:     endTime,
				NextToken:   nextToken,
				TweetFields: tweetFields,
				UserFields:  userFields,
				Expansions:  expansions,
				Exclude:     exclude,
			}
			printResult(api.GetTimeline(client, userID, maxResults, pag, opts))
		},
	}
	cmd.Flags().IntVarP(&maxResults, "max-results", "n", 10, "Number of results (1–100)")
	cmd.Flags().StringVar(&sinceID, "since-id", "", "Only return results with an ID greater than this")
	cmd.Flags().StringVar(&untilID, "until-id", "", "Only return results with an ID less than this")
	cmd.Flags().StringVar(&startTime, "start-time", "", "Oldest UTC datetime (YYYY-MM-DDTHH:mm:ssZ)")
	cmd.Flags().StringVar(&endTime, "end-time", "", "Newest UTC datetime (YYYY-MM-DDTHH:mm:ssZ)")
	cmd.Flags().StringVar(&nextToken, "next-token", "", "Pagination token for next page of results")
	cmd.Flags().StringVar(&tweetFields, "tweet-fields", "", "Comma-separated tweet fields (overrides defaults)")
	cmd.Flags().StringVar(&userFields, "user-fields", "", "Comma-separated user fields (overrides defaults)")
	cmd.Flags().StringVar(&expansions, "expansions", "", "Comma-separated expansions (overrides defaults)")
	cmd.Flags().StringVar(&exclude, "exclude", "", "Comma-separated exclusions: replies,retweets")
	addCommonFlags(cmd)
	return cmd
}

func mentionsCmd(a *auth.Auth) *cobra.Command {
	var maxResults int
	var sinceID, untilID, startTime, endTime, nextToken string
	var tweetFields, userFields, expansions string
	cmd := &cobra.Command{
		Use:   "mentions",
		Short: "Show your recent mentions",
		Long: `Fetch posts that mention the authenticated user.

Examples:
  xurl mentions
  xurl mentions -n 25
  xurl mentions --since-id 1234567890`,
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			userID, err := resolveMyUserID(client, opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			pag := api.PaginationOptions{
				SinceID:     sinceID,
				UntilID:     untilID,
				StartTime:   startTime,
				EndTime:     endTime,
				NextToken:   nextToken,
				TweetFields: tweetFields,
				UserFields:  userFields,
				Expansions:  expansions,
			}
			printResult(api.GetMentions(client, userID, maxResults, pag, opts))
		},
	}
	cmd.Flags().IntVarP(&maxResults, "max-results", "n", 10, "Number of results (5–100)")
	cmd.Flags().StringVar(&sinceID, "since-id", "", "Only return results with an ID greater than this")
	cmd.Flags().StringVar(&untilID, "until-id", "", "Only return results with an ID less than this")
	cmd.Flags().StringVar(&startTime, "start-time", "", "Oldest UTC datetime (YYYY-MM-DDTHH:mm:ssZ)")
	cmd.Flags().StringVar(&endTime, "end-time", "", "Newest UTC datetime (YYYY-MM-DDTHH:mm:ssZ)")
	cmd.Flags().StringVar(&nextToken, "next-token", "", "Pagination token for next page of results")
	cmd.Flags().StringVar(&tweetFields, "tweet-fields", "", "Comma-separated tweet fields (overrides defaults)")
	cmd.Flags().StringVar(&userFields, "user-fields", "", "Comma-separated user fields (overrides defaults)")
	cmd.Flags().StringVar(&expansions, "expansions", "", "Comma-separated expansions (overrides defaults)")
	addCommonFlags(cmd)
	return cmd
}

// =================================================================
//  ENGAGEMENT — Like / Repost / Bookmark
// =================================================================

func likeCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "like POST_ID_OR_URL",
		Short: "Like a post",
		Long: `Like a post. Accepts a post ID or full URL.

Examples:
  xurl like 1234567890
  xurl like https://x.com/user/status/1234567890`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			userID, err := resolveMyUserID(client, opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			printResult(api.LikePost(client, userID, args[0], opts))
		},
	}
	addCommonFlags(cmd)
	return cmd
}

func unlikeCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unlike POST_ID_OR_URL",
		Short: "Unlike a post",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			userID, err := resolveMyUserID(client, opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			printResult(api.UnlikePost(client, userID, args[0], opts))
		},
	}
	addCommonFlags(cmd)
	return cmd
}

func repostCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repost POST_ID_OR_URL",
		Short: "Repost a post",
		Long: `Repost a post. Accepts a post ID or full URL.

Examples:
  xurl repost 1234567890
  xurl repost https://x.com/user/status/1234567890`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			userID, err := resolveMyUserID(client, opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			printResult(api.Repost(client, userID, args[0], opts))
		},
	}
	addCommonFlags(cmd)
	return cmd
}

func unrepostCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unrepost POST_ID_OR_URL",
		Short: "Undo a repost",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			userID, err := resolveMyUserID(client, opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			printResult(api.Unrepost(client, userID, args[0], opts))
		},
	}
	addCommonFlags(cmd)
	return cmd
}

func bookmarkCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bookmark POST_ID_OR_URL",
		Short: "Bookmark a post",
		Long: `Bookmark a post. Accepts a post ID or full URL.

Examples:
  xurl bookmark 1234567890
  xurl bookmark https://x.com/user/status/1234567890`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			userID, err := resolveMyUserID(client, opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			printResult(api.Bookmark(client, userID, args[0], opts))
		},
	}
	addCommonFlags(cmd)
	return cmd
}

func unbookmarkCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unbookmark POST_ID_OR_URL",
		Short: "Remove a bookmark",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			userID, err := resolveMyUserID(client, opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			printResult(api.Unbookmark(client, userID, args[0], opts))
		},
	}
	addCommonFlags(cmd)
	return cmd
}

func bookmarksCmd(a *auth.Auth) *cobra.Command {
	var maxResults int
	cmd := &cobra.Command{
		Use:   "bookmarks",
		Short: "List your bookmarks",
		Long: `Fetch your bookmarked posts.

Examples:
  xurl bookmarks
  xurl bookmarks -n 25`,
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			userID, err := resolveMyUserID(client, opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			printResult(api.GetBookmarks(client, userID, maxResults, opts))
		},
	}
	cmd.Flags().IntVarP(&maxResults, "max-results", "n", 10, "Number of results (1–100)")
	addCommonFlags(cmd)
	return cmd
}

func likesCmd(a *auth.Auth) *cobra.Command {
	var maxResults int
	cmd := &cobra.Command{
		Use:   "likes",
		Short: "List your liked posts",
		Long: `Fetch posts you have liked.

Examples:
  xurl likes
  xurl likes -n 25`,
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			userID, err := resolveMyUserID(client, opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			printResult(api.GetLikedPosts(client, userID, maxResults, opts))
		},
	}
	cmd.Flags().IntVarP(&maxResults, "max-results", "n", 10, "Number of results (1–100)")
	addCommonFlags(cmd)
	return cmd
}

// =================================================================
//  SOCIAL GRAPH — Follow / Block / Mute
// =================================================================

func followCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "follow USERNAME",
		Short: "Follow a user",
		Long: `Follow a user by their @username.

Examples:
  xurl follow elonmusk
  xurl follow @XDevelopers`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			myID, err := resolveMyUserID(client, opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			targetID, err := resolveUserID(client, args[0], opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			printResult(api.FollowUser(client, myID, targetID, opts))
		},
	}
	addCommonFlags(cmd)
	return cmd
}

func unfollowCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unfollow USERNAME",
		Short: "Unfollow a user",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			myID, err := resolveMyUserID(client, opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			targetID, err := resolveUserID(client, args[0], opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			printResult(api.UnfollowUser(client, myID, targetID, opts))
		},
	}
	addCommonFlags(cmd)
	return cmd
}

func followingCmd(a *auth.Auth) *cobra.Command {
	var maxResults int
	var targetUser string
	cmd := &cobra.Command{
		Use:   "following",
		Short: "List users you follow",
		Long: `Fetch the list of users you (or another user) follow.

Examples:
  xurl following
  xurl following --of elonmusk -n 50`,
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			var userID string
			var err error
			if targetUser != "" {
				userID, err = resolveUserID(client, targetUser, opts)
			} else {
				userID, err = resolveMyUserID(client, opts)
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			printResult(api.GetFollowing(client, userID, maxResults, opts))
		},
	}
	cmd.Flags().IntVarP(&maxResults, "max-results", "n", 10, "Number of results (1–1000)")
	cmd.Flags().StringVar(&targetUser, "of", "", "Username to list following for (default: you)")
	addCommonFlags(cmd)
	return cmd
}

func followersCmd(a *auth.Auth) *cobra.Command {
	var maxResults int
	var targetUser string
	cmd := &cobra.Command{
		Use:   "followers",
		Short: "List your followers",
		Long: `Fetch the list of your (or another user's) followers.

Examples:
  xurl followers
  xurl followers --of elonmusk -n 50`,
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			var userID string
			var err error
			if targetUser != "" {
				userID, err = resolveUserID(client, targetUser, opts)
			} else {
				userID, err = resolveMyUserID(client, opts)
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			printResult(api.GetFollowers(client, userID, maxResults, opts))
		},
	}
	cmd.Flags().IntVarP(&maxResults, "max-results", "n", 10, "Number of results (1–1000)")
	cmd.Flags().StringVar(&targetUser, "of", "", "Username to list followers for (default: you)")
	addCommonFlags(cmd)
	return cmd
}

func blockCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "block USERNAME",
		Short: "Block a user",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			myID, err := resolveMyUserID(client, opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			targetID, err := resolveUserID(client, args[0], opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			printResult(api.BlockUser(client, myID, targetID, opts))
		},
	}
	addCommonFlags(cmd)
	return cmd
}

func unblockCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unblock USERNAME",
		Short: "Unblock a user",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			myID, err := resolveMyUserID(client, opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			targetID, err := resolveUserID(client, args[0], opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			printResult(api.UnblockUser(client, myID, targetID, opts))
		},
	}
	addCommonFlags(cmd)
	return cmd
}

func muteCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mute USERNAME",
		Short: "Mute a user",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			myID, err := resolveMyUserID(client, opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			targetID, err := resolveUserID(client, args[0], opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			printResult(api.MuteUser(client, myID, targetID, opts))
		},
	}
	addCommonFlags(cmd)
	return cmd
}

func unmuteCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unmute USERNAME",
		Short: "Unmute a user",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			myID, err := resolveMyUserID(client, opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			targetID, err := resolveUserID(client, args[0], opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			printResult(api.UnmuteUser(client, myID, targetID, opts))
		},
	}
	addCommonFlags(cmd)
	return cmd
}

// =================================================================
//  DIRECT MESSAGES
// =================================================================

func dmCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   `dm USERNAME "TEXT"`,
		Short: "Send a direct message",
		Long: `Send a direct message to a user.

Examples:
  xurl dm @elonmusk "Hey, great post!"
  xurl dm someuser "Hello there"`,
		Args: cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			targetID, err := resolveUserID(client, args[0], opts)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			printResult(api.SendDM(client, targetID, args[1], opts))
		},
	}
	addCommonFlags(cmd)
	return cmd
}

func dmsCmd(a *auth.Auth) *cobra.Command {
	var maxResults int
	cmd := &cobra.Command{
		Use:   "dms",
		Short: "List recent direct messages",
		Long: `Fetch your recent direct message events.

Examples:
  xurl dms
  xurl dms -n 25`,
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			client := newClient(a)
			opts := baseOpts(cmd)
			printResult(api.GetDMEvents(client, maxResults, opts))
		},
	}
	cmd.Flags().IntVarP(&maxResults, "max-results", "n", 10, "Number of results (1–100)")
	addCommonFlags(cmd)
	return cmd
}
