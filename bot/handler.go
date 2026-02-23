package bot

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/xdevplatform/xurl/api"
)

// maxMediaFileSize is the maximum size of a single media download (50MB).
const maxMediaFileSize = 50 * 1024 * 1024

// allowedMediaHosts is the set of hosts we allow media downloads from.
// C2: Prevents SSRF by restricting downloads to known X/Twitter media domains.
var allowedMediaHosts = map[string]bool{
	"pbs.twimg.com":   true,
	"video.twimg.com": true,
	"abs.twimg.com":   true,
	"ton.twimg.com":   true,
}

// Handler processes individual tweets through the bot pipeline.
type Handler struct {
	Config *BotConfig
	State  *BotState
	Client api.Client
	Opts   api.RequestOptions
	Agent  Agent
	Logger *log.Logger
}

// Process handles a single trigger tweet: fetches the parent bug report,
// downloads media, runs the coding agent, and replies with the PR link.
func (h *Handler) Process(ctx context.Context, trigger ParsedTweet) error {
	h.Logger.Printf("[PROCESSING] Tweet %s from @%s: %s", trigger.ID, trigger.AuthorUsername, truncate(trigger.Text, 100))

	// 1. Fetch parent tweet (the actual bug report) if this is a reply
	var bugText string
	var mediaURLs []string
	var bugAuthor string

	if trigger.InReplyToID != "" {
		parent, err := FetchParentTweet(h.Client, trigger.InReplyToID, h.Opts)
		if err != nil {
			h.Logger.Printf("[WARN] Could not fetch parent tweet %s: %v (using trigger text only)", trigger.InReplyToID, err)
			bugText = trigger.BugDescription
			mediaURLs = trigger.MediaURLs
		} else {
			bugText = parent.Text
			bugAuthor = parent.AuthorUsername
			mediaURLs = parent.MediaURLs
			// Also include trigger tweet media if any
			mediaURLs = append(mediaURLs, trigger.MediaURLs...)
		}
	} else {
		// Direct tweet (not a reply) — use the trigger text itself
		bugText = trigger.BugDescription
		mediaURLs = trigger.MediaURLs
	}

	h.Logger.Printf("[BUG] %s", truncate(bugText, 200))
	if len(mediaURLs) > 0 {
		h.Logger.Printf("[MEDIA] %d attachment(s) found", len(mediaURLs))
	}

	// 2. Download media to temp dir
	var mediaFiles []string
	if len(mediaURLs) > 0 {
		var err error
		mediaFiles, err = downloadMedia(mediaURLs)
		if err != nil {
			h.Logger.Printf("[WARN] Media download failed: %v (continuing without media)", err)
		} else {
			defer cleanupMedia(mediaFiles)
		}
	}

	// 3. Generate branch name
	branchName := h.Config.BranchPrefix + trigger.ID

	// 4. Dry run check
	if h.Config.DryRun {
		h.Logger.Printf("[DRY-RUN] Would run %q agent for bug: %s", h.Config.Agent, truncate(bugText, 100))
		h.Logger.Printf("[DRY-RUN] Branch: %s, Repo: %s", branchName, h.Config.Repo)
		h.State.MarkProcessed(trigger.ID, "skipped")
		return h.State.Save()
	}

	// 5. Run the coding agent
	h.Logger.Printf("[AGENT] Running %s agent...", h.Agent.Name())

	founderNote := trigger.BugDescription
	prompt := bugText
	if bugAuthor != "" {
		prompt = fmt.Sprintf("Bug from @%s: %s", bugAuthor, bugText)
	}

	result, err := h.Agent.Run(ctx, prompt, mediaFiles, h.Config.Repo, branchName)
	if err != nil {
		h.Logger.Printf("[ERROR] Agent failed: %v", err)
		if result != nil && result.Output != "" {
			h.Logger.Printf("[AGENT OUTPUT] %s", truncate(result.Output, 500))
		}
		h.State.MarkProcessed(trigger.ID, "failed")
		_ = h.State.Save()
		return fmt.Errorf("agent failed: %w", err)
	}

	_ = founderNote // reserved for future enhanced prompts

	if result.PRLink != "" {
		h.Logger.Printf("[DONE] PR created: %s", result.PRLink)
	} else {
		h.Logger.Printf("[WARN] Agent completed but no PR link found")
	}

	// 6. Update state
	status := "success"
	if result.PRLink == "" {
		status = "no_pr"
	}
	h.State.MarkProcessed(trigger.ID, status)
	return h.State.Save()
}

// validateMediaURL checks that a URL is safe to download.
// C2: Prevents SSRF by restricting to HTTPS and known media hosts.
// M5: Enforces HTTPS.
func validateMediaURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// M5: HTTPS only
	if parsed.Scheme != "https" {
		return fmt.Errorf("non-HTTPS URL rejected: %s", parsed.Scheme)
	}

	host := parsed.Hostname()

	// C2: Only allow known X media hosts
	if !allowedMediaHosts[host] {
		return fmt.Errorf("blocked media host: %s (allowed: twimg.com domains)", host)
	}

	// C2: Verify host does not resolve to private/loopback IP
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("DNS lookup failed for %s: %w", host, err)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("blocked private/loopback IP for host %s", host)
		}
	}

	return nil
}

// downloadMedia downloads media from URLs to a temp directory.
func downloadMedia(urls []string) ([]string, error) {
	tmpDir, err := os.MkdirTemp("", "xurl-bot-media-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}

	var files []string
	for i, u := range urls {
		// C2+M5: Validate URL before downloading
		if err := validateMediaURL(u); err != nil {
			continue // Skip invalid URLs
		}

		ext := filepath.Ext(u)
		if ext == "" || len(ext) > 5 {
			ext = ".jpg"
		}
		// Strip query params from extension
		if idx := strings.Index(ext, "?"); idx != -1 {
			ext = ext[:idx]
		}

		filePath := filepath.Join(tmpDir, fmt.Sprintf("media_%d%s", i, ext))

		resp, err := http.Get(u) //nolint:gosec // URL validated above
		if err != nil {
			continue // Skip failed downloads
		}

		f, err := os.Create(filePath)
		if err != nil {
			resp.Body.Close()
			continue
		}

		// H1: Limit download size to prevent memory/disk exhaustion
		limited := io.LimitReader(resp.Body, maxMediaFileSize+1)
		n, err := io.Copy(f, limited)
		resp.Body.Close()
		f.Close()

		if n > maxMediaFileSize {
			os.Remove(filePath)
			continue // Skip oversized files
		}

		if err == nil {
			files = append(files, filePath)
		}
	}

	if len(files) == 0 && len(urls) > 0 {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("failed to download any media")
	}

	return files, nil
}

// cleanupMedia removes downloaded media files.
func cleanupMedia(files []string) {
	if len(files) == 0 {
		return
	}
	// All files are in the same temp dir
	dir := filepath.Dir(files[0])
	os.RemoveAll(dir)
}

// truncate shortens a string for log output and strips control characters (L3).
func truncate(s string, maxLen int) string {
	// L3: Strip control characters to prevent log injection/terminal escape sequences
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1 // Drop other control chars
		}
		return r
	}, s)
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
