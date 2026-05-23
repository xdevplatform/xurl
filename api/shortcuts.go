package api

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// ------------------------------------------------
// Request‑body helpers for the X API v2 shortcuts
// ------------------------------------------------

// PostBody is the JSON body for POST /2/tweets
type PostBody struct {
	Text  string     `json:"text"`
	Reply *PostReply `json:"reply,omitempty"`
	Quote *string    `json:"quote_tweet_id,omitempty"` // API field name — do not rename
	Media *PostMedia `json:"media,omitempty"`
	Poll  *PostPoll  `json:"poll,omitempty"`
}

// PostReply nests inside PostBody for replies
type PostReply struct {
	InReplyToPostID string `json:"in_reply_to_tweet_id"` // API field name — do not rename
}

// PostMedia nests inside PostBody to attach uploaded media
type PostMedia struct {
	MediaIDs []string `json:"media_ids"`
}

// PostPoll nests inside PostBody to create a poll
type PostPoll struct {
	Options         []string `json:"options"`
	DurationMinutes int      `json:"duration_minutes"`
}

// ------------------------------------------------
// Helpers
// ------------------------------------------------

// ResolvePostID extracts a post ID from a full URL or returns the input as‑is.
// Accepts:
//   - https://x.com/user/status/123456
//   - https://x.com/user/status/123456  (legacy domain also works)
//   - 123456
func ResolvePostID(input string) string {
	input = strings.TrimSpace(input)

	// If it looks like a URL, pull the last path segment after "status"
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		parsed, err := url.Parse(input)
		if err == nil {
			parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
			for i, p := range parts {
				if p == "status" && i+1 < len(parts) {
					return parts[i+1]
				}
			}
		}
	}
	return input
}

// ResolveUsername normalises a username – strips a leading "@" if present.
func ResolveUsername(input string) string {
	return strings.TrimPrefix(strings.TrimSpace(input), "@")
}

// ------------------------------------------------
// Shortcut executors
// ------------------------------------------------

// CreatePost sends a new post and returns the API response.
func CreatePost(client Client, text string, mediaIDs []string, opts RequestOptions) (json.RawMessage, error) {
	body := PostBody{Text: text}
	if len(mediaIDs) > 0 {
		body.Media = &PostMedia{MediaIDs: mediaIDs}
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal post body: %w", err)
	}

	opts.Method = "POST"
	opts.Endpoint = "/2/tweets"
	opts.Data = string(data)

	return client.SendRequest(opts)
}

// ReplyToPost sends a reply to an existing post.
func ReplyToPost(client Client, postID, text string, mediaIDs []string, opts RequestOptions) (json.RawMessage, error) {
	postID = ResolvePostID(postID)

	body := PostBody{
		Text:  text,
		Reply: &PostReply{InReplyToPostID: postID},
	}
	if len(mediaIDs) > 0 {
		body.Media = &PostMedia{MediaIDs: mediaIDs}
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal reply body: %w", err)
	}

	opts.Method = "POST"
	opts.Endpoint = "/2/tweets"
	opts.Data = string(data)

	return client.SendRequest(opts)
}

// QuotePost sends a quote post.
func QuotePost(client Client, postID, text string, opts RequestOptions) (json.RawMessage, error) {
	postID = ResolvePostID(postID)

	body := PostBody{
		Text:  text,
		Quote: &postID,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal quote body: %w", err)
	}

	opts.Method = "POST"
	opts.Endpoint = "/2/tweets"
	opts.Data = string(data)

	return client.SendRequest(opts)
}

// DeletePost deletes a post by ID.
func DeletePost(client Client, postID string, opts RequestOptions) (json.RawMessage, error) {
	postID = ResolvePostID(postID)

	opts.Method = "DELETE"
	opts.Endpoint = fmt.Sprintf("/2/tweets/%s", postID)
	opts.Data = ""

	return client.SendRequest(opts)
}

// ReadPost fetches a single post with useful expansions.
func ReadPost(client Client, postID string, opts RequestOptions) (json.RawMessage, error) {
	postID = ResolvePostID(postID)

	opts.Method = "GET"
	opts.Endpoint = fmt.Sprintf("/2/tweets/%s?tweet.fields=created_at,public_metrics,conversation_id,in_reply_to_user_id,referenced_tweets,entities,attachments&expansions=author_id,referenced_tweets.id&user.fields=username,name,verified", postID)
	opts.Data = ""

	return client.SendRequest(opts)
}

// PaginationOptions holds optional pagination and field-override flags
// shared by search, timeline, and mentions commands.
type PaginationOptions struct {
	SinceID     string
	UntilID     string
	StartTime   string
	EndTime     string
	NextToken   string
	TweetFields string
	UserFields  string
	Expansions  string
	Exclude     string // timeline/mentions only: "replies,retweets"
}

