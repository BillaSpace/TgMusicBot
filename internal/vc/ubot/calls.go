package ubot

import "github.com/AshokShau/TgMusicBot/internal/vc/ntgcalls"

func (ctx *Context) Calls() map[int64]*ntgcalls.CallInfo {
	return ctx.binding.Calls()
}
