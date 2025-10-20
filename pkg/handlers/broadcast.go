package handlers

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/BillaSpace/TgMusicBot/pkg/core/db"
	"github.com/amarnathcjd/gogram/telegram"
	"go.mongodb.org/mongo-driver/bson/primitive"
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
	for _, arg := range args {
		switch arg {
		case "all", "users", "chats":
			target = arg
		case "copy":
			asCopy = true
		}
	}

	if target == "" {
		_, _ = m.Reply("⚠️ Invalid target. Use: all, users, or chats.\n\n"+broadcastUsage, telegram.SendOptions{ParseMode: "html"})
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// --- Fetch IDs ---
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

	mode := "forward"
	if asCopy {
		mode = "copy"
	}

	progressMsg, _ := m.Reply(fmt.Sprintf("📢 Starting broadcast to %s (%s mode)...", target, mode), telegram.SendOptions{ParseMode: "html"})

	var text string
	if asCopy {
		text = repliedMsg.Text()
		if text == "" {
			_, _ = progressMsg.Edit("⚠️ The replied message has no text to broadcast.", telegram.SendOptions{})
			return nil
		}
	}

	// --- Broadcast with concurrency ---
	var mu sync.Mutex
	userSuccess, userFailed := 0, 0
	chatSuccess, chatFailed := 0, 0
	sentCount := 0
	totalTargets := len(chatIDs) + len(userIDs)

	sem := make(chan struct{}, 10) // concurrency limit
	var wg sync.WaitGroup

	sendFunc := func(id int64, isChat bool) {
		defer wg.Done()
		sem <- struct{}{}
		defer func() { <-sem }()

		var err error
		if asCopy {
			_, err = m.Client.SendMessage(id, text, &telegram.SendOptions{LinkPreview: false})
		} else {
			_, err = repliedMsg.ForwardTo(id)
		}

		mu.Lock()
		defer mu.Unlock()

		if err != nil {
			// skip invalid chats/users silently
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
		sentCount++

		// update progress every 20 messages or at the end
		if sentCount%20 == 0 || sentCount == totalTargets {
			progress := fmt.Sprintf(
				"📢 Broadcast in progress (%s mode)...\n\n👤 Users: ✅ %d | ❌ %d\n🏢 Chats: ✅ %d | ❌ %d\n\nSent %d/%d",
				mode, userSuccess, userFailed, chatSuccess, chatFailed, sentCount, totalTargets,
			)
			_, _ = progressMsg.Edit(progress, telegram.SendOptions{ParseMode: "html"})
		}
	}

	// Send to chats
	for _, chatID := range chatIDs {
		wg.Add(1)
		go sendFunc(chatID, true)
	}

	// Send to users
	for _, userID := range userIDs {
		wg.Add(1)
		go sendFunc(userID, false)
	}

	wg.Wait()

	// --- Final result ---
	finalMsg := fmt.Sprintf(
		"📢 Broadcast completed!\n\n👤 Users: ✅ %d | ❌ %d\n🏢 Chats: ✅ %d | ❌ %d",
		userSuccess, userFailed,
		chatSuccess, chatFailed,
	)
	_, _ = progressMsg.Edit(finalMsg, telegram.SendOptions{ParseMode: "html"})
	return nil
}

// --- Helpers to safely decode IDs from MongoDB ---

func getAllChatsSafe(ctx context.Context) ([]int64, error) {
	cursor, err := db.Instance.ChatDB.Find(ctx, map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var ids []int64
	for cursor.Next(ctx) {
		var raw map[string]interface{}
		if err := cursor.Decode(&raw); err != nil {
			continue
		}
		if id, ok := raw["_id"].(int64); ok {
			ids = append(ids, id)
		} else if oid, ok := raw["_id"].(primitive.ObjectID); ok {
			// fallback: convert ObjectID timestamp as int64
			ids = append(ids, int64(oid.Timestamp().Unix()))
		}
	}
	return ids, nil
}

func getAllUsersSafe(ctx context.Context) ([]int64, error) {
	cursor, err := db.Instance.UserDB.Find(ctx, map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var ids []int64
	for cursor.Next(ctx) {
		var raw map[string]interface{}
		if err := cursor.Decode(&raw); err != nil {
			continue
		}
		if id, ok := raw["_id"].(int64); ok {
			ids = append(ids, id)
		} else if oid, ok := raw["_id"].(primitive.ObjectID); ok {
			ids = append(ids, int64(oid.Timestamp().Unix()))
		}
	}
	return ids, nil
}
