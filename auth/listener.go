package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	xurlErrors "github.com/xdevplatform/xurl/errors"
)

func StartListener(addresses []string, callbackPath string, callback func(code, state string) error, ready chan<- struct{}) error {
	mux := http.NewServeMux()
	done := make(chan error, 1)
	servers := make([]*http.Server, 0, len(addresses))
	listeners := make([]net.Listener, 0, len(addresses))
	var doneOnce sync.Once

	finish := func(err error) {
		doneOnce.Do(func() {
			done <- err
			go func() {
				for _, server := range servers {
					_ = server.Shutdown(context.Background())
				}
			}()
		})
	}

	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")

		err := callback(code, state)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "Error: %s", err.Error())
			finish(err)
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Authentication successful! You can close this window.")

		finish(nil)
	})

	for _, address := range addresses {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			for _, existing := range listeners {
				_ = existing.Close()
			}
			return xurlErrors.NewAuthError("ServerError", err)
		}
		listeners = append(listeners, listener)
		servers = append(servers, &http.Server{
			Addr:    address,
			Handler: mux,
		})
	}

	if ready != nil {
		close(ready)
	}

	for i, listener := range listeners {
		server := servers[i]
		go func(server *http.Server, listener net.Listener) {
			if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
				finish(xurlErrors.NewAuthError("ServerError", err))
			}
		}(server, listener)
	}

	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Minute):
		for _, server := range servers {
			_ = server.Shutdown(context.Background())
		}
		return xurlErrors.NewAuthError("Timeout", errors.New("timeout waiting for callback"))
	}
}
