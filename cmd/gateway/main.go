package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

func main() {
	r := gin.Default()

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// TODO (Sat 13 Jun): /v1/messages proxy endpoint → single provider
	log.Println("llm-gateway starting on :8080")
	if err := r.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}
