//go:build cgo && ((darwin && (amd64 || arm64)) || (linux && amd64))

package cli

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
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
		chatRotateCmd(a),
		chatDownloadCmd(a),
		chatMembersCmd(a),
		chatMarkReadCmd(a),
		chatTypingCmd(a),
	)
	return chatCmd
}

func chatDownloadCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "download CONVERSATION|@USERNAME MEDIA_HASH_KEY",
		Short: "Download and decrypt a chat media attachment",
		Long: `Downloads an encrypted media attachment and decrypts it to a local
file. Find the media hash key with 'chat read --json' (attachments carry
a media_hash_key). The attachment is decrypted with the conversation key
version that was active when the message was sent.`,
		Args: cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			out, _ := cmd.Flags().GetString("output")
			s, err := newChatSession(a, cmd, true)
			exitOnError(err)
			defer s.Close()
			convID, err := s.resolveConversation(args[0])
			exitOnError(err)
			exitOnError(s.downloadMedia(convID, args[1], out))
		},
	}
	cmd.Flags().StringP("output", "o", "", "Output file path (default: the media hash key)")
	addCommonFlags(cmd)
	return cmd
}

func chatMembersCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-members GROUP @USER [@USER...]",
		Short: "Add members to a group conversation",
		Long: `Adds one or more members to an existing group, rotating the
conversation key so the new members can read messages from now on. Only a
current member can add members. New members do not gain access to messages
sent before they were added.`,
		Args: cobra.MinimumNArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			yes, _ := cmd.Flags().GetBool("yes")
			s, err := newChatSession(a, cmd, true)
			exitOnError(err)
			defer s.Close()
			convID, err := s.resolveConversation(args[0])
			exitOnError(err)
			if !strings.HasPrefix(convID, "g") {
				exitOnError(fmt.Errorf("add-members only applies to group conversations (got %s)", convID))
			}
			exitOnError(s.addGroupMembers(convID, args[1:], yes))
		},
	}
	cmd.Flags().BoolP("yes", "y", false, "Skip the confirmation prompt")
	addCommonFlags(cmd)
	return cmd
}

func chatMarkReadCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mark-read CONVERSATION|@USERNAME",
		Short: "Mark a conversation read up to its latest message",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			s, err := newChatSession(a, cmd, false)
			exitOnError(err)
			defer s.Close()
			convID, err := s.resolveConversation(args[0])
			exitOnError(err)
			exitOnError(s.markReadCommand(convID))
		},
	}
	addCommonFlags(cmd)
	return cmd
}

func chatTypingCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "typing CONVERSATION|@USERNAME",
		Short: "Send a typing indicator to a conversation",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			s, err := newChatSession(a, cmd, false)
			exitOnError(err)
			defer s.Close()
			convID, err := s.resolveConversation(args[0])
			exitOnError(err)
			_, err = api.SendChatTyping(s.client, convID, s.opts)
			exitOnError(err)
			color.Green("✓ typing indicator sent")
		},
	}
	addCommonFlags(cmd)
	return cmd
}

