// api/tweets.go
package api

import (
	"fmt"
	"strings"
)

// TweetFields defines standard fields for tweet requests, including long-form support.
var DefaultTweetFields = []string{
	"created_at",
	"public_metrics",
	"conversation_id",
	"in_reply_to_user_id",
	"referenced_tweets",
	"entities",
	"attachments",
	"note_tweet",      // Long-form text (>280 chars)
	"article",         // Article metadata
	"quoted_status",   // For quoted long-form posts
}

// TweetOptions configures tweet-related requests.
type TweetOptions struct {
	Fields     []string
	Expansions []string
}

// NewTweetOptions returns options with long-form and article support enabled by default.
func NewTweetOptions() *TweetOptions {
	return &TweetOptions{
		Fields: DefaultTweetFields,
		Expansions: []string{
			"author_id",
			"referenced_tweets.id",
			"quoted_status_id",
			"attachments.media_keys",
		},
	}
}

// GetTweet builds a request for a single tweet with full long-form support.
func (c *Client) GetTweet(tweetID string, opts *TweetOptions) (*TweetResponse, error) {
	if opts == nil {
		opts = NewTweetOptions()
	}

	fieldsStr := strings.Join(opts.Fields, ",")
	expansionsStr := strings.Join(opts.Expansions, ",")

	endpoint := fmt.Sprintf("/2/tweets/%s?tweet.fields=%s&expansions=%s&user.fields=username,name,verified",
		tweetID, fieldsStr, expansionsStr)

	var resp TweetResponse
	err := c.execute("GET", endpoint, nil, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// EnrichWithTickerLinks adds Solana-style graph links for $TICKER symbols (similar to stock tickers).
func EnrichWithTickerLinks(text string) string {
	// Simple regex replacement for $TICKER → rich link to Solana graph (e.g. Dexscreener or custom)
	// In production, use a proper regex + HTML/JSON enrichment
	re := regexp.MustCompile(`\$([A-Z0-9]+)`)
	return re.ReplaceAllStringFunc(text, func(match string) string {
		ticker := strings.TrimPrefix(match, "$")
		// Example: link to Solana graph/chart
		link := fmt.Sprintf("https://dexscreener.com/solana/%s", strings.ToLower(ticker))
		return fmt.Sprintf(`<a href="%s" target="_blank" class="ticker-link">$%s 📈</a>`, link, ticker)
	})
}

// TweetResponse represents the enriched tweet data including long-form content.
type TweetResponse struct {
	Data struct {
		ID           string `json:"id"`
		Text         string `json:"text"`
		NoteTweet    *struct {
			Text string `json:"text"`
		} `json:"note_tweet"`
		Article *struct {
			ID          string `json:"id"`
			Title       string `json:"title"`
			Preview     string `json:"preview"`
			PlainText   string `json:"plain_text,omitempty"`
		} `json:"article"`
		QuotedStatus *TweetResponse `json:"quoted_status"` // Recursive for quoted long-form
		Entities     *struct {
			Symbols []struct {
				Text    string `json:"text"`
				Indices []int  `json:"indices"`
			} `
