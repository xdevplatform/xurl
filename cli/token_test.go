package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAppFlagHint(t *testing.T) {
	assert.Equal(t, "", appFlagHint(""), "no app override yields no hint")
	assert.Equal(t, " --app my-app", appFlagHint("my-app"))
}