func chatRotateCmd(a *auth.Auth) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rotate CONVERSATION|@USERNAME",
		Short: "Rotate a conversation's encryption key",
		Long: `Generates a fresh conversation key and distributes it to every current
participant's newest registered keys.

Rotate when a conversation key may have been exposed, or to grant a member
access going forward when their keys were registered after the last rotation
(they gain access to new messages only). Rotation protects future messages:
anyone holding an earlier key version can still read the messages encrypted
under it, and members without the old versions still cannot read old history.`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			yes, _ := cmd.Flags().GetBool("yes")
			s, err := newChatSession(a, cmd, true)
			exitOnError(err)
			defer s.Close()
			convID, err := s.resolveConversation(args[0])
			exitOnError(err)
			exitOnError(s.rotateConversationKey(convID, yes))
		},
	}
	cmd.Flags().BoolP("yes", "y", false, "Skip the confirmation prompt")
	addCommonFlags(cmd)
	return cmd
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
			noMarkRead, _ := cmd.Flags().GetBool("no-mark-read")
			s, err := newChatSession(a, cmd, true)
			exitOnError(err)
			defer s.Close()
			convID, err := s.resolveConversation(args[0])
			exitOnError(err)
			exitOnError(s.readConversation(convID, maxResults, asJSON, !noMarkRead))
		},
	}
	cmd.Flags().IntP("max-results", "n", 50, "Maximum number of events to fetch (1-100)")
	cmd.Flags().Bool("json", false, "Output decrypted events as JSON")
	cmd.Flags().Bool("no-mark-read", false, "Do not mark the conversation read")
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
		Args: cobra.RangeArgs(1, 2),
		Run: func(cmd *cobra.Command, args []string) {
			file, _ := cmd.Flags().GetString("file")
			replyTo, _ := cmd.Flags().GetString("reply-to")
			noMarkRead, _ := cmd.Flags().GetBool("no-mark-read")
			noTyping, _ := cmd.Flags().GetBool("no-typing")
			text := ""
			if len(args) == 2 {
				text = args[1]
			}
			if text == "" && file == "" {
				exitOnError(fmt.Errorf("provide message text, --file, or both"))
			}
			s, err := newChatSession(a, cmd, true)
			exitOnError(err)
			defer s.Close()
			convID, err := s.resolveConversation(args[0])
			exitOnError(err)
			exitOnError(s.sendMessage(convID, text, sendOptions{filePath: file, replyToID: replyTo, markRead: !noMarkRead, typing: !noTyping}))
		},
	}
	cmd.Flags().StringP("file", "F", "", "Attach an encrypted media file")
	cmd.Flags().String("reply-to", "", "Sequence id of the event to reply to")
	cmd.Flags().Bool("no-mark-read", false, "Do not mark the conversation read after sending")
	cmd.Flags().Bool("no-typing", false, "Do not send a typing indicator before sending")
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
			noMarkRead, _ := cmd.Flags().GetBool("no-mark-read")
			s, err := newChatSession(a, cmd, true)
			exitOnError(err)
			defer s.Close()
			convID, err := s.resolveConversation(args[0])
			exitOnError(err)
			exitOnError(s.listen(convID, time.Duration(interval)*time.Second, !noMarkRead))
		},
	}
	cmd.Flags().Int("interval", 3, "Polling interval in seconds")
	cmd.Flags().Bool("no-mark-read", false, "Do not mark new messages read as they arrive")
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
	meta, _, err := api.GetChatConversation(s.client, conversationID, s.opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not fetch conversation participants: %v\n", err)
		return
	}
	s.loadSigningKeys(meta.AllUserIDs())
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