// SearchPosts searches recent posts.
func SearchPosts(client Client, query string, maxResults int, pag PaginationOptions, opts RequestOptions) (json.RawMessage, error) {
	q := url.QueryEscape(query)

	// X API enforces min 10 / max 100 for search
	if maxResults < 10 {
		maxResults = 10
	} else if maxResults > 100 {
		maxResults = 100
	}

	// Defaults
	tweetFields := "created_at,public_metrics,conversation_id,entities"
	userFields := "username,name,verified"
	expansions := "author_id"
	if pag.TweetFields != "" {
		tweetFields = pag.TweetFields
	}
	if pag.UserFields != "" {
		userFields = pag.UserFields
	}
	if pag.Expansions != "" {
		expansions = pag.Expansions
	}

	endpoint := fmt.Sprintf("/2/tweets/search/recent?query=%s&max_results=%d&tweet.fields=%s&expansions=%s&user.fields=%s",
		q, maxResults, tweetFields, expansions, userFields)

	if pag.SinceID != "" {
		endpoint += "&since_id=" + pag.SinceID
	}
	if pag.UntilID != "" {
		endpoint += "&until_id=" + pag.UntilID
	}
	if pag.StartTime != "" {
		endpoint += "&start_time=" + url.QueryEscape(pag.StartTime)
	}
	if pag.EndTime != "" {
		endpoint += "&end_time=" + url.QueryEscape(pag.EndTime)
	}
	if pag.NextToken != "" {
		endpoint += "&next_token=" + pag.NextToken
	}

	opts.Method = "GET"
	opts.Endpoint = endpoint
	opts.Data = ""

	return client.SendRequest(opts)
}

// GetMe fetches the authenticated user's profile.
func GetMe(client Client, opts RequestOptions) (json.RawMessage, error) {
	opts.Method = "GET"
	opts.Endpoint = "/2/users/me?user.fields=created_at,description,public_metrics,verified,profile_image_url"
	opts.Data = ""

	return client.SendRequest(opts)
}

// LookupUser fetches a user by username.
func LookupUser(client Client, username string, opts RequestOptions) (json.RawMessage, error) {
	username = ResolveUsername(username)

	opts.Method = "GET"
	opts.Endpoint = fmt.Sprintf("/2/users/by/username/%s?user.fields=created_at,description,public_metrics,verified,profile_image_url", username)
	opts.Data = ""

	return client.SendRequest(opts)
}

// GetUserPosts fetches recent posts by a user ID.
func GetUserPosts(client Client, userID string, maxResults int, opts RequestOptions) (json.RawMessage, error) {
	opts.Method = "GET"
	opts.Endpoint = fmt.Sprintf("/2/users/%s/tweets?max_results=%d&tweet.fields=created_at,public_metrics,conversation_id,entities&expansions=referenced_tweets.id", userID, maxResults)
	opts.Data = ""

	return client.SendRequest(opts)
}

// GetTimeline fetches the authenticated user's reverse‑chronological timeline.
// Route: GET /2/users/{id}/timelines/reverse_chronological
func GetTimeline(client Client, userID string, maxResults int, pag PaginationOptions, opts RequestOptions) (json.RawMessage, error) {
	tweetFields := "created_at,public_metrics,conversation_id,entities"
	userFields := "username,name"
	expansions := "author_id"
	if pag.TweetFields != "" {
		tweetFields = pag.TweetFields
	}
	if pag.UserFields != "" {
		userFields = pag.UserFields
	}
	if pag.Expansions != "" {
		expansions = pag.Expansions
	}

	endpoint := fmt.Sprintf("/2/users/%s/timelines/reverse_chronological?max_results=%d&tweet.fields=%s&expansions=%s&user.fields=%s",
		userID, maxResults, tweetFields, expansions, userFields)

	if pag.SinceID != "" {
		endpoint += "&since_id=" + pag.SinceID
	}
	if pag.UntilID != "" {
		endpoint += "&until_id=" + pag.UntilID
	}
	if pag.StartTime != "" {
		endpoint += "&start_time=" + url.QueryEscape(pag.StartTime)
	}
	if pag.EndTime != "" {
		endpoint += "&end_time=" + url.QueryEscape(pag.EndTime)
	}
	if pag.NextToken != "" {
		endpoint += "&pagination_token=" + pag.NextToken
	}
	if pag.Exclude != "" {
		endpoint += "&exclude=" + pag.Exclude
	}

	opts.Method = "GET"
	opts.Endpoint = endpoint
	opts.Data = ""

	return client.SendRequest(opts)
}

