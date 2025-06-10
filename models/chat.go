package models

import (
	"time"

	"gorm.io/gorm"
)

type Chat struct {
	ID          uint           `json:"id" gorm:"primaryKey"`
	UserID      uint           `json:"user_id" gorm:"not null;index"`
	Title       string         `json:"title" gorm:"not null"`
	IsActive    bool           `json:"is_active" gorm:"default:true"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
	User        User           `json:"user,omitempty" gorm:"foreignKey:UserID"`
	Messages    []Message      `json:"messages,omitempty" gorm:"foreignKey:ChatID;constraint:OnDelete:CASCADE"`
}

type Message struct {
	ID        uint           `json:"id" gorm:"primaryKey"`
	ChatID    uint           `json:"chat_id" gorm:"not null;index"`
	Role      string         `json:"role" gorm:"not null"` // "user" or "assistant"
	Content   string         `json:"content" gorm:"type:text;not null"`
	TokensUsed int           `json:"tokens_used" gorm:"default:0"`
	Model     string         `json:"model"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
	Chat      Chat           `json:"chat,omitempty" gorm:"foreignKey:ChatID"`
}



type UpdateChatRequest struct {
	Title    string `json:"title" binding:"omitempty,min=1,max=100"`
	IsActive *bool  `json:"is_active"`
}

type CreateMessageRequest struct {
	Content string `json:"content" binding:"required,min=1"`
	Role    string `json:"role" binding:"required,oneof=user assistant"`
	Model   string `json:"model"`
}

type CreateDirectMessageRequest struct {
	Content string `json:"content" binding:"required,min=1"`
	Model   string `json:"model"`
}

type ChatResponse struct {
	ID           uint      `json:"id"`
	UserID       uint      `json:"user_id"`
	Title        string    `json:"title"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	MessageCount int64     `json:"message_count"`
	LastMessage  *Message  `json:"last_message,omitempty"`
}

type ChatWithMessagesResponse struct {
	ChatResponse
	Messages []Message `json:"messages"`
}