func (s *chatSession) readConversation(conversationID string, maxResults int, asJSON, markRead bool) error {
	result, events, _, err := s.loadBacklog(conversationID, maxResults, "")
	if err != nil {
		return err
	}
	// Reading a conversation means you have seen it: mark it read up to the
	// newest event (a watermark — this also marks every earlier message
	// read). Best-effort; opt out with --no-mark-read.
	if markRead {
		s.markReadLatest(conversationID, events)
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
			s.printEvent(e, s.opts.Verbose)
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

func (s *chatSession) listen(conversationID string, interval time.Duration, markRead bool) error {
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
	// Opening the conversation marks the existing backlog read.
	if markRead {
		s.markReadLatest(conversationID, backlog)
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
		// New arrivals shown means they have been seen: advance the read
		// watermark to the newest of this batch.
		if markRead && len(fresh) > 0 {
			s.markReadLatest(conversationID, fresh)
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
		var prefix, suffix string
		if tc := msg.Content.TextContent; tc != nil && len(tc.ReplyingToPreview) > 0 {
			prefix = color.New(color.Faint).Sprint("↩ ")
		}
		if n := max(len(msg.MediaHashes), len(msg.Attachments)); n > 0 {
			count := ""
			if n > 1 {
				count = fmt.Sprintf(" ×%d", n)
			}
			marker := "📎 attachment" + count
			// Show the hash key so it can be passed to 'chat download'.
			if len(msg.MediaHashes) > 0 {
				marker += " " + msg.MediaHashes[0].MediaHashKey
			}
			if text == "" {
				text = color.New(color.Faint).Sprint(marker)
			} else {
				suffix = color.New(color.Faint).Sprintf("  %s", marker)
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
		fmt.Printf("  %s%s%s\n", prefix, text, suffix)
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

// sendOptions carries the optional extras for a send.
type sendOptions struct {
	filePath  string // attach an encrypted media file
	replyToID string // reply to the event with this sequence id
	markRead  bool   // mark the conversation read after sending
	typing    bool   // send a typing indicator before sending
}

func (s *chatSession) sendMessage(conversationID, text string, sopts sendOptions) error {
	// A typing indicator before the message mirrors how a person composes;
	// best-effort so it never blocks the send. Opt out with --no-typing.
	if sopts.typing {
		if _, err := api.SendChatTyping(s.client, conversationID, s.opts); err != nil && s.opts.Verbose {
			fmt.Fprintf(os.Stderr, "warning: could not send typing indicator: %v\n", err)
		}
	}

	// Load the backlog: it extracts the conversation key and tells us whether
	// the conversation exists.
	result, events, _, err := s.loadBacklog(conversationID, 100, "")
	if err != nil && !isNotFoundError(err) {
		// Only a missing conversation may proceed to key initialization; any
		// other failure must not trigger a blind key rotation.
		return err
	}

	// The message signature covers the conversation id in its canonical
	// (colon) form; prefer the id carried inside a decrypted event, then the
	// raw event items, then the caller-derived form.
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

	// Resolve the raw conversation key (needed to encrypt media under the
	// same key as the message), initializing or rotating only with consent.
	rawKey, keyVer, newSignConvID, err := s.resolveSendKey(conversationID, signConvID, result, len(events) > 0)
	if err != nil {
		return err
	}
	signConvID = newSignConvID

	params := chatxdk.EncryptMessageParams{
		ConversationID:         signConvID,
		Text:                   text,
		ConversationKey:        rawKey,
		ConversationKeyVersion: keyVer,
	}

	// Attach an encrypted media file, if requested.
	if sopts.filePath != "" {
		att, err := s.uploadMedia(signConvID, sopts.filePath, rawKey)
		if err != nil {
			return err
		}
		params.Attachments = []chatxdk.AttachmentDescriptor{*att}
	}

	var payload *chatxdk.SendPayload
	if sopts.replyToID != "" {
		rawEvent := findEncodedEvent(events, sopts.replyToID)
		if rawEvent == "" {
			return fmt.Errorf("could not find event %s in the recent history to reply to", sopts.replyToID)
		}
		payload, err = s.chat.EncryptReply(chatxdk.EncryptReplyParams{
			ConversationID:         signConvID,
			Text:                   text,
			ReplyToEvent:           rawEvent,
			ConversationKey:        rawKey,
			ConversationKeyVersion: keyVer,
			Attachments:            params.Attachments,
		})
	} else {
		payload, err = s.chat.EncryptMessage(params)
	}
	if err != nil {
		return fmt.Errorf("failed to encrypt message: %w", err)
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
	if sopts.markRead {
		s.markReadLatest(signConvID, events)
	}
	if s.opts.Verbose {
		utils.FormatAndPrintResponse(resp)
	}
	return nil
}

// resolveSendKey returns the raw conversation key and version to encrypt a
// message and any media under, plus the canonical conversation id. It uses
// the newest key already readable on the conversation; failing that, it
// initializes a fresh key for a new 1:1, or — for an existing 1:1 whose key
// this device cannot read — rotates only after explicit confirmation. A
// group with no readable key is an error (join/rotation is a separate step).
func (s *chatSession) resolveSendKey(conversationID, signConvID string, result *chatxdk.DecryptEventsResult, hasHistory bool) (rawKey []byte, keyVer, outConvID string, err error) {
	keys := map[string][]byte{}
	if result != nil {
		for v, k := range result.ConversationKeys.Keys {
			keys[v] = k
		}
	}
	for v, k := range s.convKeys {
		keys[v] = k
	}
	if v := latestKeyVersion(keys); v != "" {
		return keys[v], v, signConvID, nil
	}

	if strings.HasPrefix(conversationID, "g") {
		return nil, "", "", fmt.Errorf("no usable conversation key for group %s — its key was not encrypted to this device (try 'xurl chat rotate %s' from a current member, or ask to be re-added)", conversationID, conversationID)
	}
	if hasHistory {
		fmt.Fprintf(os.Stderr, "This conversation exists but none of its keys are readable by this device.\n")
		fmt.Fprintln(os.Stderr, "Sending will rotate the conversation key: earlier messages stay unreadable here, and the other participant's clients will see a key change.")
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return nil, "", "", fmt.Errorf("refusing to rotate the conversation key non-interactively — run again from a terminal to confirm")
		}
		fmt.Fprint(os.Stderr, "Rotate and send? [y/N] ")
		var answer string
		_, _ = fmt.Scanln(&answer)
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			return nil, "", "", fmt.Errorf("send aborted")
		}
	}
	prepared, confirmedID, err := s.prepareOneToOneKey(conversationID)
	if err != nil {
		return nil, "", "", err
	}
	return prepared.ConversationKey, prepared.ConversationKeyVersion, api.ChatConversationEventID(confirmedID), nil
}

// findEncodedEvent returns the raw base64 event whose id matches, or "".
func findEncodedEvent(events []api.ChatEventItem, id string) string {
	for _, e := range events {
		if e.ID == id {
			return e.EncodedEvent
		}
	}
	return ""
}

// markReadLatest marks the conversation read up to the newest event's
// sequence id (best-effort; failures warn but don't fail the send).
func (s *chatSession) markReadLatest(conversationID string, events []api.ChatEventItem) {
	newest := ""
	for _, e := range events {
		if api.CompareChatKeyVersions(e.ID, newest) > 0 {
			newest = e.ID
		}
	}
	if newest == "" {
		return
	}
	if _, err := api.MarkChatRead(s.client, conversationID, newest, s.opts); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not mark read: %v\n", err)
	}
}

// downloadMedia fetches and decrypts a media attachment to a local file,
// trying every conversation key this device holds (the attachment was
// encrypted under the key version active when its message was sent).
func (s *chatSession) downloadMedia(conversationID, mediaHashKey, outPath string) error {
	// Load the backlog so the conversation keys are available.
	result, _, _, err := s.loadBacklog(conversationID, 100, "")
	if err != nil && !isNotFoundError(err) {
		return err
	}
	keys := map[string][]byte{}
	if result != nil {
		for v, k := range result.ConversationKeys.Keys {
			keys[v] = k
		}
	}
	for v, k := range s.convKeys {
		keys[v] = k
	}
	if len(keys) == 0 {
		return fmt.Errorf("no conversation key available to decrypt media in %s", conversationID)
	}

	ciphertext, err := api.DownloadChatMedia(s.client, conversationID, mediaHashKey, s.opts)
	if err != nil {
		return err
	}

	// Try newest key first; the right version decrypts, others fail cleanly.
	versions := make([]string, 0, len(keys))
	for v := range keys {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool {
		return api.CompareChatKeyVersions(versions[i], versions[j]) > 0
	})
	var plaintext []byte
	for _, v := range versions {
		if pt, derr := s.chat.DecryptStream(ciphertext, keys[v]); derr == nil {
			plaintext = pt
			break
		}
	}
	if plaintext == nil {
		return fmt.Errorf("could not decrypt media with any available conversation key (%d tried)", len(versions))
	}

	if outPath == "" {
		outPath = mediaHashKey
	}
	if err := os.WriteFile(outPath, plaintext, 0600); err != nil {
		return err
	}
	fmt.Printf("%s %s (%d bytes)\n", color.GreenString("✓ saved"), outPath, len(plaintext))
	return nil
}

// markReadCommand marks a conversation read up to its newest event.
func (s *chatSession) markReadCommand(conversationID string) error {
	page, err := api.GetChatEvents(s.client, conversationID, 1, "", s.opts)
	if err != nil {
		return err
	}
	if len(page.Events) == 0 {
		fmt.Println("Nothing to mark read.")
		return nil
	}
	newest := ""
	for _, e := range page.Events {
		if api.CompareChatKeyVersions(e.ID, newest) > 0 {
			newest = e.ID
		}
	}
	if _, err := api.MarkChatRead(s.client, conversationID, newest, s.opts); err != nil {
		return err
	}
	fmt.Printf("%s %s (through sequence %s)\n", color.GreenString("✓ marked read"), api.ChatConversationPathID(conversationID), newest)
	return nil
}

// addGroupMembers adds members to a group, rotating the conversation key so
// the new roster can read messages going forward.
func (s *chatSession) addGroupMembers(conversationID string, newMembers []string, skipConfirm bool) error {
	// Resolve the new members to user ids.
	var newIDs []string
	for _, m := range newMembers {
		id, err := s.resolveUserToID(m)
		if err != nil {
			return err
		}
		newIDs = append(newIDs, id)
	}

	meta, _, err := api.GetChatConversation(s.client, conversationID, s.opts)
	if err != nil {
		return fmt.Errorf("could not fetch the group: %w", err)
	}
	current := meta.AllUserIDs()

	if !skipConfirm {
		var names []string
		for _, id := range newIDs {
			names = append(names, "@"+s.username(id))
		}
		fmt.Fprintf(os.Stderr, "Add %s to %s? This rotates the conversation key (visible to all members); new members read messages from now on only.\n", strings.Join(names, ", "), conversationID)
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return fmt.Errorf("refusing to add members non-interactively — pass --yes to confirm")
		}
		fmt.Fprint(os.Stderr, "Proceed? [y/N] ")
		var answer string
		_, _ = fmt.Scanln(&answer)
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			return fmt.Errorf("add-members aborted")
		}
	}

	// The rotated key is wrapped to the full new roster (current + new).
	roster := append(append([]string{}, current...), newIDs...)
	inputs, err := s.latestKeyInputs(roster)
	if err != nil {
		return err
	}
	prepared, err := s.chat.PrepareGroupMembersChange(chatxdk.GroupMembersChangeParams{
		PublicKeys:       inputs,
		ConversationID:   api.ChatConversationEventID(conversationID),
		NewMemberIDs:     newIDs,
		CurrentMemberIDs: meta.MemberIDs,
		CurrentAdminIDs:  meta.AdminIDs,
		CurrentTitle:     meta.GroupName,
		CurrentAvatarURL: meta.GroupAvatarURL,
	})
	if err != nil {
		return fmt.Errorf("failed to prepare member change: %w", err)
	}

	pub, err := s.chat.GetPublicKeys()
	if err != nil {
		return err
	}
	body := preparedChangeToRequest(prepared, pub.Signing)
	body["user_ids"] = newIDs
	if _, err := api.AddChatGroupMembers(s.client, conversationID, body, s.opts); err != nil {
		return err
	}
	fmt.Printf("%s %d member(s) to %s\n", color.GreenString("✓ added"), len(newIDs), api.ChatConversationPathID(conversationID))
	color.New(color.Faint).Printf("  new key version %s\n", prepared.ConversationKeyVersion)
	return nil
}

// resolveUserToID resolves @username / username / bare id to a user id.
func (s *chatSession) resolveUserToID(input string) (string, error) {
	input = strings.TrimSpace(input)
	if isAllDigits(input) {
		return input, nil
	}
	return resolveUserID(s.client, input, s.opts)
}

// mediaTypeForMime maps a detected MIME type to the wire MediaType code.
func mediaTypeForMime(mime string) int32 {
	switch {
	case mime == "image/gif":
		return 2 // GIF
	case mime == "image/svg+xml":
		return 6 // SVG
	case strings.HasPrefix(mime, "image/"):
		return 1 // IMAGE
	case strings.HasPrefix(mime, "video/"):
		return 3 // VIDEO
	case strings.HasPrefix(mime, "audio/"):
		return 4 // AUDIO
	default:
		return 5 // FILE
	}
}

// uploadMedia encrypts a local file under the conversation key, uploads the
// ciphertext, and returns an attachment descriptor referencing it.
func (s *chatSession) uploadMedia(conversationID, path string, conversationKey []byte) (*chatxdk.AttachmentDescriptor, error) {
	plaintext, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", path, err)
	}
	ciphertext, err := s.chat.EncryptStream(plaintext, conversationKey)
	if err != nil {
		return nil, fmt.Errorf("could not encrypt media: %w", err)
	}
	sessionID, mediaHashKey, err := api.InitializeChatMediaUpload(s.client, conversationID, len(ciphertext), s.opts)
	if err != nil {
		return nil, err
	}
	if err := api.UploadChatMedia(s.client, sessionID, conversationID, mediaHashKey, ciphertext, s.opts); err != nil {
		return nil, err
	}

	mime, _ := chatxdk.DetectMimeType(plaintext)
	mediaType := mediaTypeForMime(mime)
	att := &chatxdk.AttachmentDescriptor{
		AttachmentType: "media",
		MediaHashKey:   mediaHashKey,
		FilesizeBytes:  int64(len(plaintext)),
		Filename:       filepath.Base(path),
		MediaType:      &mediaType,
	}
	if dim, err := chatxdk.DetectImageDimensions(plaintext); err == nil && dim != nil {
		att.Width = int64(dim.Width)
		att.Height = int64(dim.Height)
	}
	color.New(color.Faint).Fprintf(os.Stderr, "  uploaded %s (%s, %d bytes)\n", att.Filename, mime, len(plaintext))
	return att, nil
}

// prepareOneToOneKey generates a conversation key for a 1:1 conversation,
// ECIES-encrypts it to both participants, and registers it with the API.
func (s *chatSession) prepareOneToOneKey(conversationID string) (*chatxdk.PreparedConversationChange, string, error) {
	parts := strings.Split(conversationID, "-")
	if len(parts) != 2 {
		return nil, "", fmt.Errorf("cannot initialize keys for conversation %s", conversationID)
	}
	// An empty conversation id has the SDK derive the canonical 1:1 id from
	// the participants.
	return s.establishConversationKey(parts, "")
}

// establishConversationKey generates a conversation key, encrypts it to each
// participant's newest registered identity key, and POSTs it to the API. An
// empty conversationID creates a fresh 1:1 key; a set id rotates the existing
// conversation's key. It returns the prepared change and the canonical
// conversation id confirmed by the server (which callers must prefer over a
// client-reconstructed id).
func (s *chatSession) establishConversationKey(participantIDs []string, conversationID string) (*chatxdk.PreparedConversationChange, string, error) {
	inputs, err := s.latestKeyInputs(participantIDs)
	if err != nil {
		return nil, "", err
	}

	prepared, err := s.chat.PrepareConversationKeyChange(chatxdk.ConversationKeyChangeParams{
		PublicKeys:     inputs,
		ConversationID: conversationID,
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed to prepare conversation key: %w", err)
	}

	pub, err := s.chat.GetPublicKeys()
	if err != nil {
		return nil, "", err
	}
	body := preparedChangeToRequest(prepared, pub.Signing)
	resp, err := api.AddChatConversationKeys(s.client, prepared.ConversationID, body, s.opts)
	if err != nil {
		return nil, "", fmt.Errorf("failed to register conversation key: %w", err)
	}

	confirmedID := prepared.ConversationID
	var out struct {
		Data struct {
			ConversationID string `json:"conversation_id"`
		} `json:"data"`
	}
	if json.Unmarshal(resp, &out) == nil && out.Data.ConversationID != "" {
		confirmedID = out.Data.ConversationID
	}
	return prepared, confirmedID, nil
}

// rotateConversationKey generates a fresh conversation key for an existing
// conversation and wraps it to every current participant's newest keys.
func (s *chatSession) rotateConversationKey(conversationID string, skipConfirm bool) error {
	// The roster comes from the conversation itself: metadata participants
	// for a group, the two ids from the canonical id for a 1:1.
	var roster []string
	if strings.HasPrefix(conversationID, "g") {
		meta, _, err := api.GetChatConversation(s.client, conversationID, s.opts)
		if err != nil {
			return fmt.Errorf("could not fetch the group roster: %w", err)
		}
		roster = meta.AllUserIDs()
	} else {
		roster = strings.Split(conversationID, "-")
	}
	if len(roster) < 2 {
		return fmt.Errorf("conversation %s has no roster to rotate a key for", conversationID)
	}

	if !skipConfirm {
		fmt.Fprintf(os.Stderr, "Rotating the key of %s re-encrypts future messages to %d participant(s)' newest keys.\n", conversationID, len(roster))
		fmt.Fprintln(os.Stderr, "Other participants' clients will see a key change. Old messages stay readable only to holders of the old key versions.")
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return fmt.Errorf("refusing to rotate non-interactively — pass --yes to confirm")
		}
		fmt.Fprint(os.Stderr, "Rotate? [y/N] ")
		var answer string
		_, _ = fmt.Scanln(&answer)
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			return fmt.Errorf("rotation aborted")
		}
	}

	prepared, confirmedID, err := s.establishConversationKey(roster, api.ChatConversationEventID(conversationID))
	if err != nil {
		return err
	}
	fmt.Printf("%s %s\n", color.GreenString("✓ rotated key of"), api.ChatConversationPathID(confirmedID))
	color.New(color.Faint).Printf("  new key version %s, wrapped to %d participant(s)\n", prepared.ConversationKeyVersion, len(prepared.ParticipantKeys))
	return nil
}

// latestKeyInputs fetches each participant's registered public keys and
// selects the newest version to encrypt the conversation key to.
func (s *chatSession) latestKeyInputs(userIDs []string) ([]chatxdk.PublicKeyInput, error) {
	var inputs []chatxdk.PublicKeyInput
	for _, uid := range userIDs {
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
	return inputs, nil
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
