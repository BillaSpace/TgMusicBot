/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package vc

import (
	"fmt"
	"html" // for html.EscapeString

	"github.com/AshokShau/TgMusicBot/pkg/config"
	"github.com/AshokShau/TgMusicBot/pkg/core/cache"

	"github.com/Laky-64/gologging"
	tg "github.com/amarnathcjd/gogram/telegram"
)

// sendLogger sends a formatted log message to the designated logger chat.
// It includes details about the song being played, such as its title, duration, and the user who requested it.
func sendLogger(client *tg.Client, chatID int64, song *cache.CachedTrack) {
	if chatID == 0 || song == nil {
		return
	}

	text := fmt.Sprintf(
		"<b>A song is playing</b> in <code>%d</code>\n\n‣ <b>Title:</b> <a href='%s'>%s</a>\n‣ <b>Duration:</b> %s\n‣ <b>Requested by:</b> %s\n‣ <b>Platform:</b> %s\n‣ <b>Is Video:</b> %t",
		chatID,
		html.EscapeString(song.URL),
		html.EscapeString(song.Name),
		cache.SecToMin(song.Duration),
		html.EscapeString(song.User),
		html.EscapeString(song.Platform),
		song.IsVideo,
	)

	_, err := client.SendMessage(config.Conf.LoggerId, text, &tg.SendOptions{
		ParseMode:   tg.HTML, // correct constant name in gogram
		LinkPreview: false,
	})
	if err != nil {
		gologging.WarnF("[sendLogger] Failed to send the message to %d: %v", config.Conf.LoggerId, err)
	} else {
		gologging.InfoF("[sendLogger] Sent log message for chat %d", chatID)
	}
}
