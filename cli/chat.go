//go:build cgo && ((darwin && (amd64 || arm64)) || (linux && amd64))

package cli

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/xdevplatform/chat-xdk/go/chatxdk"
	"github.com/xdevplatform/xurl/api"
	"github.com/xdevplatform/xurl/auth"
	"github.com/xdevplatform/xurl/store"
	"github.com/xdevplatform/xurl/utils"
)

// chatSupported reports whether this build includes the XChat client.
const chatSupported = true

// CreateChatCommand creates the `chat` command family: an end-to-end
// encrypted XChat client backed by the chat-xdk crypto binding.
func CreateChatCommand(a *auth.Auth) *cobra.Command {
	chatCmd := &cobra.Command{
		Use:   "chat",
		Short: "Send and read end-to-end encrypted XChat messages",
		Long: `An end-to-end encrypted XChat client.

Quick start:
  xurl chat keys restore               Fetch your existing keys from Juicebox (one-time)
  xurl chat send @bob "hello"          Send an encrypted message
  xurl chat read @bob                  Read decrypted conversation history
  xurl chat conversations              List your chat inbox
  xurl chat listen @bob                Print new messages as they arrive

xurl never generates or registers encryption keys: the account must already
have XChat keys, registered by another XChat client. Bring them to this
machine with 'keys restore' (Juicebox PIN recovery) or 'keys import' (an
exported private-key blob).

Conversations can be addressed by @username, user id, or conversation id
(e.g. 123-456 or g123). Requires OAuth2 user authentication with the
dm.read and dm.write scopes (run 'xurl auth oauth2' first).`,
	}

	chatCmd.AddCommand(
		createChatKeysCommand(a),
		chatConversationsCmd(a),
		chatReadCmd(a),
		chatSendCmd(a),
		chatListenCmd(a),
	)
	return chatCmd
}

// -----------------------------------------------------------------
// chat keys
// -----------------------------------------------------------------

func createChatKeysCommand(a *auth.Auth) *cobra.Command {
	keysCmd := &cobra.Command{
		Use:   "keys",
		Short: "Manage XChat encryption keys",
		Long: `Manage the XChat private keys stored on this machine.

xurl never generates or registers keys — it only fetches keys that already
exist for the account (registered by another XChat client).`,
	}
	keysCmd.AddCommand(chatKeysStatusCmd(a), chatKeysRestoreCmd(a), chatKeysImportCmd(a))
	return keysCmd
}

func chatKeysStatusCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show local and registered XChat key status",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			s, err := newChatSession(a, cmd, false)
			exitOnError(err)
			defer s.Close()
			exitOnError(s.keyStatus())
		},
	}
	addCommonFlags(cmd)
	return cmd
}

func chatKeysRestoreCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Recover XChat keys from Juicebox onto this machine",
		Long: `Recovers the account's private keys from the Juicebox
PIN-protected recovery service and saves them locally (mode 600). The keys
must have been stored in Juicebox by another XChat client; this is a
read-only recovery — xurl never writes to Juicebox.`,
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			pin, _ := cmd.Flags().GetString("pin")
			s, err := newChatSession(a, cmd, false)
			exitOnError(err)
			defer s.Close()
			exitOnError(s.restoreKeys(pin))
		},
	}
	cmd.Flags().String("pin", "", "Recovery PIN (prompted interactively if omitted)")
	addCommonFlags(cmd)
	return cmd
}

func chatKeysImportCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import [PRIVATE_KEYS_B64]",
		Short: "Import an exported XChat private-key blob",
		Long: `Imports a base64 private-key blob exported by another XChat client
and saves it locally (mode 600). The blob is prompted for (without echo)
when not passed as an argument, so it stays out of shell history.

The key version is detected by matching the imported identity against the
public keys registered on the account; a blob whose keys are not registered
is rejected.`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			blob := ""
			if len(args) == 1 {
				blob = args[0]
			}
			s, err := newChatSession(a, cmd, false)
			exitOnError(err)
			defer s.Close()
			exitOnError(s.importKeys(blob))
		},
	}
	addCommonFlags(cmd)
	return cmd
}

// -----------------------------------------------------------------
// chat conversations / read / send / listen
// -----------------------------------------------------------------

func chatConversationsCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "conversations",
		Short: "List your XChat inbox",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			maxResults, _ := cmd.Flags().GetInt("max-results")
			asJSON, _ := cmd.Flags().GetBool("json")
			s, err := newChatSession(a, cmd, false)
			exitOnError(err)
			defer s.Close()
			exitOnError(s.listConversations(maxResults, asJSON))
		},
	}
	cmd.Flags().IntP("max-results", "n", 20, "Maximum number of conversations to list (1-100)")
	cmd.Flags().Bool("json", false, "Output the raw JSON response")
	addCommonFlags(cmd)
	return cmd
}

func chatReadCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "read CONVERSATION|@USERNAME",
		Short: "Read decrypted messages from a conversation",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			maxResults, _ := cmd.Flags().GetInt("max-results")
			asJSON, _ := cmd.Flags().GetBool("json")
			s, err := newChatSession(a, cmd, true)
			exitOnError(err)
			defer s.Close()
			convID, err := s.resolveConversation(args[0])
			exitOnError(err)
			exitOnError(s.readConversation(convID, maxResults, asJSON))
		},
	}
	cmd.Flags().IntP("max-results", "n", 50, "Maximum number of events to fetch (1-100)")
	cmd.Flags().Bool("json", false, "Output decrypted events as JSON")
	addCommonFlags(cmd)
	return cmd
}

func chatSendCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "send CONVERSATION|@USERNAME \"TEXT\"",
		Short: "Send an encrypted message",
		Long: `Encrypts and sends a message. On the first message of a 1:1
conversation, a conversation key is generated and distributed to both
participants automatically.`,
		Args: cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			s, err := newChatSession(a, cmd, true)
			exitOnError(err)
			defer s.Close()
			convID, err := s.resolveConversation(args[0])
			exitOnError(err)
			exitOnError(s.sendMessage(convID, args[1]))
		},
	}
	addCommonFlags(cmd)
	return cmd
}

func chatListenCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "listen CONVERSATION|@USERNAME",
		Short: "Print new messages as they arrive (Ctrl-C to stop)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			interval, _ := cmd.Flags().GetInt("interval")
			s, err := newChatSession(a, cmd, true)
			exitOnError(err)
			defer s.Close()
			convID, err := s.resolveConversation(args[0])
			exitOnError(err)
			exitOnError(s.listen(convID, time.Duration(interval)*time.Second))
		},
	}
	cmd.Flags().Int("interval", 3, "Polling interval in seconds")
	addCommonFlags(cmd)
	return cmd
}

// -----------------------------------------------------------------
// chatSession: one authenticated user + one unlocked chat-xdk instance
// -----------------------------------------------------------------

type chatSession struct {
	chat   *chatxdk.Chat
	client api.Client
	opts   api.RequestOptions
	store  *store.ChatKeyStore
	userID string

	// signingKeys accumulates every seen sender's keys; the merged set is
	// stored in the SDK so decrypt calls can pass nil.
	signingKeys []chatxdk.SigningKeyEntry
	seenSenders map[string]bool
	loadedConvs map[string]bool
	usernames   map[string]string
	// convKeys holds conversation keys (by version) recovered via ECIES-only
	// extraction when the verified key-adoption path fails; see
	// adoptKeyEvents.
	convKeys map[string][]byte
	// lastPrintedDay tracks day-separator state across printed events.
	lastPrintedDay string
}

// newChatSession resolves the authenticated user and creates a chat-xdk
// instance. When requireKeys is true the local private keys are imported and
// the session identity is set, erroring if no keys exist yet.
func newChatSession(a *auth.Auth, cmd *cobra.Command, requireKeys bool) (*chatSession, error) {
	client := newClient(a)
	opts := baseOpts(cmd)

	userID, err := resolveMyUserID(client, opts)
	if err != nil {
		return nil, err
	}

	keyStore := store.NewChatKeyStore()
	if err := keyStore.LoadErr(); err != nil {
		return nil, fmt.Errorf("the chat key store %s exists but could not be loaded — fix or remove it before continuing: %w", keyStore.FilePath(), err)
	}

	s := &chatSession{
		chat:        chatxdk.New(),
		client:      client,
		opts:        opts,
		store:       keyStore,
		userID:      userID,
		seenSenders: map[string]bool{},
		loadedConvs: map[string]bool{},
		usernames:   map[string]string{},
		convKeys:    map[string][]byte{},
	}
	s.chat.SetCacheKeys(true)

	// Chat identity is account-bound: always show who this session acts as,
	// so a stale default user is visible immediately instead of silently
	// reading another account's conversations. stderr keeps --json clean.
	color.New(color.Faint).Fprintf(os.Stderr, "acting as @%s (user %s) — switch with -u or 'xurl auth default'\n", s.username(s.userID), s.userID)

	if requireKeys {
		if err := s.unlock(); err != nil {
			keys := s.store.GetKeys(s.userID)
			hasNoKeys := keys == nil || keys.PrivateKeysB64 == ""
			if !hasNoKeys {
				// Keys exist but are unusable (e.g. corrupt) — never
				// auto-restore over them.
				s.Close()
				return nil, err
			}

			// The acting account has no keys on this machine. Distinguish
			// "wrong acting user" (keys exist here for another account)
			// from "new machine" so the user picks the right fix.
			if others := s.otherKeyHolders(); len(others) > 0 {
				fmt.Fprintf(os.Stderr, "This machine has XChat keys for %s, but you are acting as @%s.\n", strings.Join(others, ", "), s.username(s.userID))
				fmt.Fprintln(os.Stderr, "To act as that account instead, re-run with -u USERNAME or set it with 'xurl auth default'.")
			}

			// Otherwise (or additionally), offer the Juicebox PIN recovery
			// for the acting account right here; scripts get the error.
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				s.Close()
				return nil, err
			}
			fmt.Fprintf(os.Stderr, "No XChat keys on this machine for @%s. Enter the recovery PIN to fetch them from the account's backup (Ctrl-C to abort).\n", s.username(s.userID))
			if rerr := s.restoreKeys(""); rerr != nil {
				s.Close()
				return nil, fmt.Errorf("%w\n(no local keys for this account: run 'xurl chat keys restore' or 'xurl chat keys import')", rerr)
			}
			if uerr := s.unlock(); uerr != nil {
				s.Close()
				return nil, uerr
			}
		}
	}
	return s, nil
}

// otherKeyHolders returns "@handle (user id)" labels for accounts other than
// the acting user that have keys in the local store.
func (s *chatSession) otherKeyHolders() []string {
	var out []string
	for uid, keys := range s.store.Users {
		if uid == s.userID || keys == nil || keys.PrivateKeysB64 == "" {
			continue
		}
		out = append(out, fmt.Sprintf("@%s (user %s)", s.username(uid), uid))
	}
	sort.Strings(out)
	return out
}

func (s *chatSession) Close() {
	if s.chat != nil {
		s.chat.Close()
	}
}

// unlock imports the locally stored private keys and sets the session identity.
func (s *chatSession) unlock() error {
	keys := s.store.GetKeys(s.userID)
	if keys == nil || keys.PrivateKeysB64 == "" {
		return fmt.Errorf("no XChat keys found for user %s — bring your existing keys to this machine with 'xurl chat keys restore' (Juicebox PIN) or 'xurl chat keys import' (exported key blob)", s.userID)
	}
	raw, err := base64.StdEncoding.DecodeString(keys.PrivateKeysB64)
	if err != nil {
		return fmt.Errorf("stored chat keys are corrupt (%s): %w", s.store.FilePath(), err)
	}
	version := keys.KeyVersion
	if version == "" {
		version = "1"
	}
	if err := s.chat.ImportKeysWithVersion(raw, version); err != nil {
		return fmt.Errorf("failed to import chat keys: %w", err)
	}
	if err := s.chat.SetIdentity(s.userID, version); err != nil {
		return fmt.Errorf("failed to set chat identity: %w", err)
	}
	return nil
}

