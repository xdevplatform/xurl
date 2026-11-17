package cli

import (
	"strings"
	"testing"
)

func TestResolveNgrokAuthtoken(t *testing.T) {
	env := func(values map[string]string) func(string) (string, bool) {
		return func(key string) (string, bool) {
			value, ok := values[key]
			return value, ok
		}
	}
	tests := []struct {
		name    string
		input   string
		env     map[string]string
		want    string
		wantErr bool
	}{
		{name: "typed token wins", input: "typed\n", env: map[string]string{ngrokAuthtokenEnv: "from-env"}, want: "typed"},
		{name: "blank prompt falls back to the environment", input: "\n", env: map[string]string{ngrokAuthtokenEnv: "from-env"}, want: "from-env"},
		{name: "surrounding whitespace is trimmed", input: "  \t\n", env: map[string]string{ngrokAuthtokenEnv: " from-env \n"}, want: "from-env"},
		{name: "blank prompt and no variable is an error", input: "\n", env: map[string]string{}, wantErr: true},
		{name: "blank prompt and an empty variable is an error", input: "", env: map[string]string{ngrokAuthtokenEnv: ""}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveNgrokAuthtoken(tt.input, env(tt.env))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got token %q", got)
				}
				if !strings.Contains(err.Error(), ngrokAuthtokenEnv) {
					t.Fatalf("the error should say how to supply a token, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
