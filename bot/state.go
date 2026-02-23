package bot

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

// maxProcessedIDs is the maximum number of processed tweet IDs to keep in state (M3).
const maxProcessedIDs = 1000

// BotState tracks polling progress to avoid reprocessing tweets.
type BotState struct {
	SinceID      string            `yaml:"since_id,omitempty"`
	ProcessedIDs map[string]string `yaml:"processed_ids,omitempty"` // tweet_id -> "success"|"failed"|"skipped"
	LastPollTime time.Time         `yaml:"last_poll_time,omitempty"`
	filePath     string
}

// DefaultStatePath returns the default path for the bot state file.
func DefaultStatePath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "."
	}
	return filepath.Join(homeDir, ".xurl-bot-state")
}

// LoadState reads the bot state from ~/.xurl-bot-state.
func LoadState() *BotState {
	return LoadStateFromPath(DefaultStatePath())
}

// LoadStateFromPath reads the bot state from the given path.
func LoadStateFromPath(path string) *BotState {
	state := &BotState{
		ProcessedIDs: make(map[string]string),
		filePath:     path,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return state
	}

	if err := yaml.Unmarshal(data, state); err != nil {
		return state
	}

	if state.ProcessedIDs == nil {
		state.ProcessedIDs = make(map[string]string)
	}
	state.filePath = path
	return state
}

// Save writes the bot state to disk with file locking (M6) and pruning (M3).
func (s *BotState) Save() error {
	// M3: Prune old processed IDs to prevent unbounded growth
	s.pruneProcessedIDs()

	data, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	// M6: Use file locking to prevent race conditions
	f, err := os.OpenFile(s.filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("opening state file: %w", err)
	}
	defer f.Close()

	// Advisory lock — blocks if another process holds it
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("locking state file: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("writing state: %w", err)
	}
	return nil
}

// pruneProcessedIDs keeps only the most recent maxProcessedIDs entries (M3).
// Uses tweet ID ordering (higher ID = more recent).
func (s *BotState) pruneProcessedIDs() {
	if len(s.ProcessedIDs) <= maxProcessedIDs {
		return
	}

	// Collect IDs and sort descending (newest first)
	ids := make([]string, 0, len(s.ProcessedIDs))
	for id := range s.ProcessedIDs {
		ids = append(ids, id)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))

	// Keep only the newest entries
	pruned := make(map[string]string, maxProcessedIDs)
	for i := 0; i < maxProcessedIDs && i < len(ids); i++ {
		pruned[ids[i]] = s.ProcessedIDs[ids[i]]
	}
	s.ProcessedIDs = pruned
}

// IsProcessed checks if a tweet has already been processed.
func (s *BotState) IsProcessed(tweetID string) bool {
	_, ok := s.ProcessedIDs[tweetID]
	return ok
}

// MarkProcessed records a tweet as processed with the given status.
func (s *BotState) MarkProcessed(tweetID, status string) {
	s.ProcessedIDs[tweetID] = status
}

// numericID validates that a string is purely numeric (tweet IDs are numeric).
var numericID = regexp.MustCompile(`^[0-9]+$`)

// UpdateSinceID updates the since_id if the given ID is newer (higher).
// H4: Only accepts numeric IDs.
func (s *BotState) UpdateSinceID(tweetID string) {
	if !numericID.MatchString(tweetID) {
		return // Silently reject non-numeric IDs
	}
	if s.SinceID == "" || tweetID > s.SinceID {
		s.SinceID = tweetID
	}
}
