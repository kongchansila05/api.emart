package controllers

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"
    "errors"        // ← add back
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"project-api/middleware"
	"project-api/models"
	"project-api/services"
)

type DirectChatController struct {
	db  *gorm.DB
	hub *services.Hub
}

func NewDirectChatController(db *gorm.DB, hub *services.Hub) *DirectChatController {
	return &DirectChatController{db: db, hub: hub}
}

// ─── WebSocket message handler ────────────────────────────────────────────────

func (dc *DirectChatController) HandleWSMessage(client *services.Client, raw []byte) bool {
	var msg services.WSMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return false
	}
	switch msg.Type {
	case "direct_message":
		dc.handleDirectMessage(client, &msg)
		return true
	case "direct_read":
		dc.handleDirectRead(client, &msg)
		return true
	case "direct_typing":
		dc.handleDirectTyping(client, &msg)
		return true
	}
	return false
}

func (dc *DirectChatController) sendError(client *services.Client, errMsg string) {
	data, _ := json.Marshal(&services.WSMessage{Type: "error", Error: errMsg})
	select {
	case client.Send <- data:
	default:
	}
}

// handleDirectMessage — save and deliver a direct message via WebSocket
func (dc *DirectChatController) handleDirectMessage(client *services.Client, msg *services.WSMessage) {
	// Allow content only, image only, or both
	if msg.ConversationID == 0 || (msg.Content == "" && msg.ImageURL == "") {
		dc.sendError(client, "conversation_id and content or image_url required")
		return
	}

	var conv models.DirectConversation
	if err := dc.db.First(&conv, msg.ConversationID).Error; err != nil {
		dc.sendError(client, "Direct conversation not found")
		return
	}
	if conv.User1ID != client.UserID && conv.User2ID != client.UserID {
		dc.sendError(client, "Access denied")
		return
	}

	dm := models.DirectMessage{
		ConversationID: conv.ID,
		SenderID:       client.UserID,
		Content:        msg.Content,
		ImageURL:       msg.ImageURL, // ← add
		IsRead:         false,
	}
	if err := dc.db.Create(&dm).Error; err != nil {
		dc.sendError(client, "Failed to save message")
		return
	}
	dc.db.Preload("Sender").First(&dm, dm.ID)

	// last_message preview
	lastMsg := msg.Content
	if lastMsg == "" {
		lastMsg = "📷 Image"
	}

	now := time.Now()
	updates := map[string]interface{}{
		"last_message":    lastMsg, // ← use lastMsg
		"last_message_at": &now,
	}
	if client.UserID == conv.User1ID {
		updates["unread_user2"] = gorm.Expr("unread_user2 + 1")
	} else {
		updates["unread_user1"] = gorm.Expr("unread_user1 + 1")
	}
	dc.db.Model(&conv).Updates(updates)

	recipientID := conv.User2ID
	if client.UserID == conv.User2ID {
		recipientID = conv.User1ID
	}

	out := &services.WSMessage{
		Type:           "direct_message",
		ID:             dm.ID,
		ConversationID: conv.ID,
		SenderID:       client.UserID,
		RecipientID:    recipientID,
		Content:        dm.Content,
		ImageURL:       dm.ImageURL, // ← add
		IsRead:         false,
		CreatedAt:      dm.CreatedAt.Format(time.RFC3339),
	}
	dc.hub.SendToUser(recipientID, out)
	dc.hub.SendToUser(client.UserID, out)

	log.Printf("[DirectChat] User %d → User %d (conv %d)", client.UserID, recipientID, conv.ID)
}

