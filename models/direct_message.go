package models

import (
	"time"

	"gorm.io/gorm"
)

// DirectConversation — private chat between any two clients (not linked to a post)
type DirectConversation struct {
	gorm.Model
	User1ID       uint       `json:"user1_id"                               gorm:"index;not null"`
	User1         User       `json:"user1"          gorm:"foreignKey:User1ID"`
	User2ID       uint       `json:"user2_id"                               gorm:"index;not null"`
	User2         User       `json:"user2"          gorm:"foreignKey:User2ID"`
	LastMessage   string     `json:"last_message"`
	LastMessageAt *time.Time `json:"last_message_at"`
	UnreadUser1   int        `json:"unread_user1"` // unread for user1
	UnreadUser2   int        `json:"unread_user2"` // unread for user2
}

func (DirectConversation) TableName() string { return "direct_conversations" }

// DirectMessage — a message in a direct conversation
type DirectMessage struct {
	gorm.Model
	ConversationID uint   `json:"conversation_id"  gorm:"index;not null"`
	SenderID       uint   `json:"sender_id"        gorm:"index;not null"`
	Sender         User   `json:"sender"           gorm:"foreignKey:SenderID"`
	Content        string `json:"content"          gorm:"not null"`
	ImageURL       string `json:"image_url"        gorm:"default:''"`  // ← add
	IsRead         bool   `json:"is_read"          gorm:"default:false"`
}

func (DirectMessage) TableName() string { return "direct_messages" }