// GetMentions fetches recent mentions for a user.
func GetMentions(client Client, userID string, maxResults int, pag PaginationOptions, opts RequestOptions) (json.RawMessage, error) {
	tweetFields := "created_at,public_metrics,conversation_id,entities"
	userFields := "username,name"
	expansions := "author_id"
	if pag.TweetFields != "" {
		tweetFields = pag.TweetFields
	}
	if pag.UserFields != "" {
		userFields = pag.UserFields
	}
	if pag.Expansions != "" {
		expansions = pag.Expansions
	}

	endpoint := fmt.Sprintf("/2/users/%s/mentions?max_results=%d&tweet.fields=%s&expansions=%s&user.fields=%s",
		userID, maxResults, tweetFields, expansions, userFields)

	if pag.SinceID != "" {
		endpoint += "&since_id=" + pag.SinceID
	}
	if pag.UntilID != "" {
		endpoint += "&until_id=" + pag.UntilID
	}
	if pag.StartTime != "" {
		endpoint += "&start_time=" + url.QueryEscape(pag.StartTime)
	}
	if pag.EndTime != "" {
		endpoint += "&end_time=" + url.QueryEscape(pag.EndTime)
	}
	if pag.NextToken != "" {
		endpoint += "&pagination_token=" + pag.NextToken
	}
	if pag.Exclude != "" {
		endpoint += "&exclude=" + pag.Exclude
	}

	opts.Method = "GET"
	opts.Endpoint = endpoint
	opts.Data = ""

	return client.SendRequest(opts)
}

// LikePost likes a post on behalf of the authenticated user.
func LikePost(client Client, userID, postID string, opts RequestOptions) (json.RawMessage, error) {
	postID = ResolvePostID(postID)

	body := fmt.Sprintf(`{"tweet_id":"%s"}`, postID) // API field name — do not rename

	opts.Method = "POST"
	opts.Endpoint = fmt.Sprintf("/2/users/%s/likes", userID)
	opts.Data = body

	return client.SendRequest(opts)
}

// UnlikePost unlikes a post.
func UnlikePost(client Client, userID, postID string, opts RequestOptions) (json.RawMessage, error) {
	postID = ResolvePostID(postID)

	opts.Method = "DELETE"
	opts.Endpoint = fmt.Sprintf("/2/users/%s/likes/%s", userID, postID)
	opts.Data = ""

	return client.SendRequest(opts)
}

// Repost reposts a post.
func Repost(client Client, userID, postID string, opts RequestOptions) (json.RawMessage, error) {
	postID = ResolvePostID(postID)

	body := fmt.Sprintf(`{"tweet_id":"%s"}`, postID) // API field name — do not rename

	opts.Method = "POST"
	opts.Endpoint = fmt.Sprintf("/2/users/%s/retweets", userID)
	opts.Data = body

	return client.SendRequest(opts)
}

// Unrepost removes a repost.
func Unrepost(client Client, userID, postID string, opts RequestOptions) (json.RawMessage, error) {
	postID = ResolvePostID(postID)

	opts.Method = "DELETE"
	opts.Endpoint = fmt.Sprintf("/2/users/%s/retweets/%s", userID, postID)
	opts.Data = ""

	return client.SendRequest(opts)
}

// Bookmark bookmarks a post.
func Bookmark(client Client, userID, postID string, opts RequestOptions) (json.RawMessage, error) {
	postID = ResolvePostID(postID)

	body := fmt.Sprintf(`{"tweet_id":"%s"}`, postID) // API field name — do not rename

	opts.Method = "POST"
	opts.Endpoint = fmt.Sprintf("/2/users/%s/bookmarks", userID)
	opts.Data = body

	return client.SendRequest(opts)
}

// Unbookmark removes a bookmark.
func Unbookmark(client Client, userID, postID string, opts RequestOptions) (json.RawMessage, error) {
	postID = ResolvePostID(postID)

	opts.Method = "DELETE"
	opts.Endpoint = fmt.Sprintf("/2/users/%s/bookmarks/%s", userID, postID)
	opts.Data = ""

	return client.SendRequest(opts)
}

// GetBookmarks fetches the authenticated user's bookmarks.
func GetBookmarks(client Client, userID string, maxResults int, opts RequestOptions) (json.RawMessage, error) {
	opts.Method = "GET"
	opts.Endpoint = fmt.Sprintf("/2/users/%s/bookmarks?max_results=%d&tweet.fields=created_at,public_metrics,entities&expansions=author_id&user.fields=username,name", userID, maxResults)
	opts.Data = ""

	return client.SendRequest(opts)
}

