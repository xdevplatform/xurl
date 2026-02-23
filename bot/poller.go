package bot

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/xdevplatform/xurl/api"
)

// Poller monitors X for trigger tweets and dispatches the handler.
type Poller struct {
	Config  *BotConfig
	State   *BotState
	Client  api.Client
	Opts    api.RequestOptions
	Handler *Handler
	Logger  *log.Logger
}

// Run starts the polling loop. It blocks until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) error {
	p.Logger.Printf("[BOT] Started polling for '%s' triggers from @%s", p.Config.TriggerKeyword, p.Config.Handle)
	p.Logger.Printf("[BOT] Repo: %s | Agent: %s | Interval: %s", p.Config.Repo, p.Config.Agent, p.Config.PollInterval)

	if p.Config.DryRun {
		p.Logger.Printf("[BOT] DRY-RUN mode enabled — no agents will run, no replies will be posted")
	}

	backoff := p.Config.PollInterval

	for {
		select {
		case <-ctx.Done():
			p.Logger.Printf("[BOT] Shutting down gracefully...")
			return nil
		default:
		}

		err := p.poll(ctx)
		if err != nil {
			p.Logger.Printf("[ERROR] Poll failed: %v", err)
			// Exponential backoff on error (up to 5 minutes)
			backoff = backoff * 2
			if backoff > 5*time.Minute {
				backoff = 5 * time.Minute
			}
			p.Logger.Printf("[BOT] Backing off for %s", backoff)
		} else {
			backoff = p.Config.PollInterval // Reset on success
		}

		select {
		case <-ctx.Done():
			p.Logger.Printf("[BOT] Shutting down gracefully...")
			return nil
		case <-time.After(backoff):
		}
	}
}

// RunOnce performs a single poll cycle and returns.
func (p *Poller) RunOnce(ctx context.Context) error {
	p.Logger.Printf("[BOT] Running single poll cycle...")
	return p.poll(ctx)
}

// poll performs one search → process cycle.
func (p *Poller) poll(ctx context.Context) error {
	// Build search query: tweets from the founder containing the trigger keyword
	query := fmt.Sprintf("from:%s \"%s\"", p.Config.Handle, p.Config.TriggerKeyword)

	p.Logger.Printf("[POLL] Searching: %s (since_id: %s)", query, p.State.SinceID)

	resp, err := api.SearchRecentWithSinceID(p.Client, query, p.State.SinceID, 10, p.Opts)
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}

	tweets, err := ParseSearchResponse(resp, p.Config.TriggerKeyword)
	if err != nil {
		return fmt.Errorf("parse failed: %w", err)
	}

	if len(tweets) == 0 {
		p.Logger.Printf("[POLL] No new triggers found")
		p.State.LastPollTime = time.Now()
		return p.State.Save()
	}

	p.Logger.Printf("[POLL] Found %d new trigger(s)", len(tweets))

	// Sort by ID (ascending) to process oldest first
	sort.Slice(tweets, func(i, j int) bool {
		return tweets[i].ID < tweets[j].ID
	})

	for _, tweet := range tweets {
		// Skip already processed
		if p.State.IsProcessed(tweet.ID) {
			continue
		}

		// Process the tweet
		if err := p.Handler.Process(ctx, tweet); err != nil {
			p.Logger.Printf("[ERROR] Processing tweet %s: %v", tweet.ID, err)
			// Continue with remaining tweets
		}

		// Update since_id to track progress
		p.State.UpdateSinceID(tweet.ID)
		_ = p.State.Save()
	}

	p.State.LastPollTime = time.Now()
	return p.State.Save()
}
