package main

import (
	"log"
	"github.com/joho/godotenv"
)

func init() {

	err := godotenv.Load()
	if err != nil {

		log.Printf("NOTE: Error loading .env file: %v. Will rely on OS environment variables or defaults.", err)
	} else {
		log.Println("Successfully loaded .env file.")
	}
}