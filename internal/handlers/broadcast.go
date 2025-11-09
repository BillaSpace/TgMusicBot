/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package handlers

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/AshokShau/TgMusicBot/internal/core/db"
	"github.com/amarnathcjd/gogram/telegram"
)

const broadcastUsage = `⚠️ Usage: <code>/broadcast [all|users|chats] [copy]</code>
• <b>all</b>: All users and chats
• <b>users</b>: Only users
• <b>chats</b>: Only groups/channels
• <b>copy</b>: Send as copy (no forward tag)`

func broadcastHandler(m *telegram.NewMessage) error {
	if !m.IsReply() {
		_, _ = m.Reply(broadcastUsage, telegram.SendOptions{ParseMode: "html"})
		return nil
	}

	repliedMsg, err := m.GetReplyMessage()
	if err != nil || repliedMsg.Chat == nil {
		msg := "❌ Failed to retrieve the replied message."
		if repliedMsg != nil && repliedMsg.Chat == nil {
			msg = "❌ Replied message has no associated chat."
		}
		_, _ = m.Reply(msg, telegram.SendOptions{})
		return err
	}

	args := strings.Fields(m.Args())
	if len(args) == 0 {
		_, _ = m.Reply("⚠️ Please specify a target.\n\n"+broadcastUsage, telegram.SendOptions{ParseMode: "html"})
		return nil
	}

	var target string
	var asCopy bool
	for _, a := range args {
		switch a {
		case "all", "users", "chats":
			target = a
		case "copy":
			asCopy = true
		}
	}
	if target == "" {
		_, _ = m.Reply("⚠️ Invalid target. Use: all, users, or chats.\n\n"+broadcastUsage, telegram.SendOptions{ParseMode: "html"})
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var chatIDs, userIDs []int64

	if target == "chats" || target == "all" {
		if chatIDs, err = db.Instance.GetAllChats(ctx); err != nil {
			_, _ = m.Reply("❌ Failed to fetch chat IDs from the database.", telegram.SendOptions{})
			return err
		}
	}
	if target == "users" || target == "all" {
		if userIDs, err = db.Instance.GetAllUsers(ctx); err != nil {
			_, _ = m.Reply("❌ Failed to fetch user IDs from the database.", telegram.SendOptions{})
			return err
		}
	}

	totalTargets := len(chatIDs) + len(userIDs)
	if totalTargets == 0 {
		_, _ = m.Reply("ℹ️ Nothing to broadcast to (no users/chats found).", telegram.SendOptions{})
		return nil
	}

	mode := "forward"
	if asCopy {
		mode = "copy"
	}

	progressMsg, _ := m.Reply(fmt.Sprintf("📢 Starting broadcast to %s (%s mode)...", target, mode), telegram.SendOptions{ParseMode: "html"})

	var copyText string
	if asCopy {
		copyText = repliedMsg.Text()
		if copyText == "" {
			_, _ = progressMsg.Edit("⚠️ The replied message has no text to broadcast in copy mode.", telegram.SendOptions{})
			return nil
		}
	}

	var mu sync.Mutex
	userSuccess, userFailed := 0, 0
	chatSuccess, chatFailed := 0, 0
	sent := 0

	sem := make(chan struct{}, 12)
	var wg sync.WaitGroup

	send := func(id int64, isChat bool) {
		defer wg.Done()
		sem <- struct{}{}
		defer func() { <-sem }()

		var sendErr error
		if asCopy {
			_, sendErr = m.Client.SendMessage(id, copyText, &telegram.SendOptions{LinkPreview: false})
		} else {
			_, sendErr = repliedMsg.ForwardTo(id)
		}

		mu.Lock()
		if sendErr != nil {
			if isChat {
				chatFailed++
			} else {
				userFailed++
			}
		} else {
			if isChat {
				chatSuccess++
			} else {
				userSuccess++
			}
		}
		sent++
		if sent%20 == 0 || sent == totalTargets {
			_, _ = progressMsg.Edit(fmt.Sprintf(
				"📢 Broadcast in progress (%s mode)...\n\n👤 Users: ✅ %d | ❌ %d\n🏢 Chats: ✅ %d | ❌ %d\n\nSent %d/%d",
				mode, userSuccess, userFailed, chatSuccess, chatFailed, sent, totalTargets,
			), telegram.SendOptions{ParseMode: "html"})
		}
		mu.Unlock()
	}

	for _, cid := range chatIDs {
		wg.Add(1)
		go send(cid, true)
	}
	for _, uid := range userIDs {
		wg.Add(1)
		go send(uid, false)
	}

	wg.Wait()

	_, _ = progressMsg.Edit(fmt.Sprintf(
		"📢 Broadcast completed!\n\n👤 Users: ✅ %d | ❌ %d\n🏢 Chats: ✅ %d | ❌ %d",
		userSuccess, userFailed, chatSuccess, chatFailed,
	), telegram.SendOptions{ParseMode: "html"})

	return nil
}
