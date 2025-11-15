package ubot

func (ctx *Context) Stop(chatId int64) error {
	ctx.presentations = stdRemove(ctx.presentations, chatId)
	delete(ctx.pendingPresentation, chatId)
	delete(ctx.callSources, chatId)
	err := ctx.binding.Stop(chatId)
	if err != nil {
		return err
	}
	ctx.groupCallsMutex.RLock()
	call := ctx.inputGroupCalls[chatId]
	ctx.groupCallsMutex.RUnlock()
	_, err = ctx.App.PhoneLeaveGroupCall(call, 0)
	if err != nil {
		return err
	}
	return nil
}
