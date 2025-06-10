package services

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"kapi/models"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"
)

type ChatService struct {
	db             *gorm.DB
	openRouterKey  string
	openRouterURL  string
}

func NewChatService(db *gorm.DB, openRouterKey string) *ChatService {
	return &ChatService{
		db:            db,
		openRouterKey: openRouterKey,
		openRouterURL: "https://openrouter.ai/api/v1/chat/completions",
	}
}

func (cs *ChatService) generateChatTitle(content string) string {
	if len(content) > 50 {
		return content[:47] + "..."
	}
	return content
}

type OpenRouterMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OpenRouterRequest struct {
	Model    string               `json:"model"`
	Messages []OpenRouterMessage  `json:"messages"`
	Stream   bool                 `json:"stream"`
}

type OpenRouterStreamResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role    string `json:"role,omitempty"`
			Content string `json:"content,omitempty"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}



func (cs *ChatService) GetUserChats(userID uint, limit, offset int) ([]models.ChatResponse, error) {
	var chats []models.Chat

	query := cs.db.Where("user_id = ?", userID).
		Order("updated_at DESC")

	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}

	if err := query.Find(&chats).Error; err != nil {
		return nil, err
	}

	var responses []models.ChatResponse
	for _, chat := range chats {
		response := models.ChatResponse{
			ID:        chat.ID,
			UserID:    chat.UserID,
			Title:     chat.Title,
			IsActive:  chat.IsActive,
			CreatedAt: chat.CreatedAt,
			UpdatedAt: chat.UpdatedAt,
		}

		cs.db.Model(&models.Message{}).Where("chat_id = ?", chat.ID).Count(&response.MessageCount)

		var lastMessage models.Message
		if err := cs.db.Where("chat_id = ?", chat.ID).
			Order("created_at DESC").
			First(&lastMessage).Error; err == nil {
			response.LastMessage = &lastMessage
		}

		responses = append(responses, response)
	}

	return responses, nil
}

func (cs *ChatService) GetChatByID(chatID, userID uint) (*models.ChatWithMessagesResponse, error) {
	var chat models.Chat
	if err := cs.db.Where("id = ? AND user_id = ?", chatID, userID).
		First(&chat).Error; err != nil {
		return nil, err
	}

	var messages []models.Message
	if err := cs.db.Where("chat_id = ?", chatID).
		Order("created_at ASC").
		Find(&messages).Error; err != nil {
		return nil, err
	}

	response := &models.ChatWithMessagesResponse{
		ChatResponse: models.ChatResponse{
			ID:        chat.ID,
			UserID:    chat.UserID,
			Title:     chat.Title,
			IsActive:  chat.IsActive,
			CreatedAt: chat.CreatedAt,
			UpdatedAt: chat.UpdatedAt,
		},
		Messages: messages,
	}

	return response, nil
}

func (cs *ChatService) UpdateChat(chatID, userID uint, req *models.UpdateChatRequest) (*models.Chat, error) {
	var chat models.Chat
	if err := cs.db.Where("id = ? AND user_id = ?", chatID, userID).
		First(&chat).Error; err != nil {
		return nil, err
	}

	updates := map[string]interface{}{}
	if req.Title != "" {
		updates["title"] = req.Title
	}
	if req.IsActive != nil {
		updates["is_active"] = *req.IsActive
	}

	if len(updates) > 0 {
		if err := cs.db.Model(&chat).Updates(updates).Error; err != nil {
			return nil, err
		}
	}

	return &chat, nil
}

func (cs *ChatService) DeleteChat(chatID, userID uint) error {
	result := cs.db.Where("id = ? AND user_id = ?", chatID, userID).
		Delete(&models.Chat{})

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return errors.New("chat not found")
	}

	return nil
}

func (cs *ChatService) CreateChatWithMessage(userID uint, req *models.CreateMessageRequest, responseChan chan<- string, errorChan chan<- error, chatIDChan chan<- uint) {
	defer close(responseChan)
	defer close(errorChan)
	defer close(chatIDChan)

	title := cs.generateChatTitle(req.Content)
	chat := &models.Chat{
		UserID:   userID,
		Title:    title,
		IsActive: true,
	}

	if err := cs.db.Create(chat).Error; err != nil {
		errorChan <- err
		return
	}

	chatIDChan <- chat.ID

	userMessage := &models.Message{
		ChatID:  chat.ID,
		Role:    req.Role,
		Content: req.Content,
		Model:   req.Model,
	}

	if err := cs.db.Create(userMessage).Error; err != nil {
		errorChan <- err
		return
	}

	cs.db.Model(chat).Update("updated_at", userMessage.CreatedAt)

	if req.Role == "user" {
		cs.streamLLMResponse(chat.ID, userID, req.Model, responseChan, errorChan)
	}
}

func (cs *ChatService) CreateMessage(chatID, userID uint, req *models.CreateMessageRequest) (*models.Message, error) {
	var chat models.Chat
	if err := cs.db.Where("id = ? AND user_id = ?", chatID, userID).
		First(&chat).Error; err != nil {
		return nil, errors.New("chat not found or access denied")
	}

	userMessage := &models.Message{
		ChatID:  chatID,
		Role:    req.Role,
		Content: req.Content,
		Model:   req.Model,
	}

	if err := cs.db.Create(userMessage).Error; err != nil {
		return nil, err
	}

	cs.db.Model(&chat).Update("updated_at", userMessage.CreatedAt)

	return userMessage, nil
}

func (cs *ChatService) streamLLMResponse(chatID, userID uint, model string, responseChan chan<- string, errorChan chan<- error) {

	var chat models.Chat
	if err := cs.db.Where("id = ? AND user_id = ?", chatID, userID).
		First(&chat).Error; err != nil {
		errorChan <- errors.New("chat not found or access denied")
		return
	}

	var messages []models.Message
	if err := cs.db.Where("chat_id = ?", chatID).
		Order("created_at ASC").
		Find(&messages).Error; err != nil {
		errorChan <- err
		return
	}

	var openRouterMessages []OpenRouterMessage
	for _, msg := range messages {
		openRouterMessages = append(openRouterMessages, OpenRouterMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	if model == "" {
		model = "google/gemini-2.0-flash-lite-001"
	}

	openRouterReq := OpenRouterRequest{
		Model:    model,
		Messages: openRouterMessages,
		Stream:   true,
	}

	jsonData, err := json.Marshal(openRouterReq)
	if err != nil {
		errorChan <- err
		return
	}

	req, err := http.NewRequest("POST", cs.openRouterURL, bytes.NewBuffer(jsonData))
	if err != nil {
		errorChan <- err
		return
	}

	fmt.Println("Sending request to OpenRouter with model: " + model)
	fmt.Println("Sending request to OpenRouter with key: " + cs.openRouterKey)
	fmt.Println("Sending request to OpenRouter with url: " + cs.openRouterURL)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cs.openRouterKey)
	req.Header.Set("HTTP-Referer", "http://localhost:8080")
	req.Header.Set("X-Title", "Your App Name")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		errorChan <- err
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		errorChan <- fmt.Errorf("OpenRouter API error: %s", string(body))
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	var fullResponse strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		if data == "[DONE]" {
			break
		}

		var streamResp OpenRouterStreamResponse
		if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
			continue
		}

		if len(streamResp.Choices) > 0 && streamResp.Choices[0].Delta.Content != "" {
			content := streamResp.Choices[0].Delta.Content
			fullResponse.WriteString(content)
			responseChan <- content
		}
	}

	if err := scanner.Err(); err != nil {
		errorChan <- err
		return
	}

	assistantMessage := &models.Message{
		ChatID:  chatID,
		Role:    "assistant",
		Content: fullResponse.String(),
		Model:   model,
	}

	if err := cs.db.Create(assistantMessage).Error; err != nil {
		errorChan <- err
		return
	}

	cs.db.Model(&chat).Update("updated_at", assistantMessage.CreatedAt)
}

func (cs *ChatService) StreamLLMResponse(chatID, userID uint, model string, responseChan chan<- string, errorChan chan<- error) {
	defer close(responseChan)
	defer close(errorChan)
	cs.streamLLMResponse(chatID, userID, model, responseChan, errorChan)
}

func (cs *ChatService) GetChatMessages(chatID, userID uint, limit, offset int) ([]models.Message, error) {
	var chat models.Chat
	if err := cs.db.Where("id = ? AND user_id = ?", chatID, userID).
		First(&chat).Error; err != nil {
		return nil, errors.New("chat not found or access denied")
	}

	var messages []models.Message
	query := cs.db.Where("chat_id = ?", chatID).
		Order("created_at ASC")

	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}

	if err := query.Find(&messages).Error; err != nil {
		return nil, err
	}

	return messages, nil
}

func (cs *ChatService) UpdateMessage(messageID, chatID, userID uint, content string) (*models.Message, error) {
	var chat models.Chat
	if err := cs.db.Where("id = ? AND user_id = ?", chatID, userID).
		First(&chat).Error; err != nil {
		return nil, errors.New("chat not found or access denied")
	}

	var message models.Message
	if err := cs.db.Where("id = ? AND chat_id = ?", messageID, chatID).
		First(&message).Error; err != nil {
		return nil, errors.New("message not found")
	}

	message.Content = content
	if err := cs.db.Save(&message).Error; err != nil {
		return nil, err
	}

	return &message, nil
}

func (cs *ChatService) DeleteMessage(messageID, chatID, userID uint) error {
	var chat models.Chat
	if err := cs.db.Where("id = ? AND user_id = ?", chatID, userID).
		First(&chat).Error; err != nil {
		return errors.New("chat not found or access denied")
	}

	result := cs.db.Where("id = ? AND chat_id = ?", messageID, chatID).
		Delete(&models.Message{})

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return errors.New("message not found")
	}

	return nil
}

func (cs *ChatService) CreateChatWithMessageSync(userID uint, req *models.CreateMessageRequest) (*models.ChatWithMessagesResponse, error) {
	title := cs.generateChatTitle(req.Content)
	chat := &models.Chat{
		UserID:   userID,
		Title:    title,
		IsActive: true,
	}

	if err := cs.db.Create(chat).Error; err != nil {
		return nil, err
	}

	userMessage := &models.Message{
		ChatID:  chat.ID,
		Role:    req.Role,
		Content: req.Content,
		Model:   req.Model,
	}

	if err := cs.db.Create(userMessage).Error; err != nil {
		return nil, err
	}

	cs.db.Model(chat).Update("updated_at", userMessage.CreatedAt)

	response := &models.ChatWithMessagesResponse{
		ChatResponse: models.ChatResponse{
			ID:           chat.ID,
			UserID:       chat.UserID,
			Title:        chat.Title,
			IsActive:     chat.IsActive,
			CreatedAt:    chat.CreatedAt,
			UpdatedAt:    chat.UpdatedAt,
			MessageCount: 1,
			LastMessage:  userMessage,
		},
		Messages: []models.Message{*userMessage},
	}

	return response, nil
}
