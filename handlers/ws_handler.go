package handlers

import (
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

    client := &models.Client{
        Hub:    wh.hubService.GetHub(),
        Conn:   conn,
        Send:   make(chan []byte, 256),
        UserID: userIDStr,
    }

    client.Hub.Register <- client
    go wh.writePump(client)
    go wh.readPump(client)
}
const (
    writeWait = 10 * time.Second
    pongWait = 60 * time.Second
    pingPeriod = (pongWait * 9) / 10
    maxMessageSize = 512
)

func (wh *WebSocketHandler) readPump(client *models.Client) {
    defer func() {
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
        _, _, err := client.Conn.ReadMessage()
        if err != nil {
            if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
                log.Printf("error: %v", err)
            }
            break
        }
    }
}

func (wh *WebSocketHandler) writePump(client *models.Client) {
    ticker := time.NewTicker(pingPeriod)
    defer func() {
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
                return
            }
            w.Write(message)

            n := len(client.Send)
            for i := 0; i < n; i++ {
                w.Write([]byte{'\n'})
                w.Write(<-client.Send)
            }

            if err := w.Close(); err != nil {
                return
            }
            
        case <-ticker.C:
            client.Conn.SetWriteDeadline(time.Now().Add(writeWait))
            if err := client.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
                return
            }
        }
    }
}