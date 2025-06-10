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
}

func NewChatController(db *gorm.DB, cfg *config.Config) *ChatController {
	return &ChatController{
		db:          db,
		chatService: services.NewChatService(db, cfg.OpenRouterKey),
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
// @Summary Create a new chat with an initial user message
// @Description Creates a new chat and saves the user's first message. The response includes the complete chat data and the initial message.
// @Tags Chat
// @Accept json
// @Produce json
// @Param request body models.CreateDirectMessageRequest true "Request body for creating a direct message"
// @Success 200 {object} object{success=bool,data=models.ChatWithMessagesResponse} "Successfully created chat and initial message"
// @Failure 400 {object} object{error=string} "Invalid request payload"
// @Failure 401 {object} object{error=string} "Unauthorized - User not authenticated"
// @Failure 500 {object} object{error=string} "Internal server error"
// @Security BearerAuth
// @Router /chats/direct-message [post]
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

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    chatResponse,
	})
}

// CreateDirectMessageStream streams LLM response for a given chat
// @Summary Stream LLM response for a given chat
// @Description Streams the Large Language Model's response for a specified chat ID. This endpoint is designed for server-sent events (SSE) and will continuously send chunks of the LLM's response.
// @Tags Chat
// @Accept json
// @Produce text/plain
// @Param id path int true "The ID of the chat to stream messages for"
// @Success 200 {string} string "Successful streaming response. Chunks of text will be sent continuously."
// @Failure 400 {object} object{error=string} "Invalid chat ID or request payload"
// @Failure 401 {object} object{error=string} "Unauthorized - User not authenticated"
// @Failure 500 {object} object{error=string} "Internal server error or streaming not supported by the server"
// @Security BearerAuth
// @Router /chats/{id}/stream [post]
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
// @Summary Get user's chats
// @Description Retrieve all chats belonging to the authenticated user with pagination
// @Tags chats
// @Accept json
// @Produce json
// @Param limit query int false "Number of chats to return" default(20)
// @Param offset query int false "Number of chats to skip" default(0)
// @Success 200 {object} map[string]interface{} "Successfully retrieved chats"
// @Failure 401 {object} map[string]interface{} "Unauthorized"
// @Failure 500 {object} map[string]interface{} "Internal server error"
// @Security BearerAuth
// @Router /chats [get]
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
// @Summary Get a specific chat
// @Description Retrieve a specific chat with all its messages
// @Tags chats
// @Accept json
// @Produce json
// @Param id path int true "Chat ID"
// @Success 200 {object} map[string]interface{} "Successfully retrieved chat"
// @Failure 400 {object} map[string]interface{} "Invalid chat ID"
// @Failure 401 {object} map[string]interface{} "Unauthorized"
// @Failure 404 {object} map[string]interface{} "Chat not found"
// @Security BearerAuth
// @Router /chats/{id} [get]
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
// @Summary Update a chat
// @Description Update a chat's title or active status
// @Tags chats
// @Accept json
// @Produce json
// @Param id path int true "Chat ID"
// @Param chat body models.UpdateChatRequest true "Chat update request"
// @Success 200 {object} map[string]interface{} "Successfully updated chat"
// @Failure 400 {object} map[string]interface{} "Invalid request"
// @Failure 401 {object} map[string]interface{} "Unauthorized"
// @Failure 404 {object} map[string]interface{} "Chat not found"
// @Security BearerAuth
// @Router /chats/{id} [put]
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
// @Summary Delete a chat
// @Description Delete a chat and all its associated messages
// @Tags chats
// @Accept json
// @Produce json
// @Param id path int true "Chat ID"
// @Success 200 {object} map[string]interface{} "Successfully deleted chat"
// @Failure 400 {object} map[string]interface{} "Invalid chat ID"
// @Failure 401 {object} map[string]interface{} "Unauthorized"
// @Failure 404 {object} map[string]interface{} "Chat not found"
// @Security BearerAuth
// @Router /chats/{id} [delete]
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
// @Summary Create a message
// @Description Add a new message to a specific chat and get streaming LLM response
// @Tags messages
// @Accept json
// @Produce text/plain
// @Param id path int true "Chat ID"
// @Param message body models.CreateMessageRequest true "Message creation request"
// @Success 200 {string} string "Streaming response"
// @Failure 400 {object} map[string]interface{} "Invalid request"
// @Failure 401 {object} map[string]interface{} "Unauthorized"
// @Failure 404 {object} map[string]interface{} "Chat not found"
// @Failure 500 {object} map[string]interface{} "Internal server error"
// @Security BearerAuth
// @Router /chats/{id}/messages [post]
func (cc *ChatController) CreateMessage(c *gin.Context) {
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

	var req models.CreateMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	message, err := cc.chatService.CreateMessage(uint(chatID), userID, &req)
	if err != nil {
		if err.Error() == "chat not found or access denied" {
			c.JSON(http.StatusNotFound, gin.H{"error": "Chat not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create message"})
		}
		return
	}

	if req.Role == "user" {
		c.Header("Content-Type", "text/plain; charset=utf-8")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Headers", "Cache-Control")

		c.Status(http.StatusOK)

		responseChan := make(chan string, 100)
		errorChan := make(chan error, 1)

		go cc.chatService.StreamLLMResponse(uint(chatID), userID, req.Model, responseChan, errorChan)

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
	} else {
		c.JSON(http.StatusCreated, gin.H{"data": message})
	}
}

// GetChatMessages retrieves all messages for a specific chat
// @Summary Get chat messages
// @Description Retrieve all messages for a specific chat with pagination
// @Tags messages
// @Accept json
// @Produce json
// @Param id path int true "Chat ID"
// @Param limit query int false "Number of messages to return" default(50)
// @Param offset query int false "Number of messages to skip" default(0)
// @Success 200 {object} map[string]interface{} "Successfully retrieved messages"
// @Failure 400 {object} map[string]interface{} "Invalid chat ID"
// @Failure 401 {object} map[string]interface{} "Unauthorized"
// @Failure 404 {object} map[string]interface{} "Chat not found"
// @Failure 500 {object} map[string]interface{} "Internal server error"
// @Security BearerAuth
// @Router /chats/{id}/messages [get]
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
// @Summary Update a message
// @Description Update the content of a specific message
// @Tags messages
// @Accept json
// @Produce json
// @Param id path int true "Chat ID"
// @Param messageId path int true "Message ID"
// @Param content body object{content=string} true "Message content update"
// @Success 200 {object} map[string]interface{} "Successfully updated message"
// @Failure 400 {object} map[string]interface{} "Invalid request"
// @Failure 401 {object} map[string]interface{} "Unauthorized"
// @Failure 404 {object} map[string]interface{} "Message not found"
// @Failure 500 {object} map[string]interface{} "Internal server error"
// @Security BearerAuth
// @Router /chats/{id}/messages/{messageId} [put]
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
// @Summary Delete a message
// @Description Delete a specific message from a chat
// @Tags messages
// @Accept json
// @Produce json
// @Param id path int true "Chat ID"
// @Param messageId path int true "Message ID"
// @Success 200 {object} map[string]interface{} "Successfully deleted message"
// @Failure 400 {object} map[string]interface{} "Invalid request"
// @Failure 401 {object} map[string]interface{} "Unauthorized"
// @Failure 404 {object} map[string]interface{} "Message not found"
// @Failure 500 {object} map[string]interface{} "Internal server error"
// @Security BearerAuth
// @Router /chats/{id}/messages/{messageId} [delete]
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
