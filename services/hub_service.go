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
	log.Printf("Client %s registered for user: %s", client.ID, client.UserID)
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
		log.Printf("Client %s unregistered for user: %s", client.ID, client.UserID)
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

func (h *HubService) GetClientByID(clientID string) *models.Client {
	log.Printf("Looking for client with ID: '%s'", clientID)

	for client := range h.hub.Clients {
		log.Printf("Checking client: ID='%s', UserID='%s'", client.ID, client.UserID)
		if client.ID == clientID {
			log.Printf("Found matching client: %s", clientID)
			return client
		}
	}

	log.Printf("Client '%s' not found", clientID)
	return nil
}

func (h *HubService) BroadcastToUserExceptByClientID(userID uint, messageType string, data interface{}, originClientID string) {
	log.Printf("BroadcastToUserExceptByClientID called: userID=%d, messageType=%s, originClientID='%s'", userID, messageType, originClientID)

	if originClientID == "" {
		log.Printf("No client ID provided, broadcasting to all")
		h.BroadcastToUser(userID, messageType, data)
		return
	}

	originClient := h.GetClientByID(originClientID)
	if originClient == nil {
		log.Printf("Origin client '%s' not found, broadcasting to all user clients", originClientID)
		h.BroadcastToUser(userID, messageType, data)
		return
	}

	log.Printf("Found origin client '%s', broadcasting to others", originClientID)
	h.BroadcastToUserExcept(userID, messageType, data, originClient)
}

func (h *HubService) BroadcastToUserExcept(userID uint, messageType string, data interface{}, originClient *models.Client) {
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
		log.Printf("Broadcasting to %d clients for user %s (excluding origin %s)", len(clients)-1, userIDStr, originClient.ID)

		for _, client := range clients {
			if client == originClient {
				log.Printf("Skipping origin client %s for user %s", originClient.ID, userIDStr)
				continue
			}

			select {
			case client.Send <- messageBytes:
				log.Printf("Message sent to client %s for user %s", client.ID, userIDStr)
			default:
				close(client.Send)
				delete(h.hub.Clients, client)
				log.Printf("Failed to send message, client %s removed for user %s", client.ID, userIDStr)
			}
		}
	} else {
		log.Printf("No clients found for user %s", userIDStr)
	}
}
