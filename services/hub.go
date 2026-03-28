package services

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 4096
)

// WSMessage — JSON envelope exchanged over WebSocket
type WSMessage struct {
	Type           string `json:"type"`
	ID             uint   `json:"id,omitempty"`
	ConversationID uint   `json:"conversation_id,omitempty"`
	SenderID       uint   `json:"sender_id,omitempty"`
	RecipientID    uint   `json:"recipient_id,omitempty"`
	Content        string `json:"content,omitempty"`
	ImageURL       string `json:"image_url,omitempty"`  // ← add
	IsRead         bool   `json:"is_read,omitempty"`
	CreatedAt      string `json:"created_at,omitempty"`
	Error          string `json:"error,omitempty"`
}

// Client — one connected WebSocket session
type Client struct {
	Hub    *Hub
	Conn   *websocket.Conn
	Send   chan []byte
	UserID uint
}

// Hub — manages all active WebSocket connections
type Hub struct {
	clients    map[uint]*Client
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
}

var GlobalHub = NewHub()

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[uint]*Client),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

// Register — add client to hub
func (h *Hub) Register(client *Client) {
	h.register <- client
}

// Run — start event loop (call as goroutine in main.go)
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.UserID] = client
			h.mu.Unlock()
			log.Printf("[WS] User %d connected (%d online)", client.UserID, len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.UserID]; ok {
				delete(h.clients, client.UserID)
				close(client.Send)
			}
			h.mu.Unlock()
			log.Printf("[WS] User %d disconnected (%d online)", client.UserID, len(h.clients))
		}
	}
}

// SendToUser — push a message to a user if they are online
func (h *Hub) SendToUser(userID uint, msg *WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.mu.RLock()
	client, ok := h.clients[userID]
	h.mu.RUnlock()

	if ok {
		select {
		case client.Send <- data:
		default:
			h.mu.Lock()
			delete(h.clients, userID)
			close(client.Send)
			h.mu.Unlock()
		}
	}
}

// IsOnline — true if user has an active connection
func (h *Hub) IsOnline(userID uint) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.clients[userID]
	return ok
}

// OnlineUsers — list of currently connected user IDs
func (h *Hub) OnlineUsers() []uint {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ids := make([]uint, 0, len(h.clients))
	for id := range h.clients {
		ids = append(ids, id)
	}
	return ids
}

// ReadPump — read loop for a client connection
func (c *Client) ReadPump(onMessage func(*Client, []byte)) {
	defer func() {
		c.Hub.unregister <- c
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(maxMessageSize)
	c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[WS] Read error user %d: %v", c.UserID, err)
			}
			break
		}
		onMessage(c, message)
	}
}

// WritePump — write loop for a client connection
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}