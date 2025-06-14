package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"kapi/models"
	"kapi/services"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// TODO: Configure this properly for production
		return true
	},
}

type WebSocketHandler struct {
	hubService *services.HubService
}

func NewWebSocketHandler(hubService *services.HubService) *WebSocketHandler {
	return &WebSocketHandler{
		hubService: hubService,
	}
}

func (wh *WebSocketHandler) HandleWebSocket(c *gin.Context) {
	log.Println("WebSocket connection attempt received")

	userID, exists := c.Get("user_id")
	if !exists {
		log.Println("No user_id found in context")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	log.Printf("User %v attempting WebSocket connection", userID)
	log.Printf("User ID type: %T", userID)

	var userIDStr string
	switch v := userID.(type) {
	case uint:
		userIDStr = fmt.Sprintf("%d", v)
		log.Printf("Converted uint %d to string %s", v, userIDStr)
	case int:
		userIDStr = fmt.Sprintf("%d", v)
		log.Printf("Converted int %d to string %s", v, userIDStr)
	case string:
		userIDStr = v
		log.Printf("Using string userID: %s", userIDStr)
	case float64:
		userIDStr = fmt.Sprintf("%.0f", v)
		log.Printf("Converted float64 %.0f to string %s", v, userIDStr)
	default:
		log.Printf("Unexpected user ID type: %T with value: %v", userID, userID)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid user ID type"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Failed to upgrade connection: %v", err)
		return
	}

	log.Printf("WebSocket connection upgraded successfully for user: %s", userIDStr)

	client := models.NewClient(wh.hubService.GetHub(), conn, userIDStr)

	client.Hub.Register <- client
	go wh.writePump(client)
	go wh.readPump(client)
}

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512
)

func (wh *WebSocketHandler) readPump(client *models.Client) {
	defer func() {
		log.Printf("Client %s (user %s) disconnecting", client.ID, client.UserID)
		client.Hub.Unregister <- client
		client.Conn.Close()
	}()

	client.Conn.SetReadLimit(maxMessageSize)
	client.Conn.SetReadDeadline(time.Now().Add(pongWait))
	client.Conn.SetPongHandler(func(string) error {
		client.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := client.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Unexpected close error for client %s: %v", client.ID, err)
			}
			break
		}

		log.Printf("Received message from client %s (user %s): %s", client.ID, client.UserID, string(message))

		var wsMessage models.WSMessage
		err = json.Unmarshal(message, &wsMessage)
		if err != nil {
			log.Printf("Error unmarshaling WebSocket message from client %s: %v", client.ID, err)
			continue
		}

		switch wsMessage.Type {
		case "client_connect":
			log.Printf("Client %s (user %s) sent 'client_connect' message.", client.ID, client.UserID)

			responseMessage := models.WSMessage{
				Type: "client_connected",
				Data: map[string]string{"client_id": client.ID},
			}

			responseBytes, err := json.Marshal(responseMessage)
			if err != nil {
				log.Printf("Error marshaling 'client_connected' response for client %s: %v", client.ID, err)
				continue
			}

			select {
			case client.Send <- responseBytes:
				log.Printf("'client_connected' message sent to client %s with ID: %s", client.ID, client.ID)
			default:
				log.Printf("Failed to send 'client_connected' message to client %s, closing connection.", client.ID)
				close(client.Send)
				client.Hub.Unregister <- client
				client.Conn.Close()
				return
			}

		// TODO: handle other message types
		// case "chat_message":
		//     // Handle chat messages
		// case "sync_event":
		//     // Handle sync events
		default:
			log.Printf("Unknown message type '%s' received from client %s (user %s).", wsMessage.Type, client.ID, client.UserID)
		}
	}
}

func (wh *WebSocketHandler) writePump(client *models.Client) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		log.Printf("Write pump closing for client %s (user %s)", client.ID, client.UserID)
		ticker.Stop()
		client.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-client.Send:
			client.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				client.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := client.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				log.Printf("Error getting writer for client %s: %v", client.ID, err)
				return
			}
			w.Write(message)

			// Write any additional queued messages
			n := len(client.Send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-client.Send)
			}

			if err := w.Close(); err != nil {
				log.Printf("Error closing writer for client %s: %v", client.ID, err)
				return
			}

			log.Printf("Message sent to client %s (user %s)", client.ID, client.UserID)

		case <-ticker.C:
			client.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := client.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("Error sending ping to client %s: %v", client.ID, err)
				return
			}
		}
	}
}
