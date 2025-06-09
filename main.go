// FILE: main.go
package main

import (
	"fmt"
	"log"
	"net/http"
)

func main() {
	initDB()
	StartBackgroundRefresher()

	http.HandleFunc("/api/jobs", jobsHandler) // For the main job list (now fetches from DB)
	// The UI uses /api/job-details for fetching detailed view. Let's point it to our DB-backed handler.
	http.HandleFunc("/api/job-details", jobDetailsFromDBHandler) 
	// Keep original /api/job if needed for direct full Jenkins API pass-through & sync
	http.HandleFunc("/api/job", jobDetailHandler) // This is the original one

	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	fmt.Println("Server running at http://localhost:9090")
	log.Fatal(http.ListenAndServe(":9090", nil))
}