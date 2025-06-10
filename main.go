// FILE: main.go
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	// Refactored to handle missing Jenkins URL gracefully
	initDB()
	if os.Getenv("JENKINS_URL") != "" {
		log.Println("Jenkins URL is configured. Starting background data refresher.")
		StartBackgroundRefresher()
	} else {
		log.Println("WARNING: JENKINS_URL is not set. Application will run without connecting to Jenkins.")
	}

	mux := http.NewServeMux()

	// --- Public Routes ---
	mux.HandleFunc("/login", loginHandler)
	mux.HandleFunc("/signup", signupHandler)
	mux.HandleFunc("/logout", logoutHandler)
	fs := http.FileServer(http.Dir("./static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	// --- Protected Routes ---
	protectedRouter := http.NewServeMux()

	// NEW: Route for the Jenkins selection page after login
	protectedRouter.HandleFunc("/select-jenkins", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/select-jenkins.html")
	})

	// Dashboard & Job List APIs
	protectedRouter.HandleFunc("/api/jobs", jobsHandler)
	protectedRouter.HandleFunc("/api/stats/summary", statsSummaryHandler)
	protectedRouter.HandleFunc("/api/stats/build-history", buildHistoryHandler)
	protectedRouter.HandleFunc("/api/builds/recent-failures", recentFailuresHandler)

	// Job Detail View APIs
	protectedRouter.HandleFunc("/api/job-details", jobDetailsFromDBHandler) // Basic info
	protectedRouter.HandleFunc("/api/job-analysis", jobAnalysisHandler)    // NEW: Detailed metrics
	protectedRouter.HandleFunc("/api/job", originalJobDetailHandlerFromJenkins) // Live Jenkins fetch

	// Serve the main application
	protectedRouter.Handle("/", http.FileServer(http.Dir("./static")))

	// Apply middleware and start server
	mux.Handle("/", authMiddleware(protectedRouter))
	fmt.Println("Server running at http://localhost:9090")
	log.Fatal(http.ListenAndServe(":9090", mux))
}