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

	"github.com/spf13/cobra"
	"golang.ngrok.com/ngrok"
	"golang.ngrok.com/ngrok/config"
	"xurl/auth"
)

var webhookPort int

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
		Long:  `Starts a local HTTP server and an ngrok tunnel to listen for X API webhook events, including CRC checks.`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Starting webhook server with ngrok...")

			if authInstance == nil || authInstance.TokenStore == nil {
				log.Fatalf("Error: Authentication module not initialized properly.")
				return
			}

			oauth1Token := authInstance.TokenStore.GetOAuth1Tokens()
			if oauth1Token == nil || oauth1Token.OAuth1 == nil || oauth1Token.OAuth1.ConsumerSecret == "" {
				log.Fatalf("Error: OAuth 1.0a consumer secret not found. Please configure OAuth 1.0a credentials using 'xurl auth oauth1'.")
				return
			}
			consumerSecret := oauth1Token.OAuth1.ConsumerSecret

			// Prompt for ngrok authtoken
			fmt.Print("Enter your ngrok authtoken (leave empty to try NGROK_AUTHTOKEN env var): ")
			reader := bufio.NewReader(os.Stdin)
			ngrokAuthToken, _ := reader.ReadString('\n')
			ngrokAuthToken = strings.TrimSpace(ngrokAuthToken)

			ctx := context.Background()
			var tunnelOpts []ngrok.ConnectOption
			if ngrokAuthToken != "" {
				tunnelOpts = append(tunnelOpts, ngrok.WithAuthtoken(ngrokAuthToken))
			} else {
				tunnelOpts = append(tunnelOpts, ngrok.WithAuthtokenFromEnv()) // Fallback to env
			}

			forwardToAddr := fmt.Sprintf("localhost:%d", webhookPort)
			fmt.Printf("Configuring ngrok to forward to local port: %d\n", webhookPort)

			ngrokListener, err := ngrok.Listen(ctx,
				config.HTTPEndpoint(
					config.WithForwardsTo(forwardToAddr), // Tell ngrok to forward to our specific local port
				),
				tunnelOpts...,
			)
			if err != nil {
				log.Fatalf("Error starting ngrok tunnel: %v", err)
			}
			defer ngrokListener.Close()
			fmt.Printf("Ngrok tunnel established. Forwarding URL: %s -> %s\n", ngrokListener.URL(), forwardToAddr)
			fmt.Printf("Use this URL for your X API webhook registration: %s/webhook\n", ngrokListener.URL())


			http.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					crcToken := r.URL.Query().Get("crc_token")
					if crcToken == "" {
						http.Error(w, "Error: crc_token missing from request", http.StatusBadRequest)
						log.Println("Received GET /webhook without crc_token")
						return
					}
					log.Printf("Received GET %s%s with crc_token: %s\n", r.Host, r.URL.Path, crcToken)

					mac := hmac.New(sha256.New, []byte(consumerSecret))
					mac.Write([]byte(crcToken))
					hashedToken := mac.Sum(nil)
					encodedToken := base64.StdEncoding.EncodeToString(hashedToken)

					response := map[string]string{
						"response_token": "sha256=" + encodedToken,
					}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(response)
					log.Printf("Responded to CRC check with token: %s\n", response["response_token"])

				} else if r.Method == http.MethodPost {
					log.Printf("Received POST %s%s event:\n", r.Host, r.URL.Path)
					body, err := io.ReadAll(r.Body)
					if err != nil {
						http.Error(w, "Error reading request body", http.StatusInternalServerError)
						log.Printf("Error reading POST body: %v\n", err)
						return
					}
					defer r.Body.Close()
					log.Printf("Body: %s\n", string(body))
					// For now, just acknowledge receipt
					w.WriteHeader(http.StatusOK)
				} else {
					http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				}
			})
			
			// The HTTP server will serve requests coming through the ngrok listener.
			// The local port webhookPort is what ngrok forwards to internally.
			fmt.Printf("Starting local HTTP server to handle requests from ngrok tunnel (forwarded from %s)...\n", ngrokListener.URL())
			if err := http.Serve(ngrokListener, nil); err != nil {
				// Only log fatal if it's not a graceful shutdown of the listener (e.g. by ngrokListener.Close())
				if err != http.ErrServerClosed {
					log.Fatalf("HTTP server error: %v", err)
				} else {
					log.Println("HTTP server closed gracefully.")
				}
			}
			log.Println("Webhook server and ngrok tunnel shut down.")
		},
	}

	webhookStartCmd.Flags().IntVarP(&webhookPort, "port", "p", 8080, "Local port for the webhook server to listen on (ngrok will forward to this port)")

	webhookCmd.AddCommand(webhookStartCmd)
	return webhookCmd
} 