// handleDirectRead — mark messages as read via WebSocket
func (dc *DirectChatController) handleDirectRead(client *services.Client, msg *services.WSMessage) {
	if msg.ConversationID == 0 {
		return
	}
	var conv models.DirectConversation
	if err := dc.db.First(&conv, msg.ConversationID).Error; err != nil {
		return
	}
	if conv.User1ID != client.UserID && conv.User2ID != client.UserID {
		return
	}

	dc.db.Model(&models.DirectMessage{}).
		Where("conversation_id = ? AND sender_id != ? AND is_read = false", msg.ConversationID, client.UserID).
		Update("is_read", true)

	if client.UserID == conv.User1ID {
		dc.db.Model(&conv).Update("unread_user1", 0)
	} else {
		dc.db.Model(&conv).Update("unread_user2", 0)
	}

	otherID := conv.User2ID
	if client.UserID == conv.User2ID {
		otherID = conv.User1ID
	}
	dc.hub.SendToUser(otherID, &services.WSMessage{
		Type:           "direct_read",
		ConversationID: msg.ConversationID,
		SenderID:       client.UserID,
	})
}

// handleDirectTyping — forward typing indicator via WebSocket
func (dc *DirectChatController) handleDirectTyping(client *services.Client, msg *services.WSMessage) {
	if msg.ConversationID == 0 {
		return
	}
	var conv models.DirectConversation
	if err := dc.db.First(&conv, msg.ConversationID).Error; err != nil {
		return
	}
	otherID := conv.User2ID
	if client.UserID == conv.User2ID {
		otherID = conv.User1ID
	}
	dc.hub.SendToUser(otherID, &services.WSMessage{
		Type:           "direct_typing",
		ConversationID: conv.ID,
		SenderID:       client.UserID,
	})
}

// ─── REST: Client ─────────────────────────────────────────────────────────────

// StartDirectConversation — create or get a direct conversation with another user
// POST /api/direct/start
// Body: { "recipient_id": 5, "content": "Hello!" }
func (dc *DirectChatController) StartDirectConversation(c *gin.Context) {
	myID := c.GetUint(middleware.CtxUserID)

	var req struct {
		RecipientID uint   `json:"recipient_id" binding:"required"`
		Content     string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.RecipientID == myID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot start a conversation with yourself"})
		return
	}

	// Check recipient exists
	var recipient models.User
	if err := dc.db.First(&recipient, req.RecipientID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	// Normalize: smaller ID = user1
	user1ID, user2ID := myID, req.RecipientID
	if user1ID > user2ID {
		user1ID, user2ID = user2ID, user1ID
	}

	var conv models.DirectConversation
	err := dc.db.
		Where("user1_id = ? AND user2_id = ?", user1ID, user2ID).
		Preload("User1").Preload("User2").
		First(&conv).Error

	isNew := false
	if errors.Is(err, gorm.ErrRecordNotFound) {
		conv = models.DirectConversation{
			User1ID: user1ID,
			User2ID: user2ID,
		}
		if err := dc.db.Create(&conv).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create conversation"})
			return
		}
		dc.db.Preload("User1").Preload("User2").First(&conv, conv.ID)
		isNew = true
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// ── Send first message if content provided (new conversation only) ────────
	var firstMessage *models.DirectMessage
	if req.Content != "" && isNew {
		dm := models.DirectMessage{
			ConversationID: conv.ID,
			SenderID:       myID,
			Content:        req.Content,
			IsRead:         false,
		}
		if err := dc.db.Create(&dm).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send message"})
			return
		}
		dc.db.Preload("Sender").First(&dm, dm.ID)

		now := time.Now()
		updates := map[string]interface{}{
			"last_message":    req.Content,
			"last_message_at": &now,
		}
		if myID == conv.User1ID {
			updates["unread_user2"] = gorm.Expr("unread_user2 + 1")
		} else {
			updates["unread_user1"] = gorm.Expr("unread_user1 + 1")
		}
		dc.db.Model(&conv).Updates(updates)

		if dc.hub != nil {
			dc.hub.SendToUser(req.RecipientID, &services.WSMessage{
				Type:           "direct_message",
				ID:             dm.ID,
				ConversationID: conv.ID,
				SenderID:       myID,
				RecipientID:    req.RecipientID,
				Content:        dm.Content,
				IsRead:         false,
				CreatedAt:      dm.CreatedAt.Format(time.RFC3339),
			})
		}
		firstMessage = &dm
	}

	c.JSON(http.StatusOK, gin.H{
		"conversation": conv,
		"message":      firstMessage, // null if no content or existing conversation
	})
}