// -----------------------------------------------------------------
// Key management flows
// -----------------------------------------------------------------

func (s *chatSession) keyStatus() error {
	keys := s.store.GetKeys(s.userID)
	label := color.New(color.Faint)
	label.Print("user:         ")
	fmt.Printf("@%s (%s)\n", s.username(s.userID), s.userID)

	if keys == nil || keys.PrivateKeysB64 == "" {
		label.Print("local keys:   ")
		fmt.Println("none — run 'xurl chat keys restore' or 'xurl chat keys import'")
	} else {
		label.Print("local keys:   ")
		fmt.Printf("present — version %s (%s)\n", keys.KeyVersion, s.store.FilePath())
		if err := s.unlock(); err == nil {
			if fp, err := s.chat.GetPublicKeyFingerprint(); err == nil && fp != "" {
				label.Print("fingerprint:  ")
				fmt.Println(fp)
			}
		}
	}

	serverKeys, err := api.GetChatPublicKeys(s.client, s.userID, s.opts)
	if err != nil {
		return fmt.Errorf("could not fetch registered keys: %w", err)
	}
	if len(serverKeys) == 0 {
		color.New(color.Faint).Print("registered:   ")
		fmt.Println("none — register keys with another XChat client first (xurl never registers keys)")
		return nil
	}
	color.New(color.Faint).Print("registered:   ")
	fmt.Printf("%d key(s) on account\n", len(serverKeys))
	for _, k := range serverKeys {
		marker := ""
		// Errors just mean no local keys are loaded — no marker to show.
		if ok, err := s.chat.MatchesRegisteredKey(k.PublicKey); err == nil && ok {
			marker = color.GreenString("  ← this machine")
		}
		fmt.Printf("  version %s%s\n", k.Version, marker)
	}
	return nil
}

func (s *chatSession) restoreKeys(pin string) error {
	if existing := s.store.GetKeys(s.userID); existing != nil && existing.PrivateKeysB64 != "" {
		return fmt.Errorf("local keys already exist for user %s in %s — remove them first if you really want to restore over them", s.userID, s.store.FilePath())
	}

	serverKeys, err := api.GetChatPublicKeys(s.client, s.userID, s.opts)
	if err != nil {
		return err
	}
	// Use the config of the newest key version carrying one.
	var juiceboxCfg string
	var juiceboxVersion string
	for _, k := range serverKeys {
		if len(k.JuiceboxConfig) == 0 || string(k.JuiceboxConfig) == "null" {
			continue
		}
		if juiceboxCfg == "" || api.CompareChatKeyVersions(k.Version, juiceboxVersion) > 0 {
			juiceboxCfg = string(k.JuiceboxConfig)
			juiceboxVersion = k.Version
		}
	}
	if juiceboxCfg == "" {
		return fmt.Errorf("no Juicebox backup found on this account — keys were never backed up with a PIN")
	}

	if pin == "" {
		p, err := promptSecret("Recovery PIN: ")
		if err != nil {
			return err
		}
		pin = p
	}
	if pin == "" {
		return fmt.Errorf("a recovery PIN is required")
	}

	fmt.Println("Recovering keys from Juicebox...")
	if err := s.chat.Unlock([]byte(pin), juiceboxCfg); err != nil {
		return fmt.Errorf("Juicebox recovery failed (wrong PIN?): %w", err)
	}

	version, err := s.adoptSessionKeys(serverKeys)
	if err != nil {
		return err
	}
	color.Green("Keys recovered and saved to %s (version %s).", s.store.FilePath(), version)
	return nil
}

// importKeys imports a base64 private-key blob exported by another XChat
// client and saves it locally, adopting the key version of the matching
// registered public key.
func (s *chatSession) importKeys(blob string) error {
	if existing := s.store.GetKeys(s.userID); existing != nil && existing.PrivateKeysB64 != "" {
		return fmt.Errorf("local keys already exist for user %s in %s — remove them first if you really want to import over them", s.userID, s.store.FilePath())
	}

	if blob == "" {
		b, err := promptSecret("Private key blob (base64): ")
		if err != nil {
			return err
		}
		blob = b
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(blob))
	if err != nil {
		return fmt.Errorf("the key blob is not valid base64: %w", err)
	}
	if err := s.chat.ImportKeys(raw); err != nil {
		return fmt.Errorf("failed to import the key blob: %w", err)
	}

	serverKeys, err := api.GetChatPublicKeys(s.client, s.userID, s.opts)
	if err != nil {
		return err
	}

	version, err := s.adoptSessionKeys(serverKeys)
	if err != nil {
		return err
	}
	color.Green("Keys imported and saved to %s (version %s).", s.store.FilePath(), version)
	return nil
}

// adoptSessionKeys persists the keys currently loaded in the session,
// adopting the key version of the registered public key they match. Keys
// that are not registered on the account are rejected: xurl never registers
// keys itself, so unregistered keys could never send or verify messages.
func (s *chatSession) adoptSessionKeys(serverKeys []api.ChatPublicKey) (string, error) {
	version := ""
	for _, k := range serverKeys {
		if ok, err := s.chat.MatchesRegisteredKey(k.PublicKey); err == nil && ok {
			version = k.Version
			break
		}
	}
	if version == "" {
		return "", fmt.Errorf("these keys are not registered on account %s — xurl never registers keys, so only keys registered by another XChat client can be used", s.userID)
	}

	exported, err := s.chat.ExportKeys()
	if err != nil {
		return "", fmt.Errorf("failed to export keys: %w", err)
	}
	if err := s.store.SaveKeys(s.userID, &store.ChatKeys{
		PrivateKeysB64: base64.StdEncoding.EncodeToString(exported),
		KeyVersion:     version,
	}); err != nil {
		return "", err
	}
	return version, nil
}

