package cli

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/xdevplatform/xurl/auth"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/tidwall/pretty"
	"golang.ngrok.com/ngrok"
	"golang.ngrok.com/ngrok/config"
)

var webhookPort int
var outputFileName string // To store the output file name from the flag
var quietMode bool        // To store the quiet flag state
var prettyMode bool       // To store the pretty-print flag state

// CreateWebhookCommand creates the webhook command and its subcommands.
func CreateWebhookCommand(authInstance *auth.Auth) *cobra.Command {
	webhookCmd := &cobra.Command{
		Use:   "webhook",
		Short: "Manage webhooks for the X API",
		Long:  `Manages X API webhooks. Currently supports starting a local server with an ngrok tunnel to handle CRC checks.`,
	}

	webhookStartCmd := &cobra.Command{
		Use:   "start",
		Short: "Start a local webhook server with an ngrok tunnel",
		Long:  `Starts a local HTTP server and an ngrok tunnel to listen for X API webhook events, including CRC checks. CRC checks use the selected app's OAuth 2.0 client secret, or its OAuth 1.0a consumer secret for legacy integrations. POST request bodies can be saved to a file using the -o flag. Use -q for quieter console logging of POST events. Use -P to pretty-print JSON POST bodies in the console.`,
		Run: func(cmd *cobra.Command, args []string) {
			color.Cyan("Starting webhook server with ngrok...")

			signingSecret, err := webhookSigningSecret(authInstance)
			if err != nil {
				color.Red("Error: %v", err)
				os.Exit(1)
			}

			// Handle output file if -o flag is used
			var outputFile *os.File
			var errOpenFile error
			if outputFileName != "" {
				outputFile, errOpenFile = os.OpenFile(outputFileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				if errOpenFile != nil {
					color.Red("Error opening output file %s: %v", outputFileName, errOpenFile)
					os.Exit(1)
				}
				defer outputFile.Close()
				color.Green("Logging POST request bodies to: %s", outputFileName)
			}

			// Prompt for ngrok authtoken
			color.Yellow("Enter your ngrok authtoken (leave empty to try NGROK_AUTHTOKEN env var): ")
			reader := bufio.NewReader(os.Stdin)
			ngrokAuthToken, _ := reader.ReadString('\n')
			ngrokAuthToken = strings.TrimSpace(ngrokAuthToken)

			ctx := context.Background()
			var tunnelOpts []ngrok.ConnectOption
			if ngrokAuthToken != "" {
				tunnelOpts = append(tunnelOpts, ngrok.WithAuthtoken(ngrokAuthToken))
			} else {
				color.Cyan("Attempting to use NGROK_AUTHTOKEN environment variable for ngrok authentication.")
				tunnelOpts = append(tunnelOpts, ngrok.WithAuthtokenFromEnv()) // Fallback to env
			}

			forwardToAddr := fmt.Sprintf("localhost:%d", webhookPort)
			color.Cyan("Configuring ngrok to forward to local port: %s", color.MagentaString("%d", webhookPort))

			ngrokListener, err := ngrok.Listen(ctx,
				config.HTTPEndpoint(
					config.WithForwardsTo(forwardToAddr),
				),
				tunnelOpts...,
			)
			if err != nil {
				color.Red("Error starting ngrok tunnel: %v", err)
				os.Exit(1)
			}
			defer ngrokListener.Close()

			color.Green("Ngrok tunnel established!")
			fmt.Printf("  Forwarding URL: %s -> %s\n", color.HiGreenString(ngrokListener.URL()), color.MagentaString(forwardToAddr))
			color.Yellow("Use this URL for your X API webhook registration: %s/webhook", color.HiGreenString(ngrokListener.URL()))

			mux := http.NewServeMux()
			mux.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					handleWebhookCRC(w, r, signingSecret)
				} else if r.Method == http.MethodPost {
					bodyBytes, err := io.ReadAll(r.Body)
					if err != nil {
						http.Error(w, "Error reading request body", http.StatusInternalServerError)
						log.Printf("[ERROR] Error reading POST body: %v", err)
						return
					}
					defer r.Body.Close()

					if quietMode {
						log.Printf("[INFO] Received POST %s%s event (quiet mode).", color.BlueString(r.Host), color.BlueString(r.URL.Path))
					} else {
						log.Printf("[INFO] Received POST %s%s event:", color.BlueString(r.Host), color.BlueString(r.URL.Path))
						if prettyMode {
							// Attempt to pretty-print if it's JSON
							var jsonData interface{}
							if json.Unmarshal(bodyBytes, &jsonData) == nil {
								prettyColored := pretty.Color(pretty.Pretty(bodyBytes), pretty.TerminalStyle)
								log.Printf("[DATA] Body:\n%s", string(prettyColored))
							} else {
								// Not valid JSON or some other error, print as raw string
								log.Printf("[DATA] Body (raw, not valid JSON for pretty print):\n%s", string(bodyBytes))
							}
						} else {
							log.Printf("[DATA] Body: %s", string(bodyBytes))
						}
					}

					// Write to output file if specified
					if outputFile != nil {
						if _, err := outputFile.Write(bodyBytes); err != nil {
							log.Printf("[ERROR] Error writing POST body to output file %s: %v", outputFileName, err)
						} else {
							// Add a separator for readability
							if _, err := outputFile.WriteString("\n--------------------\n"); err != nil {
								log.Printf("[ERROR] Error writing separator to output file %s: %v", outputFileName, err)
							}
							log.Printf("[INFO] POST body written to %s", color.GreenString(outputFileName))
						}
					}

					w.WriteHeader(http.StatusOK)
				} else {
					http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				}
			})

			color.Cyan("Starting local HTTP server to handle requests from ngrok tunnel (forwarded from %s)...", color.HiGreenString(ngrokListener.URL()))
			if err := http.Serve(ngrokListener, mux); err != nil {
				if err != http.ErrServerClosed {
					color.Red("HTTP server error: %v", err)
					os.Exit(1)
				} else {
					color.Yellow("HTTP server closed gracefully.")
				}
			}
			color.Yellow("Webhook server and ngrok tunnel shut down.")
		},
	}

	webhookStartCmd.Flags().IntVarP(&webhookPort, "port", "p", 8080, "Local port for the webhook server to listen on (ngrok will forward to this port)")
	webhookStartCmd.Flags().StringVarP(&outputFileName, "output", "o", "", "File to write incoming POST request bodies to")
	webhookStartCmd.Flags().BoolVarP(&quietMode, "quiet", "q", false, "Enable quiet mode (logs only that a POST event was received, not the full body to console)")
	webhookStartCmd.Flags().BoolVarP(&prettyMode, "pretty", "P", false, "Pretty-print JSON POST bodies in console output (ignored if -q is used)")

	webhookCmd.AddCommand(webhookStartCmd)
	return webhookCmd
}

