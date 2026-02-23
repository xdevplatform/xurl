package bot

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xdevplatform/xurl/api"
)

// ParsedTweet holds the relevant fields from a tweet for bot processing.
type ParsedTweet struct {
	ID             string
	AuthorID       string
	AuthorUsername string
	Text           string
	BugDescription string   // Text after trigger keyword
	MediaURLs      []string // URLs of attached images/videos
	CreatedAt      string
	ConversationID string
	InReplyToID    string // Parent tweet ID (if this is a reply)
}

// searchResponse models the X API v2 search response structure.
type searchResponse struct {
	Data     []tweetData    `json:"data"`
	Includes *searchIncludes `json:"includes,omitempty"`
}

type tweetData struct {
	ID               string            `json:"id"`
	Text             string            `json:"text"`
	AuthorID         string            `json:"author_id"`
	CreatedAt        string            `json:"created_at"`
	ConversationID   string            `json:"conversation_id"`
	ReferencedTweets []referencedTweet `json:"referenced_tweets,omitempty"`
	Attachments      *attachments      `json:"attachments,omitempty"`
}

type referencedTweet struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type attachments struct {
	MediaKeys []string `json:"media_keys,omitempty"`
}

type searchIncludes struct {
	Users  []userData  `json:"users,omitempty"`
	Media  []mediaData `json:"media,omitempty"`
	Tweets []tweetData `json:"tweets,omitempty"`
}

type userData struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

type mediaData struct {
	MediaKey        string `json:"media_key"`
	Type            string `json:"type"` // "photo", "video", "animated_gif"
	URL             string `json:"url,omitempty"`
	PreviewImageURL string `json:"preview_image_url,omitempty"`
}

// ParseSearchResponse parses an X API v2 search response into ParsedTweets.
func ParseSearchResponse(data json.RawMessage, triggerKeyword string) ([]ParsedTweet, error) {
	var resp searchResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing search response: %w", err)
	}

	if len(resp.Data) == 0 {
		return nil, nil
	}

	// Build lookup maps from includes
	userMap := make(map[string]string) // author_id -> username
	mediaMap := make(map[string]string) // media_key -> url

	if resp.Includes != nil {
		for _, u := range resp.Includes.Users {
			userMap[u.ID] = u.Username
		}
		for _, m := range resp.Includes.Media {
			url := m.URL
			if url == "" {
				url = m.PreviewImageURL
			}
			if url != "" {
				mediaMap[m.MediaKey] = url
			}
		}
	}

	var tweets []ParsedTweet
	for _, t := range resp.Data {
		pt := ParsedTweet{
			ID:             t.ID,
			AuthorID:       t.AuthorID,
			AuthorUsername: userMap[t.AuthorID],
			Text:           t.Text,
			BugDescription: ExtractBugDesc(t.Text, triggerKeyword),
			CreatedAt:      t.CreatedAt,
			ConversationID: t.ConversationID,
		}

		// Find parent tweet ID if this is a reply
		for _, ref := range t.ReferencedTweets {
			if ref.Type == "replied_to" {
				pt.InReplyToID = ref.ID
				break
			}
		}

		// Collect media URLs
		if t.Attachments != nil {
			for _, key := range t.Attachments.MediaKeys {
				if url, ok := mediaMap[key]; ok {
					pt.MediaURLs = append(pt.MediaURLs, url)
				}
			}
		}

		tweets = append(tweets, pt)
	}

	return tweets, nil
}

// ParseSingleTweet parses a single tweet response (from ReadPostWithMedia).
func ParseSingleTweet(data json.RawMessage) (*ParsedTweet, error) {
	var resp struct {
		Data     tweetData       `json:"data"`
		Includes *searchIncludes `json:"includes,omitempty"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing tweet response: %w", err)
	}

	// Build lookup maps
	userMap := make(map[string]string)
	mediaMap := make(map[string]string)
	if resp.Includes != nil {
		for _, u := range resp.Includes.Users {
			userMap[u.ID] = u.Username
		}
		for _, m := range resp.Includes.Media {
			url := m.URL
			if url == "" {
				url = m.PreviewImageURL
			}
			if url != "" {
				mediaMap[m.MediaKey] = url
			}
		}
	}

	t := resp.Data
	pt := &ParsedTweet{
		ID:             t.ID,
		AuthorID:       t.AuthorID,
		AuthorUsername: userMap[t.AuthorID],
		Text:           t.Text,
		CreatedAt:      t.CreatedAt,
		ConversationID: t.ConversationID,
	}

	for _, ref := range t.ReferencedTweets {
		if ref.Type == "replied_to" {
			pt.InReplyToID = ref.ID
			break
		}
	}

	if t.Attachments != nil {
		for _, key := range t.Attachments.MediaKeys {
			if url, ok := mediaMap[key]; ok {
				pt.MediaURLs = append(pt.MediaURLs, url)
			}
		}
	}

	return pt, nil
}

// FetchParentTweet fetches the parent tweet that the trigger tweet is replying to.
func FetchParentTweet(client api.Client, parentID string, opts api.RequestOptions) (*ParsedTweet, error) {
	resp, err := api.ReadPostWithMedia(client, parentID, opts)
	if err != nil {
		return nil, fmt.Errorf("fetching parent tweet %s: %w", parentID, err)
	}
	return ParseSingleTweet(resp)
}

// ExtractBugDesc extracts the bug description from a trigger tweet.
// It finds the trigger keyword and returns everything after it.
func ExtractBugDesc(text string, triggerKeyword string) string {
	lower := strings.ToLower(text)
	keyword := strings.ToLower(triggerKeyword)

	idx := strings.Index(lower, keyword)
	if idx == -1 {
		return strings.TrimSpace(text)
	}

	desc := text[idx+len(triggerKeyword):]
	return strings.TrimSpace(desc)
}
