package controllers

import (
	"kapi/config"
	"kapi/models"
	"kapi/services"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ChatController struct {
	db          *gorm.DB
	cfg         *config.Config
	chatService *services.ChatService
	hubService  *services.HubService
}

func NewChatController(db *gorm.DB, cfg *config.Config, hubService *services.HubService) *ChatController {
	return &ChatController{
		db:          db,
		chatService: services.NewChatService(db, cfg.OpenRouterKey),
		hubService:  hubService,
	}
}

func (cc *ChatController) getUserID(c *gin.Context) (uint, bool) {
	userID, exists := c.Get("user_id")
	if !exists {
		return 0, false
	}
	if id, ok := userID.(uint); ok {
		return id, true
	}
	return 0, false
}

// CreateDirectMessage creates a new chat with an initial user message (synchronous)
func (cc *ChatController) CreateDirectMessage(c *gin.Context) {
	userID, exists := cc.getUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	var req models.CreateDirectMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	messageReq := &models.CreateMessageRequest{
		Content: req.Content,
		Role:    "user",
		Model:   req.Model,
	}

	chatResponse, err := cc.chatService.CreateChatWithMessageSync(userID, messageReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create chat: " + err.Error()})
		return
	}

	cc.hubService.BroadcastToUserExceptByClientID(userID, "chat_created", chatResponse, req.ClientID)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    chatResponse,
	})
}

// CreateDirectMessageStream streams LLM response for a given chat
func (cc *ChatController) CreateDirectMessageStream(c *gin.Context) {
	userID, exists := cc.getUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	chatID := c.Param("id")
	chatIDUint, err := strconv.ParseUint(chatID, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid chat ID"})
		return
	}

	var req struct {
		Model string `json:"model"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Headers", "Cache-Control")
	c.Status(http.StatusOK)

	responseChan := make(chan string, 100)
	errorChan := make(chan error, 1)

	go cc.chatService.StreamLLMResponse(uint(chatIDUint), userID, req.Model, responseChan, errorChan)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Streaming not supported"})
		return
	}

	for {
		select {
		case content, ok := <-responseChan:
			if !ok {
				return
			}
			c.Writer.WriteString(content)
			flusher.Flush()
		case err := <-errorChan:
			if err != nil {
				c.Writer.WriteString("\n\nError: " + err.Error())
				flusher.Flush()
				return
			}
		}
	}
}

// GetUserChats retrieves all chats for the authenticated user
func (cc *ChatController) GetUserChats(c *gin.Context) {
	userID, exists := cc.getUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	chats, err := cc.chatService.GetUserChats(userID, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch chats"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": chats,
		"pagination": gin.H{
			"limit":  limit,
			"offset": offset,
			"count":  len(chats),
		},
	})
}

// GetChat retrieves a specific chat with messages
func (cc *ChatController) GetChat(c *gin.Context) {
	userID, exists := cc.getUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	chatID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid chat ID"})
		return
	}

	chat, err := cc.chatService.GetChatByID(uint(chatID), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Chat not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": chat})
}

// UpdateChat updates a chat's properties
func (cc *ChatController) UpdateChat(c *gin.Context) {
	userID, exists := cc.getUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	chatID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid chat ID"})
		return
	}

	var req models.UpdateChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	chat, err := cc.chatService.UpdateChat(uint(chatID), userID, &req)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Chat not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": chat})
}

// DeleteChat deletes a chat and all its messages
func (cc *ChatController) DeleteChat(c *gin.Context) {
	userID, exists := cc.getUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	chatID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid chat ID"})
		return
	}

	if err := cc.chatService.DeleteChat(uint(chatID), userID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Chat not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Chat deleted successfully"})
}

// CreateMessage adds a new message to a chat and streams LLM response
func (cc *ChatController) CreateMessage(c *gin.Context) {
	userID, exists := cc.getUserID(c) // Assuming getUserID is a helper function
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	chatID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid chat ID"})
		return
	}

	var req models.CreateMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userMessage, err := cc.chatService.CreateMessage(uint(chatID), userID, &req)
	if err != nil {
		if err.Error() == "chat not found or access denied" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Chat not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create message: " + err.Error()})
		}
		return
	}

	cc.hubService.BroadcastToUserExceptByClientID(userID, "message_created", userMessage, req.ClientID)

	if req.Role == "user" {
		c.Header("Content-Type", "text/plain; charset=utf-8")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Headers", "Cache-Control")

		c.Status(http.StatusOK)

		responseChan := make(chan string, 100)
		errorChan := make(chan error, 1)
		doneChan := make(chan struct{})

		go func() {
			defer close(doneChan)
			llmMessage, err := cc.chatService.StreamLLMResponse(uint(chatID), userID, req.Model, responseChan, errorChan)
			if err != nil {
				return
			}
			if llmMessage != nil {
				cc.hubService.BroadcastToUserExceptByClientID(userID, "message_created", llmMessage, req.ClientID)
			}
		}()

		flusher, ok := c.Writer.(http.Flusher)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Streaming not supported"})
			return
		}

		for {
			select {
			case content, ok := <-responseChan:
				if !ok {
					return
				}
				c.Writer.WriteString(content)
				flusher.Flush()

			case err := <-errorChan:
				if err != nil {
					c.Writer.WriteString("\n\nError: " + err.Error())
					flusher.Flush()
					return
				}
			case <-doneChan:
				return
			case <-c.Request.Context().Done():
				return
			}
		}
	} else {
		c.JSON(http.StatusCreated, gin.H{"data": userMessage})
	}
}

// GetChatMessages retrieves all messages for a specific chat
func (cc *ChatController) GetChatMessages(c *gin.Context) {
	userID, exists := cc.getUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	chatID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid chat ID"})
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	messages, err := cc.chatService.GetChatMessages(uint(chatID), userID, limit, offset)
	if err != nil {
		if err.Error() == "chat not found or access denied" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Chat not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch messages"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": messages,
		"pagination": gin.H{
			"limit":  limit,
			"offset": offset,
			"count":  len(messages),
		},
	})
}

// UpdateMessage updates a message's content
func (cc *ChatController) UpdateMessage(c *gin.Context) {
	userID, exists := cc.getUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	chatID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid chat ID"})
		return
	}

	messageID, err := strconv.ParseUint(c.Param("messageId"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid message ID"})
		return
	}

	var req struct {
		Content string `json:"content" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	message, err := cc.chatService.UpdateMessage(uint(messageID), uint(chatID), userID, req.Content)
	if err != nil {
		if err.Error() == "chat not found or access denied" || err.Error() == "message not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update message"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": message})
}

// DeleteMessage deletes a specific message
func (cc *ChatController) DeleteMessage(c *gin.Context) {
	userID, exists := cc.getUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
		return
	}

	chatID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid chat ID"})
		return
	}

	messageID, err := strconv.ParseUint(c.Param("messageId"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid message ID"})
		return
	}

	if err := cc.chatService.DeleteMessage(uint(messageID), uint(chatID), userID); err != nil {
		if err.Error() == "chat not found or access denied" || err.Error() == "message not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete message"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Message deleted successfully"})
}