func webhookSigningSecret(a *auth.Auth) (string, error) {
	if a == nil || a.TokenStore == nil {
		return "", fmt.Errorf("authentication module not initialized properly")
	}
	appName := a.TokenStore.GetActiveAppName(a.AppName())
	// Resolve only the selected app: ResolveApp would fall back to the default
	// app for an unknown name, potentially signing with another app's secret.
	app := a.TokenStore.GetApp(appName)
	if app != nil {
		if app.ClientSecret != "" {
			return app.ClientSecret, nil
		}
		if token := app.OAuth1Token; token != nil && token.OAuth1 != nil && token.OAuth1.ConsumerSecret != "" {
			return token.OAuth1.ConsumerSecret, nil
		}
	}
	return "", fmt.Errorf("webhook signing secret not found for app %q: configure an OAuth 2.0 client secret with 'xurl auth apps add' or 'xurl auth apps update', or an OAuth 1.0a consumer secret for legacy integrations", appName)
}

func handleWebhookCRC(w http.ResponseWriter, r *http.Request, signingSecret string) {
	crcToken := r.URL.Query().Get("crc_token")
	if crcToken == "" {
		http.Error(w, "Error: crc_token missing from request", http.StatusBadRequest)
		log.Printf("[WARN] Received GET /webhook without crc_token")
		return
	}
	log.Printf("[INFO] Received GET %s%s with crc_token: %s", color.BlueString(r.Host), color.BlueString(r.URL.Path), color.YellowString(crcToken))

	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte(crcToken))
	hashedToken := mac.Sum(nil)
	encodedToken := base64.StdEncoding.EncodeToString(hashedToken)

	response := map[string]string{
		"response_token": "sha256=" + encodedToken,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
	log.Printf("[INFO] Responded to CRC check with token: %s", color.GreenString(response["response_token"]))
}
