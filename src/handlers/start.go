/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package handlers

import (
	"fmt"
	"runtime"
	"time"

	"ashokshau/tgmusic/src/config"
	"ashokshau/tgmusic/src/core"
	"ashokshau/tgmusic/src/core/db"
	"ashokshau/tgmusic/src/lang"

	"github.com/amarnathcjd/gogram/telegram"
)

func pingHandler(m *telegram.NewMessage) error {
	start := time.Now()
	updateLag := time.Since(time.Unix(int64(m.Date()), 0)).Milliseconds()

	msg, err := m.Reply("⏱️ Pinging...")
	if err != nil {
		return err
	}

	latency := time.Since(start).Milliseconds()
	uptime := time.Since(startTime).Truncate(time.Second)
	senders := m.Client.GetExportedSendersStatus()

	response := fmt.Sprintf(
		"<b>📊 System Performance Metrics</b>\n\n"+
			"⏱️ <b>Bot Latency:</b> <code>%d ms</code>\n"+
			"🕒 <b>Uptime:</b> <code>%s</code>\n"+
			"📩 <b>Update Lag:</b> <code>%d ms</code>\n"+
			"⚙️ <b>Go Routines:</b> <code>%d</code>\n"+
			"📨 <b>Senders:</b> <code>%d</code>\n",
		latency, uptime, updateLag, runtime.NumGoroutine(), senders,
	)

	_, err = msg.Edit(response)
	return err
}

func startHandler(m *telegram.NewMessage) error {
	bot := m.Client.Me()
	chatID := m.ChannelID()

	if m.IsPrivate() {
		go func(id int64) {
			ctx, cancel := db.Ctx()
			defer cancel()
			_ = db.Instance.AddUser(ctx, id)
		}(chatID)
	} else {
		go func(id int64) {
			ctx, cancel := db.Ctx()
			defer cancel()
			_ = db.Instance.AddChat(ctx, id)
		}(chatID)
	}

	ctx, cancel := db.Ctx()
	defer cancel()
	langCode := db.Instance.GetLang(ctx, chatID)

	text := fmt.Sprintf(
		lang.GetString(langCode, "start_text"),
		m.Sender.FirstName,
		bot.FirstName,
	)

	if m.IsPrivate() && config.Conf.StartImg != "" {
		_, err := m.Client.SendMessage(
			m.Chat(),
			"",
			&telegram.SendOptions{
				Media: &telegram.InputMediaPhoto{
					File:    config.Conf.StartImg,
					Caption: text,
				},
				ReplyMarkup: core.AddMeMarkup(bot.Username),
			},
		)
		return err
	}

	_, err := m.Reply(text, &telegram.SendOptions{
		ReplyMarkup: core.AddMeMarkup(bot.Username),
	})
	return err
}
