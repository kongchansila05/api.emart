package controllers

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"project-api/services"
	"project-api/utils"
)

// ServeWSCombined — WebSocket endpoint for Direct Chat only
//   - Direct chat: type = "direct_message" | "direct_read" | "direct_typing"
func ServeWSCombined(
	c *gin.Context,
	directCtrl *DirectChatController,
	hub *services.Hub,
) {
	tokenStr := c.Query("token")
	if tokenStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "token required"})
		return
	}
	claims, err := utils.ParseToken(tokenStr)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}

	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(r *http.Request) bool { return true },
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[WS] upgrade error: %v", err)
		return
	}

	client := &services.Client{
		Hub:    hub,
		Conn:   conn,
		Send:   make(chan []byte, 256),
		UserID: claims.UserID,
	}

	hub.Register(client)
	go client.WritePump()

	// ── Dispatch handler — routes by message type ─────────────────────────────
	go client.ReadPump(func(cl *services.Client, raw []byte) {
		var peek struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &peek); err != nil {
			data, _ := json.Marshal(&services.WSMessage{Type: "error", Error: "invalid JSON"})
			select {
			case cl.Send <- data:
			default:
			}
			return
		}

		switch peek.Type {
		case "direct_message", "direct_read", "direct_typing":
			directCtrl.HandleWSMessage(cl, raw)

		default:
			data, _ := json.Marshal(&services.WSMessage{
				Type:  "error",
				Error: "unknown type: " + peek.Type,
			})
			select {
			case cl.Send <- data:
			default:
			}
		}
	})
}