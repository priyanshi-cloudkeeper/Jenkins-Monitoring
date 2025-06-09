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

	// Endpoints for the Grafana-style UI
	http.HandleFunc("/api/jobs", jobsHandler)                     // For the "All Jobs" table panel (fetches from DB)
	http.HandleFunc("/api/job-details", jobDetailsFromDBHandler) // For the job detail drill-down view (fetches from DB after sync)
	
	// New Stat Endpoints
	http.HandleFunc("/api/stats/summary", statsSummaryHandler)
	http.HandleFunc("/api/stats/build-history", buildHistoryHandler)
	http.HandleFunc("/api/builds/recent-failures", recentFailuresHandler)

	// Original endpoint that fetches live from Jenkins & returns full payload
	http.HandleFunc("/api/job", originalJobDetailHandlerFromJenkins) 

	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	fmt.Println("Server running at http://localhost:9090")
	log.Fatal(http.ListenAndServe(":9090", nil))
}