package main

import (
	"log"

	"lingoway/internal/config"
	"lingoway/internal/httpapi"
)

func main() {
	cfg := config.Load()

	router := httpapi.NewRouter()

	log.Printf("server started on :%s", cfg.Port)
	if err := router.Run(":" + cfg.Port); err != nil {
		log.Fatal(err)
	}
}
