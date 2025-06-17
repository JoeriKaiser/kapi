package routes

import (
	"kapi/controllers"
	"kapi/handlers"
	"kapi/middleware"
	"net/http"

	"github.com/gin-gonic/gin"
)

func SetupRoutes(r *gin.Engine, userController *controllers.UserController, authController *controllers.AuthController, chatController *controllers.ChatController, w *handlers.WebSocketHandler) {
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	api := r.Group("/api/v1")
	{
		auth := api.Group("/auth")
		{
			auth.POST("/register", authController.Register)
			auth.POST("/login", authController.Login)
			auth.GET("/me", middleware.AuthRequired(), authController.Me)
			auth.GET("/ws", middleware.AuthRequired(), w.HandleWebSocket)
		}

		users := api.Group("/users")
		users.Use(middleware.AuthRequired())
		{
			users.GET("", userController.GetUsers)
			users.GET("/:id", userController.GetUser)
			users.PUT("/:id", userController.UpdateUser)
			users.DELETE("/:id", userController.DeleteUser)

			users.PUT("/openrouter-key", userController.UpdateOpenRouterKey)
			users.GET("/openrouter-key/status", userController.GetOpenRouterKeyStatus)
			users.DELETE("/openrouter-key", userController.DeleteOpenRouterKey)
		}

		directMessages := api.Group("/messages")
		directMessages.Use(middleware.AuthRequired())
		{
			directMessages.POST("", chatController.CreateDirectMessage)
		}

		chats := api.Group("/chats")
		chats.Use(middleware.AuthRequired())
		{
			chats.GET("", chatController.GetUserChats)
			chats.GET("/:id", chatController.GetChat)
			chats.PUT("/:id", chatController.UpdateChat)
			chats.DELETE("/:id", chatController.DeleteChat)
			chats.POST("/:id/stream", chatController.CreateDirectMessageStream)
		}

		messages := api.Group("/chats/:id/messages")
		messages.Use(middleware.AuthRequired())
		{
			messages.POST("", chatController.CreateMessage)
			messages.GET("", chatController.GetChatMessages)
			messages.PUT("/:messageId", chatController.UpdateMessage)
			messages.DELETE("/:messageId", chatController.DeleteMessage)
		}
	}
}
