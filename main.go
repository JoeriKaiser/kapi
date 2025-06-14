package main

import (
	"log"
	"os"

	"kapi/config"
	"kapi/controllers"
	"kapi/database"
	"kapi/handlers"
	"kapi/middleware"
	"kapi/models"
	"kapi/routes"
	"kapi/services"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "kapi/docs"
)

// @title Chat API
// @version 1.0
// @description A simple chat API with user authentication and AI chat management
// @termsOfService http://swagger.io/terms/

// @contact.name API Support
// @contact.url http://www.swagger.io/support
// @contact.email support@swagger.io

// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html

// @host localhost:8080
// @BasePath /api/v1

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and JWT token.

func main() {
  if wd, err := os.Getwd(); err == nil {
      log.Printf("Current working directory: %s", wd)
  }

  if err := godotenv.Load(); err != nil {
      log.Printf("Error loading .env file: %v", err)
  }

	db := database.Connect()
	db.AutoMigrate(&models.User{}, &models.Post{}, &models.Chat{}, &models.Message{})

	cfg := config.Load()

	r := gin.Default()

	r.Use(middleware.CORS())
	r.Use(middleware.Logger())
	r.Use(middleware.ErrorHandler())

	hubService := services.NewHubService()

	userController := controllers.NewUserController(db)
	authController := controllers.NewAuthController(db)
	chatController := controllers.NewChatController(db, cfg, hubService)
	wsHandler := handlers.NewWebSocketHandler(hubService)

	routes.SetupRoutes(r, userController, authController, chatController, wsHandler)

	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	log.Println("Server starting on :8080")
	log.Println("Swagger docs available at: http://localhost:8080/swagger/index.html")
	r.Run(":8080")

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on port %s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
