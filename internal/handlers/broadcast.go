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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AshokShau/TgMusicBot/internal/core/db"
	"github.com/amarnathcjd/gogram/telegram"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
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

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var chatIDs, userIDs []int64

	if target == "chats" || target == "all" {
		if chatIDs, err = getAllChatsSafe(ctx); err != nil {
			_, _ = m.Reply("❌ Failed to fetch chat IDs from the database.", telegram.SendOptions{})
			return err
		}
	}
	if target == "users" || target == "all" {
		if userIDs, err = getAllUsersSafe(ctx); err != nil {
			_, _ = m.Reply("❌ Failed to fetch user IDs from the database.", telegram.SendOptions{})
			return err
		}
	}

	total := len(chatIDs) + len(userIDs)
	if total == 0 {
		_, _ = m.Reply("ℹ️ Nothing to broadcast to (no users/chats found).", telegram.SendOptions{})
		return nil
	}

	mode := "forward"
	if asCopy {
		mode = "copy"
	}

	progressMsg, _ := m.Reply(
		fmt.Sprintf("📢 Starting broadcast to %s (%s mode)...", target, mode),
		telegram.SendOptions{ParseMode: "html"},
	)

	var copyText string
	if asCopy {
		copyText = repliedMsg.Text()
		if copyText == "" {
			_, _ = progressMsg.Edit("⚠️ The replied message has no text to broadcast in copy mode.", telegram.SendOptions{})
			return nil
		}
	}

	var mu sync.Mutex
	userOK, userFail := 0, 0
	chatOK, chatFail := 0, 0
	sent := 0

	sem := make(chan struct{}, 12)
	var wg sync.WaitGroup

	send := func(id int64, isChat bool) {
		defer wg.Done()
		sem <- struct{}{}
		defer func() { <-sem }()

		var e error
		if asCopy {
			_, e = m.Client.SendMessage(id, copyText, &telegram.SendOptions{LinkPreview: false})
		} else {
			_, e = repliedMsg.ForwardTo(id)
		}

		mu.Lock()
		if e != nil {
			if isChat {
				chatFail++
			} else {
				userFail++
			}
		} else {
			if isChat {
				chatOK++
			} else {
				userOK++
			}
		}
		sent++
		if sent%20 == 0 || sent == total {
			_, _ = progressMsg.Edit(fmt.Sprintf(
				"📢 Broadcast in progress (%s mode)...\n\n👤 Users: ✅ %d | ❌ %d\n🏢 Chats: ✅ %d | ❌ %d\n\nSent %d/%d",
				mode, userOK, userFail, chatOK, chatFail, sent, total,
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
		userOK, userFail, chatOK, chatFail,
	), telegram.SendOptions{ParseMode: "html"})

	return nil
}

// ---- local safe getters (decode ObjectID by checking common ID fields) ----

func getAllChatsSafe(ctx context.Context) ([]int64, error) {
	coll := db.Instance.DB.Collection("chats")
	// Only fetch id-looking fields to reduce payload
	cur, err := coll.Find(ctx, bson.M{}, &mongo.FindOptions{
		Projection: bson.M{
			"_id":     1,
			"chat_id": 1, "id": 1, "tg_id": 1, "peer_id": 1,
		},
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = cur.Close(ctx) }()

	var ids []int64
	for cur.Next(ctx) {
		var raw bson.M
		if err := cur.Decode(&raw); err != nil {
			continue
		}
		if id, ok := extractNumericID(raw); ok {
			ids = append(ids, id)
		}
	}
	if err := cur.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

func getAllUsersSafe(ctx context.Context) ([]int64, error) {
	coll := db.Instance.DB.Collection("users")
	cur, err := coll.Find(ctx, bson.M{}, &mongo.FindOptions{
		Projection: bson.M{
			"_id":     1,
			"user_id": 1, "id": 1, "tg_id": 1,
		},
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = cur.Close(ctx) }()

	var ids []int64
	for cur.Next(ctx) {
		var raw bson.M
		if err := cur.Decode(&raw); err != nil {
			continue
		}
		if id, ok := extractNumericID(raw); ok {
			ids = append(ids, id)
		}
	}
	if err := cur.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

// extractNumericID returns a usable Telegram ID, handling multiple shapes:
// 1) Numeric/string _id directly
// 2) _id is ObjectID -> try fields: chat_id, user_id, id, tg_id, peer_id
func extractNumericID(doc bson.M) (int64, bool) {
	// Fast path: _id already numeric or numeric-string
	if id, ok := toInt64(doc["_id"]); ok {
		return id, true
	}
	// ObjectID or other -> check common fields
	candidates := []string{"chat_id", "user_id", "id", "tg_id", "peer_id"}
	for _, k := range candidates {
		if id, ok := toInt64(doc[k]); ok {
			return id, true
		}
	}
	// As a last resort, if _id is ObjectID and there’s an embedded "chat" or "user" map with "id"
	switch t := doc["_id"].(type) {
	case primitive.ObjectID:
		_ = t // just to acknowledge
	}
	if embedded, ok := doc["chat"].(bson.M); ok {
		if id, ok := toInt64(embedded["id"]); ok {
			return id, true
		}
	}
	if embedded, ok := doc["user"].(bson.M); ok {
		if id, ok := toInt64(embedded["id"]); ok {
			return id, true
		}
	}
	return 0, false
}

func toInt64(v any) (int64, bool) {
	switch t := v.(type) {
	case int64:
		return t, true
	case int32:
		return int64(t), true
	case float64:
		return int64(t), true
	case string:
		if n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64); err == nil {
			return n, true
		}
	default:
		// primitive.ObjectID and others are not directly convertible
	}
	return 0, false
}
