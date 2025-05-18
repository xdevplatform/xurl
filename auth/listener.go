package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	xurlErrors "github.com/xdevplatform/xurl/errors"
)

func StartListener(port int, callback func(code, state string) error) error {
	server := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", port),
		Handler: http.DefaultServeMux,
	}

	done := make(chan error, 1)

	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")

		err := callback(code, state)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "Error: %s", err.Error())
			done <- err
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Authentication successful! You can close this window.")

		done <- nil

		go func() {
			server.Shutdown(context.Background())
		}()
	})

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			done <- xurlErrors.NewAuthError("ServerError", err)
		}
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Minute):
		server.Shutdown(context.Background())
		return xurlErrors.NewAuthError("Timeout", errors.New("timeout waiting for callback"))
	}
}
