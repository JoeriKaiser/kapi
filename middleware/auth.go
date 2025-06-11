package middleware

import (
	"kapi/utils"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		var token string
		if websocket.IsWebSocketUpgrade(c.Request) {
			token = c.Query("token")
			log.Printf("WebSocket token from query: %s", token[:10]+"...")
		} else {
			authHeader := c.GetHeader("Authorization")
			if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
				token = authHeader[7:]
			}
		}
		
		if token == "" {
			log.Println("No token provided")
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "No token provided"})
			return
		}
		
		userID, err := utils.ValidateJWT(token)
		if err != nil {
			log.Printf("Token validation failed: %v", err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			return
		}
		
		log.Printf("Token validated successfully for user: %v", userID)
		c.Set("user_id", userID)
		c.Next()
	}
}