// -----------------------------------------------------------------
// Conversation resolution
// -----------------------------------------------------------------

// resolveConversation turns @username / user id / conversation id input into
// a conversation id accepted by the API URL paths (hyphen form).
func (s *chatSession) resolveConversation(input string) (string, error) {
	input = strings.TrimSpace(input)
	// Already a conversation id: group (g + digits), 1:1 hyphen (a-b) or
	// colon (a:b). A bare word starting with "g" that is not all digits is a
	// username (e.g. "gandalf"), not a group id.
	if (strings.HasPrefix(input, "g") && isAllDigits(strings.TrimPrefix(input, "g"))) || strings.ContainsAny(input, "-:") {
		return api.ChatConversationPathID(input), nil
	}
	peerID := input
	if !isAllDigits(input) {
		id, err := resolveUserID(s.client, input, s.opts)
		if err != nil {
			return "", err
		}
		peerID = id
	}
	// Canonical 1:1 conversation id: both participant ids, ascending.
	a, b := s.userID, peerID
	if len(a) > len(b) || (len(a) == len(b) && a > b) {
		a, b = b, a
	}
	return a + "-" + b, nil
}

// latestKeyVersion returns the newest key version in a conversation-key map
// ("" when empty).
func latestKeyVersion(keys map[string][]byte) string {
	latest := ""
	for v := range keys {
		if latest == "" || api.CompareChatKeyVersions(v, latest) > 0 {
			latest = v
		}
	}
	return latest
}

// eventTimestamp extracts a sortable timestamp from a decrypted event
// (0 when the event carries none).
func eventTimestamp(e *chatxdk.Event) int64 {
	if msg := e.AsMessage(); msg != nil && msg.CreatedAtMsec != nil {
		return *msg.CreatedAtMsec
	}
	if kc := e.AsKeyChange(); kc != nil && kc.CreatedAtMsec != nil {
		return *kc.CreatedAtMsec
	}
	return 0
}

// isNotFoundError reports whether an API error indicates a missing
// conversation (which is expected before the first message of a 1:1).
func isNotFoundError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "404") || strings.Contains(msg, "not found") || strings.Contains(msg, "does not exist")
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// -----------------------------------------------------------------
// Inbox
// -----------------------------------------------------------------

