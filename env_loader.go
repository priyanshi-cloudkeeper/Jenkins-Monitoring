// FILE: env_loader.go
package main

import (
	"log"
	"github.com/joho/godotenv"
)

func init() {
	// Attempt to load .env file.
	// This function will be called automatically when the program starts,
	// before other init() functions in files that come after it alphabetically
	// (like jenkins-client.go, main.go, db.go if their names are later).
	err := godotenv.Load()
	if err != nil {
		// This is not necessarily fatal if variables are set in the OS environment.
		log.Printf("NOTE: Error loading .env file: %v. Will rely on OS environment variables or defaults.", err)
	} else {
		log.Println("Successfully loaded .env file.")
	}
}