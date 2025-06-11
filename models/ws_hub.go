package models

import (
	"github.com/gorilla/websocket"
)

type Hub struct {
    Clients     map[*Client]bool
    Broadcast   chan []byte
    Register    chan *Client
    Unregister  chan *Client
    UserClients map[string][]*Client
}

type Client struct {
    Hub    *Hub
    Conn   *websocket.Conn
    Send   chan []byte
    UserID string
}

type WSMessage struct {
    Type string      `json:"type"`
    Data interface{} `json:"data"`
}

func NewHub() *Hub {
    return &Hub{
        Clients:     make(map[*Client]bool),
        Broadcast:   make(chan []byte),
        Register:    make(chan *Client),
        Unregister:  make(chan *Client),
        UserClients: make(map[string][]*Client),
    }
}