func (s *chatSession) listConversations(maxResults int, asJSON bool) error {
	resp, err := api.GetChatConversations(s.client, maxResults, "", s.opts)
	if err != nil {
		return err
	}
	if asJSON {
		utils.FormatAndPrintResponse(resp)
		return nil
	}

	var out struct {
		Data []struct {
			ID             string   `json:"id"`
			Type           string   `json:"type"`
			GroupName      string   `json:"group_name"`
			ParticipantIDs []string `json:"participant_ids"`
			IsMuted        bool     `json:"is_muted"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp, &out); err != nil || len(out.Data) == 0 {
		// Unexpected shape or empty inbox: show the raw response.
		utils.FormatAndPrintResponse(resp)
		return nil
	}

	// Group names are encrypted with the conversation key, so decrypting
	// them needs the identity key. Unlock is soft here: without local keys
	// the inbox still lists, just with the names left encrypted.
	canDecrypt := s.unlock() == nil
	if canDecrypt {
		// Load signing keys for every participant up front (one batch call)
		// so the key-change events fetched below verify and yield the keys.
		var allParticipants []string
		for _, c := range out.Data {
			if c.GroupName != "" {
				allParticipants = append(allParticipants, c.ParticipantIDs...)
			}
		}
		s.loadSigningKeys(allParticipants)
	}

	type convRow struct {
		id, label string
		group     bool
		muted     bool
		encrypted bool
	}
	rows := make([]convRow, 0, len(out.Data))
	idWidth := 0
	for _, c := range out.Data {
		row := convRow{
			id:    api.ChatConversationPathID(c.ID),
			group: strings.HasPrefix(c.ID, "g"),
			muted: c.IsMuted,
		}
		if c.GroupName != "" {
			row.encrypted = true
			if canDecrypt {
				if name, err := s.decryptGroupName(c.ID, c.GroupName); err == nil {
					row.label = name
					row.encrypted = false
				} else if s.opts.Verbose {
					fmt.Fprintf(os.Stderr, "warning: could not decrypt name of %s: %v\n", c.ID, err)
				}
			}
		}
		if row.label == "" && !row.encrypted {
			var others []string
			for _, p := range c.ParticipantIDs {
				if p != s.userID {
					others = append(others, "@"+s.username(p))
				}
			}
			sort.Strings(others)
			row.label = strings.Join(others, ", ")
			if row.label == "" {
				row.label = "(just you)"
			}
		}
		if len(row.id) > idWidth {
			idWidth = len(row.id)
		}
		rows = append(rows, row)
	}

	idStyle := color.New(color.Faint)
	groupStyle := color.New(color.Bold)
	mutedStyle := color.New(color.Faint)
	for _, r := range rows {
		label := r.label
		switch {
		case r.encrypted:
			label = mutedStyle.Sprint("🔒 (encrypted group name — no key on this device)")
		case r.group:
			label = groupStyle.Sprint(r.label)
		}
		suffix := ""
		if r.muted {
			suffix = mutedStyle.Sprint("  (muted)")
		}
		fmt.Printf("%s  %s%s\n", idStyle.Sprintf("%-*s", idWidth, r.id), label, suffix)
	}
	return nil
}

// decryptGroupName decrypts an encrypted group name. Conversation keys come
// from a minimal events fetch: the page's out-of-band key-change events
// (meta.conversation_key_events) carry the keys regardless of how deep the
// key change sits in the history. Each candidate key version is tried newest
// first, since the metadata does not say which version encrypted the name.
func (s *chatSession) decryptGroupName(conversationID, encryptedName string) (string, error) {
	// A full page: the meta only reports the key-change events relevant to
	// the page's messages, so tiny pages can come back without any.
	page, err := api.GetChatEvents(s.client, conversationID, 100, "", s.opts)
	if err != nil {
		return "", err
	}
	events := page.KeyEvents
	for _, e := range page.Events {
		if e.EncodedEvent != "" {
			events = append(events, e.EncodedEvent)
		}
	}
	// ECIES-only extraction is enough here: a group name is metadata, and
	// only keys encrypted to this identity can decrypt at all.
	bundle, err := s.chat.ExtractConversationKeys(events)
	if err != nil {
		return "", err
	}
	keys := map[string][]byte{}
	for v, k := range s.convKeys {
		keys[v] = k
	}
	for v, k := range bundle.Keys {
		keys[v] = k
	}
	if len(keys) == 0 {
		return "", fmt.Errorf("no conversation key available")
	}
	versions := make([]string, 0, len(keys))
	for v := range keys {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool {
		return api.CompareChatKeyVersions(versions[i], versions[j]) > 0
	})
	var lastErr error
	for _, v := range versions {
		name, err := s.chat.Decrypt(encryptedName, keys[v])
		if err == nil {
			return name, nil
		}
		lastErr = err
	}
	return "", lastErr
}

// -----------------------------------------------------------------
// Reading & decryption
// -----------------------------------------------------------------

// loadSigningKeys batch-fetches public keys for users not seen before and
// stores the merged set in the SDK, so decrypt calls verify against it.
func (s *chatSession) loadSigningKeys(userIDs []string) {
	var missing []string
	seenInCall := map[string]bool{}
	for _, id := range userIDs {
		if id == "" || s.seenSenders[id] || seenInCall[id] {
			continue
		}
		seenInCall[id] = true
		missing = append(missing, id)
	}
	if len(missing) == 0 {
		return
	}
	added := false
	for start := 0; start < len(missing); start += 100 {
		batch := missing[start:min(start+100, len(missing))]
		pks, err := api.GetChatUsersPublicKeys(s.client, batch, s.opts)
		if err != nil {
			// Leave the batch unmarked so a later page or poll retries it;
			// a transient failure must not disable a sender for the session.
			fmt.Fprintf(os.Stderr, "warning: could not fetch public keys for %d user(s): %v\n", len(batch), err)
			continue
		}
		for _, id := range batch {
			s.seenSenders[id] = true
		}
		for _, pk := range pks {
			if pk.UserID == "" {
				continue
			}
			s.signingKeys = append(s.signingKeys, chatxdk.SigningKeyEntry{
				UserID:                     pk.UserID,
				PublicKeyVersion:           pk.Version,
				PublicKey:                  pk.SigningPublicKey,
				IdentityPublicKey:          pk.PublicKey,
				IdentityPublicKeySignature: pk.IdentityPublicKeySignature,
			})
			added = true
		}
	}
	if added {
		if err := s.chat.SetSigningKeys(s.signingKeys); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not store signing keys: %v\n", err)
		}
	}
}

// refreshSigningKeys loads signing keys for the senders on a page of events.
func (s *chatSession) refreshSigningKeys(events []api.ChatEventItem) {
	var ids []string
	for _, e := range events {
		ids = append(ids, e.SenderID)
	}
	s.loadSigningKeys(ids)
}

// ensureParticipantKeys loads signing keys for every participant of a
// conversation (once per session). Key-change and group events are signed by
// members who may not appear on the fetched pages, so their keys must be
// available before decrypting.
func (s *chatSession) ensureParticipantKeys(conversationID string) {
	if s.loadedConvs[conversationID] {
		return
	}
	s.loadedConvs[conversationID] = true
	ids, _, err := api.GetChatConversation(s.client, conversationID, s.opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not fetch conversation participants: %v\n", err)
		return
	}
	s.loadSigningKeys(ids)
}

// loadBacklog fetches one page of events, batch-decrypts it (filling the
// SDK's conversation-key cache), and returns the decrypt result together
// with the raw items and the next pagination token.
//
// The page's out-of-band key-change events (meta.conversation_key_events —
// key changes that apply to this page's messages but happened before it) are
// decrypted first so the conversation keys are always available, even when
// the key change itself is deep in the history.
func (s *chatSession) loadBacklog(conversationID string, maxResults int, paginationToken string) (*chatxdk.DecryptEventsResult, []api.ChatEventItem, string, error) {
	page, err := api.GetChatEvents(s.client, conversationID, maxResults, paginationToken, s.opts)
	if err != nil {
		return nil, nil, "", err
	}
	s.ensureParticipantKeys(conversationID)
	s.refreshSigningKeys(page.Events)
	s.adoptKeyEvents(page.KeyEvents)
	var eventsB64 []string
	for _, e := range page.Events {
		if e.EncodedEvent != "" {
			eventsB64 = append(eventsB64, e.EncodedEvent)
		}
	}
	result, err := s.chat.DecryptEvents(eventsB64, nil)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to decrypt events: %w", err)
	}
	s.retryWithExtractedKeys(result, eventsB64)
	return result, page.Events, page.NextToken, nil
}

// retryWithExtractedKeys retries batch-decrypt failures with the session's
// ECIES-extracted conversation keys (adoptKeyEvents' fallback for key
// changes whose signers are no longer resolvable). Successfully retried
// events move from result.Errors into result.Messages.
func (s *chatSession) retryWithExtractedKeys(result *chatxdk.DecryptEventsResult, eventsB64 []string) {
	if len(result.Errors) == 0 || len(s.convKeys) == 0 {
		return
	}
	for idxStr := range result.Errors {
		idx, err := strconv.Atoi(idxStr)
		if err != nil || idx < 0 || idx >= len(eventsB64) {
			continue
		}
		event, err := s.chat.DecryptEvent(eventsB64[idx], s.convKeys, nil)
		if err != nil {
			continue
		}
		result.Messages = append(result.Messages, chatxdk.DecryptedEventMessage{
			Event:       *event,
			OriginalB64: eventsB64[idx],
		})
		delete(result.Errors, idxStr)
	}
}

// adoptKeyEvents extracts conversation keys from key-change events. The
// verified batch-decrypt path runs first, filling the SDK's anti-downgrade
// key cache. Events it cannot verify — typically key changes signed by
// members who have since left, whose signing keys the API no longer lists —
// fall back to ECIES-only extraction into the session key map: only keys
// encrypted to this identity can decrypt at all, and message authorship is
// still verified per message, so the fallback cannot inject readable forged
// history.
func (s *chatSession) adoptKeyEvents(keyEvents []string) {
	if len(keyEvents) == 0 {
		return
	}
	if _, err := s.chat.DecryptEvents(keyEvents, nil); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not adopt conversation keys: %v\n", err)
	}
	bundle, err := s.chat.ExtractConversationKeys(keyEvents)
	if err != nil {
		return
	}
	for v, k := range bundle.Keys {
		s.convKeys[v] = k
	}
}

func (s *chatSession) readConversation(conversationID string, maxResults int, asJSON bool) error {
	result, events, _, err := s.loadBacklog(conversationID, maxResults, "")
	if err != nil {
		return err
	}

	// Print oldest-first for reading, ordering by event timestamp.
	messages := make([]*chatxdk.Event, 0, len(result.Messages))
	for i := range result.Messages {
		messages = append(messages, &result.Messages[i].Event)
	}
	sort.SliceStable(messages, func(i, j int) bool {
		return eventTimestamp(messages[i]) < eventTimestamp(messages[j])
	})

	if asJSON {
		var events []json.RawMessage
		for _, e := range messages {
			events = append(events, e.Raw())
		}
		utils.FormatAndPrintResponse(events)
	} else {
		if len(messages) == 0 {
			fmt.Println("No messages.")
		}
		for _, e := range messages {
			s.printEvent(e, false)
		}
	}

	// The batch decrypt keys its errors by index into the submitted slice;
	// map back through the same encoded-event filter loadBacklog applied so
	// the warning names the real event id, matching listen's wording.
	var decryptedIDs []string
	for _, e := range events {
		if e.EncodedEvent != "" {
			decryptedIDs = append(decryptedIDs, e.ID)
		}
	}
	for idxStr, msg := range result.Errors {
		label := "at index " + idxStr
		if idx, err := strconv.Atoi(idxStr); err == nil && idx >= 0 && idx < len(decryptedIDs) {
			label = decryptedIDs[idx]
		}
		fmt.Fprintf(os.Stderr, "warning: could not decrypt event %s: %s\n", label, msg)
	}
	return nil
}

func (s *chatSession) listen(conversationID string, interval time.Duration) error {
	if interval <= 0 {
		interval = 3 * time.Second
	}
	// Seed the key cache and the seen-event set from the backlog. Pagination
	// tokens walk BACKWARD through history (each next page is older), so a
	// poll loop must never follow them: every poll re-fetches the newest
	// page with no token and relies on the seen set to surface only events
	// that arrived since.
	result, backlog, _, err := s.loadBacklog(conversationID, 100, "")
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, e := range backlog {
		seen[e.ID] = true
	}

	fmt.Printf("Listening on %s (polling every %s, Ctrl-C to stop)...\n", conversationID, interval)
	// Show a little context: the last few messages, oldest first.
	context := make([]*chatxdk.Event, 0, len(result.Messages))
	for i := range result.Messages {
		context = append(context, &result.Messages[i].Event)
	}
	sort.SliceStable(context, func(i, j int) bool {
		return eventTimestamp(context[i]) < eventTimestamp(context[j])
	})
	if len(context) > 3 {
		context = context[len(context)-3:]
	}
	for _, e := range context {
		s.printEvent(e, false)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	for {
		select {
		case <-sig:
			fmt.Println()
			return nil
		case <-time.After(interval):
		}

		page, err := api.GetChatEvents(s.client, conversationID, 50, "", s.opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: poll failed: %v\n", err)
			continue
		}
		s.refreshSigningKeys(page.Events)
		s.adoptKeyEvents(page.KeyEvents)

		// The page is newest-first; collect the unseen events and flip them
		// so a batch of new messages prints in chronological order.
		var fresh []api.ChatEventItem
		for _, item := range page.Events {
			if item.EncodedEvent == "" || seen[item.ID] {
				continue
			}
			seen[item.ID] = true
			fresh = append(fresh, item)
		}
		for i := len(fresh) - 1; i >= 0; i-- {
			item := fresh[i]
			event, err := s.chat.DecryptEvent(item.EncodedEvent, nil, nil)
			if err != nil && len(s.convKeys) > 0 {
				// Retry with the ECIES-extracted keys (see adoptKeyEvents).
				event, err = s.chat.DecryptEvent(item.EncodedEvent, s.convKeys, nil)
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not decrypt event %s: %v\n", item.ID, err)
				continue
			}
			if event.AsKeyChange() != nil {
				// Only the batch path adopts keys into the SDK's cache;
				// replay the key change through it so a rotated key becomes
				// the sending key.
				if _, err := s.chat.DecryptEvents([]string{item.EncodedEvent}, nil); err != nil {
					fmt.Fprintf(os.Stderr, "warning: could not adopt rotated key: %v\n", err)
				}
			}
			s.printEvent(event, s.opts.Verbose)
		}
	}
}

// senderPalette holds the colors rotated through for other participants;
// the acting user is always green.
var senderPalette = []color.Attribute{
	color.FgCyan, color.FgMagenta, color.FgYellow, color.FgBlue, color.FgHiCyan, color.FgHiMagenta,
}

// senderStyle returns a stable color for a user id (green bold for the
// acting user), so each participant keeps one color for the whole run.
func (s *chatSession) senderStyle(userID string) *color.Color {
	if userID == s.userID {
		return color.New(color.FgGreen, color.Bold)
	}
	var h uint32 = 2166136261
	for i := 0; i < len(userID); i++ {
		h = (h ^ uint32(userID[i])) * 16777619
	}
	return color.New(senderPalette[h%uint32(len(senderPalette))], color.Bold)
}

// printDaySeparator prints a faint rule when the day changes between
// consecutively printed events, so timestamps can stay short.
func (s *chatSession) printDaySeparator(t time.Time) {
	day := t.Format("Mon 2006-01-02")
	if day == s.lastPrintedDay {
		return
	}
	s.lastPrintedDay = day
	color.New(color.Faint).Printf("── %s %s\n", day, strings.Repeat("─", 24))
}

// printEvent renders one decrypted event. Non-message events are only shown
// when verbose is true (key changes, typing, receipts, ...).
func (s *chatSession) printEvent(event *chatxdk.Event, verbose bool) {
	msg := event.AsMessage()
	if msg == nil {
		if verbose {
			color.New(color.Faint).Printf("       ─ %s event ─\n", event.Type)
		}
		return
	}

	sender := "unknown"
	senderID := ""
	if msg.SenderID != nil {
		senderID = *msg.SenderID
		sender = "@" + s.username(senderID)
	}
	ts := ""
	if msg.CreatedAtMsec != nil {
		t := time.UnixMilli(*msg.CreatedAtMsec).Local()
		s.printDaySeparator(t)
		ts = t.Format("15:04")
	}

	timeStyle := color.New(color.Faint)
	senderStyle := s.senderStyle(senderID)

	switch msg.Content.ContentType {
	case "Text":
		text := msg.Text()
		var suffix string
		if n := max(len(msg.MediaHashes), len(msg.Attachments)); n > 0 {
			count := ""
			if n > 1 {
				count = fmt.Sprintf(" ×%d", n)
			}
			if text == "" {
				text = color.New(color.Faint).Sprintf("📎 attachment%s", count)
			} else {
				suffix = color.New(color.Faint).Sprintf("  📎%s", count)
			}
		}
		if text == "" {
			text = color.New(color.Faint).Sprint("(empty message)")
		}
		if !msg.Verified {
			suffix += color.RedString("  [unverified]")
		}
		timeStyle.Printf("%s  ", ts)
		senderStyle.Print(sender)
		fmt.Printf("  %s%s\n", text, suffix)
	case "Reaction":
		if msg.Content.ReactionContent != nil {
			timeStyle.Printf("%s  ", ts)
			color.New(color.Faint).Printf("%s reacted %s\n", sender, msg.Content.ReactionContent.Emoji)
		}
	case "ReactionRemoved":
		if msg.Content.ReactionContent != nil && verbose {
			timeStyle.Printf("%s  ", ts)
			color.New(color.Faint).Printf("%s removed reaction %s\n", sender, msg.Content.ReactionContent.Emoji)
		}
	default:
		if verbose {
			color.New(color.Faint).Printf("%s  %s (%s message)\n", ts, sender, msg.Content.ContentType)
		}
	}
}

// -----------------------------------------------------------------
// Sending
// -----------------------------------------------------------------

func (s *chatSession) sendMessage(conversationID, text string) error {
	// Load the backlog first: it fills the SDK's conversation-key cache with
	// the current key, and tells us whether the conversation exists at all.
	result, events, _, err := s.loadBacklog(conversationID, 100, "")
	if err != nil && !isNotFoundError(err) {
		// Only a missing conversation may proceed to key initialization; any
		// other failure must not trigger a blind key rotation.
		return err
	}

	// The message signature covers the conversation id in its canonical
	// (colon) form; prefer the id carried inside a decrypted event, then the
	// one on the raw event items, then the caller-derived form.
	signConvID := api.ChatConversationEventID(conversationID)
	for _, e := range events {
		if e.ConversationID != "" {
			signConvID = e.ConversationID
			break
		}
	}
	if result != nil {
		for _, m := range result.Messages {
			if msg := m.Event.AsMessage(); msg != nil && msg.ConversationID != nil && *msg.ConversationID != "" {
				signConvID = *msg.ConversationID
				break
			}
			if kc := m.Event.AsKeyChange(); kc != nil && kc.ConversationID != nil && *kc.ConversationID != "" {
				signConvID = *kc.ConversationID
				break
			}
		}
	}

	// Encrypt with the first key source that works: the SDK's verified key
	// cache (filled by the backlog decrypt), then the ECIES-extracted
	// session keys (see adoptKeyEvents), and only then — for a 1:1 with no
	// key at all — a freshly initialized conversation key.
	params := chatxdk.EncryptMessageParams{
		ConversationID: signConvID,
		Text:           text,
	}
	payload, encErr := s.chat.EncryptMessage(params)
	if encErr != nil {
		if v := latestKeyVersion(s.convKeys); v != "" {
			params.ConversationKey = s.convKeys[v]
			params.ConversationKeyVersion = v
			payload, encErr = s.chat.EncryptMessage(params)
		}
	}
	if encErr != nil {
		if strings.HasPrefix(conversationID, "g") {
			return fmt.Errorf("no usable conversation key for group %s (were this device's keys registered after the last key rotation?): %w", conversationID, encErr)
		}
		// A 1:1 with history but no usable key means rotating would be
		// visible to the other participant's clients — never do it as a
		// silent side effect of "send".
		if len(events) > 0 {
			fmt.Fprintf(os.Stderr, "This conversation exists but none of its keys are readable by this device (%v).\n", encErr)
			fmt.Fprintln(os.Stderr, "Sending will rotate the conversation key: earlier messages stay unreadable here, and the other participant's clients will see a key change.")
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return fmt.Errorf("refusing to rotate the conversation key non-interactively — run again from a terminal to confirm")
			}
			fmt.Fprint(os.Stderr, "Rotate and send? [y/N] ")
			var answer string
			_, _ = fmt.Scanln(&answer)
			if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
				return fmt.Errorf("send aborted")
			}
		}
		// Fresh (or confirmed key-less) 1:1 conversation: generate and
		// distribute a conversation key to every participant.
		prepared, err := s.prepareOneToOneKey(conversationID)
		if err != nil {
			return err
		}
		signConvID = prepared.ConversationID
		params.ConversationID = signConvID
		params.ConversationKey = prepared.ConversationKey
		params.ConversationKeyVersion = prepared.ConversationKeyVersion
		payload, encErr = s.chat.EncryptMessage(params)
	}
	if encErr != nil {
		return fmt.Errorf("failed to encrypt message: %w", encErr)
	}

	resp, err := api.SendChatMessage(s.client, signConvID, api.ChatSendBody{
		MessageID:                    payload.MessageID,
		EncodedMessageCreateEvent:    payload.EncryptedContent,
		EncodedMessageEventSignature: payload.EncodedEventSignature,
	}, s.opts)
	if err != nil {
		return err
	}

	fmt.Printf("%s %s\n", color.GreenString("✓ sent to"), api.ChatConversationPathID(signConvID))
	color.New(color.Faint).Printf("  message id %s\n", payload.MessageID)
	if s.opts.Verbose {
		utils.FormatAndPrintResponse(resp)
	}
	return nil
}

// prepareOneToOneKey generates a conversation key for a 1:1 conversation,
// ECIES-encrypts it to both participants, and registers it with the API.
func (s *chatSession) prepareOneToOneKey(conversationID string) (*chatxdk.PreparedConversationChange, error) {
	parts := strings.Split(conversationID, "-")
	if len(parts) != 2 {
		return nil, fmt.Errorf("cannot initialize keys for conversation %s", conversationID)
	}

	var inputs []chatxdk.PublicKeyInput
	for _, uid := range parts {
		pks, err := api.GetChatPublicKeys(s.client, uid, s.opts)
		if err != nil {
			return nil, err
		}
		if len(pks) == 0 {
			who := "user " + uid
			if uid != s.userID {
				who = "@" + s.username(uid)
			}
			return nil, fmt.Errorf("%s has no registered XChat keys — they need to enable encrypted chat first", who)
		}
		// Encrypt to the newest key version of each participant.
		latest := pks[0]
		for _, pk := range pks[1:] {
			if api.CompareChatKeyVersions(pk.Version, latest.Version) > 0 {
				latest = pk
			}
		}
		inputs = append(inputs, chatxdk.PublicKeyInput{
			UserID:     uid,
			PublicKey:  latest.PublicKey,
			KeyVersion: latest.Version,
		})
	}

	// An empty conversation id derives the canonical 1:1 id from the
	// participants.
	prepared, err := s.chat.PrepareConversationKeyChange(chatxdk.ConversationKeyChangeParams{
		PublicKeys: inputs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to prepare conversation key: %w", err)
	}

	pub, err := s.chat.GetPublicKeys()
	if err != nil {
		return nil, err
	}
	body := preparedChangeToRequest(prepared, pub.Signing)
	if _, err := api.AddChatConversationKeys(s.client, prepared.ConversationID, body, s.opts); err != nil {
		return nil, fmt.Errorf("failed to register conversation key: %w", err)
	}
	return prepared, nil
}

// preparedChangeToRequest maps a prepared conversation change into the
// /2/chat/conversations/:id/keys request shape.
func preparedChangeToRequest(prep *chatxdk.PreparedConversationChange, signingPublicKey string) map[string]any {
	participantKeys := make([]map[string]any, 0, len(prep.ParticipantKeys))
	for _, pk := range prep.ParticipantKeys {
		participantKeys = append(participantKeys, map[string]any{
			"user_id":                    pk.UserID,
			"encrypted_conversation_key": pk.EncryptedKey,
			"public_key_version":         pk.PublicKeyVersion,
		})
	}
	actionSignatures := make([]map[string]any, 0, len(prep.ActionSignatures))
	for _, sig := range prep.ActionSignatures {
		entry := map[string]any{
			"message_id":                   sig.MessageID,
			"encoded_message_event_detail": sig.EncodedMessageEventDetail,
			"message_event_signature": map[string]any{
				"signature":          sig.Signature,
				"signature_version":  sig.SignatureVersion,
				"public_key_version": sig.PublicKeyVersion,
				"signing_public_key": signingPublicKey,
			},
		}
		if sig.SignaturePayload != "" {
			entry["signature_payload"] = sig.SignaturePayload
		}
		actionSignatures = append(actionSignatures, entry)
	}
	return map[string]any{
		"conversation_key_version":      prep.ConversationKeyVersion,
		"conversation_participant_keys": participantKeys,
		"action_signatures":             actionSignatures,
	}
}

// -----------------------------------------------------------------
// Small helpers
// -----------------------------------------------------------------

// username resolves a user id to a username, caching per session; falls back
// to the id itself.
func (s *chatSession) username(userID string) string {
	if u, ok := s.usernames[userID]; ok {
		return u
	}
	name := userID
	if resp, err := api.GetUserByID(s.client, userID, s.opts); err == nil {
		var out struct {
			Data struct {
				Username string `json:"username"`
			} `json:"data"`
		}
		if json.Unmarshal(resp, &out) == nil && out.Data.Username != "" {
			name = out.Data.Username
		}
	}
	s.usernames[userID] = name
	return name
}

// promptSecret reads a line of sensitive input: without echo on a terminal,
// as a plain line read when stdin is piped.
func promptSecret(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	defer fmt.Fprintln(os.Stderr)
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("could not read input: %w", err)
		}
		return strings.TrimSpace(line), nil
	}
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return "", fmt.Errorf("could not read input: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

// exitOnError prints an error to stderr and exits non-zero.
func exitOnError(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
		os.Exit(1)
	}
}
