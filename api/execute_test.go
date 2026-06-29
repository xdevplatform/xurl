package api

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xurlErrors "github.com/xdevplatform/xurl/errors"
)

// redirectColor sends colorized output (used by FormatAndPrintResponse) to w and
// disables ANSI, returning a restore func. fatih/color writes to color.Output,
// which is captured at init, so reassigning os.Stdout alone would not capture it.
func redirectColor(w io.Writer) func() {
	oldOut, oldNoColor := color.Output, color.NoColor
	color.Output = w
	color.NoColor = true
	return func() {
		color.Output = oldOut
		color.NoColor = oldNoColor
	}
}

func TestHandleRequestError(t *testing.T) {
	t.Run("non-JSON error is returned unchanged and prints nothing", func(t *testing.T) {
		var buf bytes.Buffer
		defer redirectColor(&buf)()

		origErr := fmt.Errorf("dial tcp 127.0.0.1:9: connect: connection refused")
		got := handleRequestError(origErr)

		require.Error(t, got)
		assert.Contains(t, got.Error(), "connection refused", "real error message must be preserved")
		assert.Empty(t, strings.TrimSpace(buf.String()), "must not print anything for a non-JSON error")
		assert.NotContains(t, buf.String(), "null", "the old null-printing regression must not return")
	})

	t.Run("JSON API error body is printed and request-failed is returned", func(t *testing.T) {
		var buf bytes.Buffer
		defer redirectColor(&buf)()

		apiErr := xurlErrors.NewAPIError([]byte(`{"errors":[{"message":"bad request"}]}`))
		got := handleRequestError(apiErr)

		require.Error(t, got)
		assert.Equal(t, "request failed", got.Error())
		assert.Contains(t, buf.String(), "bad request", "the JSON error body should be printed")
	})
}