// SendDirectMessage — send a message (text, image, or both)
// POST /api/direct/conversations/:id/messages
// Body: { "content": "Hey!", "image_url": "https://r2.../img.jpg" }
func (dc *DirectChatController) SendDirectMessage(c *gin.Context) {
	myID := c.GetUint(middleware.CtxUserID)
	convID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid conversation ID"})
		return
	}

	var req struct {
		Content  string `json:"content"`
		ImageURL string `json:"image_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Must have at least content or image
	if req.Content == "" && req.ImageURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "content or image_url is required"})
		return
	}

	var conv models.DirectConversation
	if err := dc.db.First(&conv, convID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Conversation not found"})
		return
	}
	if conv.User1ID != myID && conv.User2ID != myID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	dm := models.DirectMessage{
		ConversationID: conv.ID,
		SenderID:       myID,
		Content:        req.Content,
		ImageURL:       req.ImageURL,
		IsRead:         false,
	}
	if err := dc.db.Create(&dm).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to send message"})
		return
	}
	dc.db.Preload("Sender").First(&dm, dm.ID)

	recipientID := conv.User2ID
	if myID == conv.User2ID {
		recipientID = conv.User1ID
	}

	// last_message preview
	lastMsg := req.Content
	if lastMsg == "" {
		lastMsg = "📷 Image"
	}

	now := time.Now()
	updates := map[string]interface{}{
		"last_message":    lastMsg,
		"last_message_at": &now,
	}
	if myID == conv.User1ID {
		updates["unread_user2"] = gorm.Expr("unread_user2 + 1")
	} else {
		updates["unread_user1"] = gorm.Expr("unread_user1 + 1")
	}
	dc.db.Model(&conv).Updates(updates)

	if dc.hub != nil {
		out := &services.WSMessage{
			Type:           "direct_message",
			ID:             dm.ID,
			ConversationID: conv.ID,
			SenderID:       myID,
			RecipientID:    recipientID,
			Content:        dm.Content,
			ImageURL:       dm.ImageURL,
			IsRead:         false,
			CreatedAt:      dm.CreatedAt.Format(time.RFC3339),
		}
		dc.hub.SendToUser(recipientID, out)
		dc.hub.SendToUser(myID, out)
	}

	c.JSON(http.StatusCreated, dm)
}

// GetMyDirectConversations — list all conversations for logged-in user
// GET /api/direct/conversations
func (dc *DirectChatController) GetMyDirectConversations(c *gin.Context) {
	userID := c.GetUint(middleware.CtxUserID)

	var convs []models.DirectConversation
	dc.db.
		Where("user1_id = ? OR user2_id = ?", userID, userID).
		Preload("User1").
		Preload("User2").
		Order("last_message_at IS NULL, last_message_at DESC").
		Find(&convs)

	// Return unread count from current user's perspective
	type ConvResponse struct {
		models.DirectConversation
		UnreadMe int `json:"unread_me"`  // ← int to match model
	}
	result := make([]ConvResponse, len(convs))
	for i, conv := range convs {
		unread := conv.UnreadUser1
		if userID == conv.User2ID {
			unread = conv.UnreadUser2
		}
		result[i] = ConvResponse{
			DirectConversation: conv,
			UnreadMe:           unread,
		}
	}

	c.JSON(http.StatusOK, result)
}

// GetDirectMessages — get paginated messages, auto-marks as read
// GET /api/direct/conversations/:id/messages?page=1&limit=50
func (dc *DirectChatController) GetDirectMessages(c *gin.Context) {
	userID := c.GetUint(middleware.CtxUserID)
	convID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid conversation ID"})
		return
	}

	page, _  := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset   := (page - 1) * limit

	var conv models.DirectConversation
	if err := dc.db.Preload("User1").Preload("User2").First(&conv, convID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Conversation not found"})
		return
	}
	if conv.User1ID != userID && conv.User2ID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	var total int64
	dc.db.Model(&models.DirectMessage{}).
		Where("conversation_id = ?", convID).
		Count(&total)

	var messages []models.DirectMessage
	dc.db.Preload("Sender").
		Where("conversation_id = ?", convID).
		Order("created_at ASC").
		Limit(limit).Offset(offset).
		Find(&messages)

	// Auto mark as read
	dc.db.Model(&models.DirectMessage{}).
		Where("conversation_id = ? AND sender_id != ? AND is_read = false", convID, userID).
		Update("is_read", true)

	if conv.User1ID == userID {
		dc.db.Model(&conv).Update("unread_user1", 0)
	} else {
		dc.db.Model(&conv).Update("unread_user2", 0)
	}

	c.JSON(http.StatusOK, gin.H{
		"conversation": conv,
		"messages":     messages,
		"meta": gin.H{
			"total_items": total,
			"page":        page,
			"limit":       limit,
			"has_next":    int64(offset+limit) < total,
			"has_prev":    page > 1,
		},
	})
}

// ─── REST: Admin ──────────────────────────────────────────────────────────────

// AdminGetDirectConversations — list all direct conversations (paginated + search)
// GET /admin/direct-conversations
func (dc *DirectChatController) AdminGetDirectConversations(c *gin.Context) {
	page, _  := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset   := (page - 1) * limit

	query := dc.db.Model(&models.DirectConversation{}).
		Preload("User1").Preload("User2").
		Order("last_message_at IS NULL, last_message_at DESC")

	if q := c.Query("search"); q != "" {
		like := "%" + q + "%"
		query = query.
			Joins("JOIN users u1 ON u1.id = direct_conversations.user1_id").
			Joins("JOIN users u2 ON u2.id = direct_conversations.user2_id").
			Where("u1.name LIKE ? OR u2.name LIKE ? OR u1.email LIKE ? OR u2.email LIKE ?",
				like, like, like, like)
	}

	if userID := c.Query("user_id"); userID != "" {
		query = query.Where(
			"direct_conversations.user1_id = ? OR direct_conversations.user2_id = ?",
			userID, userID,
		)
	}

	var total int64
	query.Count(&total)

	var convs []models.DirectConversation
	query.Limit(limit).Offset(offset).Find(&convs)

	c.JSON(http.StatusOK, gin.H{
		"data": convs,
		"meta": gin.H{
			"total_items": total,
			"page":        page,
			"limit":       limit,
			"has_next":    int64(offset+limit) < total,
			"has_prev":    page > 1,
		},
	})
}

// AdminGetDirectMessages — view messages in any conversation
// GET /admin/direct-conversations/:id/messages
func (dc *DirectChatController) AdminGetDirectMessages(c *gin.Context) {
	convID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	var conv models.DirectConversation
	if err := dc.db.Preload("User1").Preload("User2").First(&conv, convID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Conversation not found"})
		return
	}

	var messages []models.DirectMessage
	dc.db.Preload("Sender").
		Where("conversation_id = ?", convID).
		Order("created_at ASC").
		Find(&messages)

	c.JSON(http.StatusOK, gin.H{
		"conversation": conv,
		"messages":     messages,
	})
}

// AdminDeleteDirectConversation — hard delete conversation + messages
// DELETE /admin/direct-conversations/:id
func (dc *DirectChatController) AdminDeleteDirectConversation(c *gin.Context) {
	convID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}
	dc.db.Where("conversation_id = ?", convID).Delete(&models.DirectMessage{})
	dc.db.Delete(&models.DirectConversation{}, convID)
	c.JSON(http.StatusOK, gin.H{"message": "Direct conversation deleted"})
}

func (dc *DirectChatController) OnlineUsers(c *gin.Context) {
	ids := dc.hub.OnlineUsers()
	c.JSON(http.StatusOK, gin.H{"online_user_ids": ids, "count": len(ids)})
}