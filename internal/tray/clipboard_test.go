package tray

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClipboardCommandsPerPlatform(t *testing.T) {
	assert.Equal(t, [][]string{{"pbcopy"}}, clipboardCommands("darwin"))
	assert.Equal(t, [][]string{{"clip"}}, clipboardCommands("windows"))
	linux := clipboardCommands("linux")
	assert.Equal(t, "wl-copy", linux[0][0], "Wayland 优先")
	assert.Len(t, linux, 3)
}
