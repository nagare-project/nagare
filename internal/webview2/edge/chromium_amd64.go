//go:build windows
// +build windows

package edge

import (
	"github.com/nagare-project/nagare/internal/webview2/w32"
)

func (e *Chromium) SetSize(bounds w32.Rect) {
	if e.controller == nil {
		return
	}

	err := e.controller.PutBounds(bounds)
	if err != nil {
		e.errorCallback(err)
	}
}
