package services

import (
	"encoding/json"
	"fmt"
	"kapi/models"
	"log"
)

type HubService struct {
    hub *models.Hub
}

func NewHubService() *HubService {
    hub := models.NewHub()
    service := &HubService{hub: hub}
    
    go service.Run()
    
    return service
}

func (h *HubService) GetHub() *models.Hub {
    return h.hub
}

func (h *HubService) Run() {
    for {
        select {
        case client := <-h.hub.Register:
            h.registerClient(client)
            
        case client := <-h.hub.Unregister:
            h.unregisterClient(client)
            
        case message := <-h.hub.Broadcast:
            h.broadcastToAll(message)
        }
    }
}

func (h *HubService) registerClient(client *models.Client) {
    h.hub.Clients[client] = true
    if h.hub.UserClients[client.UserID] == nil {
        h.hub.UserClients[client.UserID] = []*models.Client{}
    }
    h.hub.UserClients[client.UserID] = append(h.hub.UserClients[client.UserID], client)
}

func (h *HubService) unregisterClient(client *models.Client) {
    if _, ok := h.hub.Clients[client]; ok {
        delete(h.hub.Clients, client)
        close(client.Send)
        
        if clients, exists := h.hub.UserClients[client.UserID]; exists {
            for i, c := range clients {
                if c == client {
                    h.hub.UserClients[client.UserID] = append(clients[:i], clients[i+1:]...)
                    break
                }
            }
            if len(h.hub.UserClients[client.UserID]) == 0 {
                delete(h.hub.UserClients, client.UserID)
            }
        }
        log.Printf("Client unregistered for user: %s", client.UserID)
    }
}

func (h *HubService) broadcastToAll(message []byte) {
    for client := range h.hub.Clients {
        select {
        case client.Send <- message:
        default:
            close(client.Send)
            delete(h.hub.Clients, client)
        }
    }
}

func (h *HubService) BroadcastToUser(userID uint, messageType string, data interface{}) {
    userIDStr := fmt.Sprintf("%d", userID)
    wsMessage := models.WSMessage{
        Type: messageType,
        Data: data,
    }
    
    messageBytes, err := json.Marshal(wsMessage)
    if err != nil {
        log.Printf("Error marshaling WebSocket message: %v", err)
        return
    }
    
    if clients, exists := h.hub.UserClients[userIDStr]; exists {
        for _, client := range clients {
            select {
            case client.Send <- messageBytes:
            default:
                close(client.Send)
                delete(h.hub.Clients, client)
            }
        }
    }
}