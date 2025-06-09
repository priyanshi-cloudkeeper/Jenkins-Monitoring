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

	// Create a new ServeMux (router). This gives us more control than using the default http handlers.
	mux := http.NewServeMux()

	// --- Step 1: Define PUBLIC routes. These are NOT protected by middleware. ---

	// Handlers for login, signup, and logout logic.
	mux.HandleFunc("/login", loginHandler)
	mux.HandleFunc("/signup", signupHandler)
	mux.HandleFunc("/logout", logoutHandler)

	// A file server for the "/static/" directory. This serves CSS, JS, etc.
	// It is crucial that this is public so the login/signup pages can be styled.
	fs := http.FileServer(http.Dir("./static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	// --- Step 2: Define PROTECTED routes. ---

	// Create a new router specifically for the protected dashboard and its API.
	protectedRouter := http.NewServeMux()

	// All API endpoints are on this protected router.
	protectedRouter.HandleFunc("/api/jobs", jobsHandler)
	protectedRouter.HandleFunc("/api/job-details", jobDetailsFromDBHandler)
	protectedRouter.HandleFunc("/api/job", jobDetailHandler)

	// The root path ("/") serves the main dashboard application (index.html).
	// This is also protected. We use a file server for this as well.
	protectedRouter.Handle("/", http.FileServer(http.Dir("./static")))

	// --- Step 3: Apply the middleware to the entire protected router. ---

	// Any request that isn't a public route will be passed to this handler.
	// We wrap our entire `protectedRouter` with the `authMiddleware`.
	// Now, any request to "/" or "/api/..." will require a valid session.
	mux.Handle("/", authMiddleware(protectedRouter))

	// --- Step 4: Start the server with our new, correctly configured mux. ---
	fmt.Println("Server running at http://localhost:9090/login")
	log.Fatal(http.ListenAndServe(":9090", mux))
}
