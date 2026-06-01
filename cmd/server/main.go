package main

import (
	"log"

	"github.com/Hyuk-II/todai-middleware/internal/config"
	"github.com/Hyuk-II/todai-middleware/internal/websocket"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file, using environment variables")
	}

	cfg := config.Load()

	r := gin.Default()

	wsHandler := websocket.NewHandler()
	r.GET("/ws", wsHandler.ServeHTTP)

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	log.Printf("todai-middleware starting on :%s", cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
