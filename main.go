package main

import (
	"log"
	"os"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"project-api/config"
	"project-api/routes"
)
import "github.com/joho/godotenv"
func main() {
	 _ = godotenv.Load()
	// ── Mode ──────────────────────────────────────────────────────────────────
	if os.Getenv("GIN_MODE") == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	// ── Database ──────────────────────────────────────────────────────────────
	config.ConnectDatabase()

	// ── Engine ────────────────────────────────────────────────────────────────
	r := gin.Default()

	// ── CORS ──────────────────────────────────────────────────────────────────
	r.Use(cors.New(cors.Config{
		// AllowOrigins:     allowedOrigins(),
		// AllowOrigins:     []string{"http://localhost:3000", "http://localhost:5173", "http://localhost:8080", "http://localhost:8081"},
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// ── Health check ──────────────────────────────────────────────────────────
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// ── Routes ────────────────────────────────────────────────────────────────
	routes.Register(r)

	// ── Listen ────────────────────────────────────────────────────────────────
	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("🚀  Marketplace API  →  http://localhost:%s", port)
	log.Printf("📧  Admin:  admin@market.com  /  admin123")
	log.Printf("👤  Client: john@example.com  /  client123")

	// if err := r.Run(":" + port); err != nil {
	// 	log.Fatalf("server error: %v", err)
	// }
	if err := r.Run("0.0.0.0:" + port); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// allowedOrigins returns the CORS allow-list from the env variable CORS_ORIGINS
// (comma-separated). Falls back to wildcard in non-release mode.
func allowedOrigins() []string {
	if origins := os.Getenv("CORS_ORIGINS"); origins != "" {
		return []string{origins}
	}
	return []string{"*"}
}
