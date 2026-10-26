//go:build !windows

package keyboard

import "errors"

type Hook struct{}

func (h *Hook) Start(func(string)) error {
	return errors.New("键盘监听仅支持 Windows")
}
func (h *Hook) Stop()         {}
func (h *Hook) Running() bool { return false }