// FollowUser follows a user.
func FollowUser(client Client, sourceUserID, targetUserID string, opts RequestOptions) (json.RawMessage, error) {
	body := fmt.Sprintf(`{"target_user_id":"%s"}`, targetUserID)

	opts.Method = "POST"
	opts.Endpoint = fmt.Sprintf("/2/users/%s/following", sourceUserID)
	opts.Data = body

	return client.SendRequest(opts)
}

// UnfollowUser unfollows a user.
func UnfollowUser(client Client, sourceUserID, targetUserID string, opts RequestOptions) (json.RawMessage, error) {
	opts.Method = "DELETE"
	opts.Endpoint = fmt.Sprintf("/2/users/%s/following/%s", sourceUserID, targetUserID)
	opts.Data = ""

	return client.SendRequest(opts)
}

// GetFollowing fetches users that a given user follows.
func GetFollowing(client Client, userID string, maxResults int, opts RequestOptions) (json.RawMessage, error) {
	opts.Method = "GET"
	opts.Endpoint = fmt.Sprintf("/2/users/%s/following?max_results=%d&user.fields=created_at,description,public_metrics,verified", userID, maxResults)
	opts.Data = ""

	return client.SendRequest(opts)
}

// GetFollowers fetches followers of a given user.
func GetFollowers(client Client, userID string, maxResults int, opts RequestOptions) (json.RawMessage, error) {
	opts.Method = "GET"
	opts.Endpoint = fmt.Sprintf("/2/users/%s/followers?max_results=%d&user.fields=created_at,description,public_metrics,verified", userID, maxResults)
	opts.Data = ""

	return client.SendRequest(opts)
}

// SendDM sends a direct message to a user.
func SendDM(client Client, participantID, text string, opts RequestOptions) (json.RawMessage, error) {
	body := fmt.Sprintf(`{"text":"%s"}`, strings.ReplaceAll(text, `"`, `\"`))

	opts.Method = "POST"
	opts.Endpoint = fmt.Sprintf("/2/dm_conversations/with/%s/messages", participantID)
	opts.Data = body

	return client.SendRequest(opts)
}

// GetDMEvents fetches recent DM events.
func GetDMEvents(client Client, maxResults int, opts RequestOptions) (json.RawMessage, error) {
	opts.Method = "GET"
	opts.Endpoint = fmt.Sprintf("/2/dm_events?max_results=%d&dm_event.fields=created_at,dm_conversation_id,sender_id,text&expansions=sender_id&user.fields=username,name", maxResults)
	opts.Data = ""

	return client.SendRequest(opts)
}

// GetLikedPosts fetches posts liked by a user.
func GetLikedPosts(client Client, userID string, maxResults int, opts RequestOptions) (json.RawMessage, error) {
	opts.Method = "GET"
	opts.Endpoint = fmt.Sprintf("/2/users/%s/liked_tweets?max_results=%d&tweet.fields=created_at,public_metrics,entities&expansions=author_id&user.fields=username,name", userID, maxResults)
	opts.Data = ""

	return client.SendRequest(opts)
}

// BlockUser blocks a user.
func BlockUser(client Client, sourceUserID, targetUserID string, opts RequestOptions) (json.RawMessage, error) {
	body := fmt.Sprintf(`{"target_user_id":"%s"}`, targetUserID)

	opts.Method = "POST"
	opts.Endpoint = fmt.Sprintf("/2/users/%s/blocking", sourceUserID)
	opts.Data = body

	return client.SendRequest(opts)
}

// UnblockUser unblocks a user.
func UnblockUser(client Client, sourceUserID, targetUserID string, opts RequestOptions) (json.RawMessage, error) {
	opts.Method = "DELETE"
	opts.Endpoint = fmt.Sprintf("/2/users/%s/blocking/%s", sourceUserID, targetUserID)
	opts.Data = ""

	return client.SendRequest(opts)
}

// MuteUser mutes a user.
func MuteUser(client Client, sourceUserID, targetUserID string, opts RequestOptions) (json.RawMessage, error) {
	body := fmt.Sprintf(`{"target_user_id":"%s"}`, targetUserID)

	opts.Method = "POST"
	opts.Endpoint = fmt.Sprintf("/2/users/%s/muting", sourceUserID)
	opts.Data = body

	return client.SendRequest(opts)
}

// UnmuteUser unmutes a user.
func UnmuteUser(client Client, sourceUserID, targetUserID string, opts RequestOptions) (json.RawMessage, error) {
	opts.Method = "DELETE"
	opts.Endpoint = fmt.Sprintf("/2/users/%s/muting/%s", sourceUserID, targetUserID)
	opts.Data = ""

	return client.SendRequest(opts)
}
