// FILE: auth.go
package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	// "fmt" // THIS LINE WAS REMOVED
	"log"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const sessionCookieName = "jenkins_dashboard_session"
const sessionDuration = 24 * time.Hour

// signupHandler handles user registration.
// GET shows the signup page, POST processes the registration.
func signupHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		http.ServeFile(w, r, "static/signup.html")
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == "" || password == "" {
		http.Error(w, "Username and password are required", http.StatusBadRequest)
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("Error hashing password: %v", err)
		http.Error(w, "Server error, unable to create your account", http.StatusInternalServerError)
		return
	}

	_, err = db.Exec("INSERT INTO users (username, password_hash) VALUES ($1, $2)", username, hashedPassword)
	if err != nil {
		// This will fail if the username is already taken due to the UNIQUE constraint
		log.Printf("Error inserting new user: %v", err)
		http.Error(w, "Username already exists or server error", http.StatusConflict)
		return
	}

	log.Printf("New user created: %s", username)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// loginHandler handles user login.
// GET shows the login page, POST processes the login attempt.
func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		http.ServeFile(w, r, "static/login.html")
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	var userID int
	var hashedPassword string
	err := db.QueryRow("SELECT id, password_hash FROM users WHERE username = $1", username).Scan(&userID, &hashedPassword)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Invalid username or password", http.StatusUnauthorized)
		} else {
			log.Printf("Error querying user: %v", err)
			http.Error(w, "Server error", http.StatusInternalServerError)
		}
		return
	}

	err = bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	if err != nil {
		// Passwords don't match
		http.Error(w, "Invalid username or password", http.StatusUnauthorized)
		return
	}

	// Login successful, create a session
	sessionToken, err := generateSessionToken()
	if err != nil {
		log.Printf("Error generating session token: %v", err)
		http.Error(w, "Server error, could not log in", http.StatusInternalServerError)
		return
	}

	expiresAt := time.Now().Add(sessionDuration)
	_, err = db.Exec("INSERT INTO sessions (user_id, token, expires_at) VALUES ($1, $2, $3)", userID, sessionToken, expiresAt)
	if err != nil {
		log.Printf("Error creating session: %v", err)
		http.Error(w, "Server error, could not log in", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionToken,
		Expires:  expiresAt,
		HttpOnly: true, // Important for security
		Path:     "/",
	})

	log.Printf("User %s (ID: %d) logged in successfully", username, userID)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// logoutHandler clears the session cookie and database record.
func logoutHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		// No cookie, so nothing to do
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Delete the session from the database
	_, err = db.Exec("DELETE FROM sessions WHERE token = $1", cookie.Value)
	if err != nil {
		log.Printf("Error deleting session from DB: %v", err)
	}

	// Expire the cookie in the browser
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Path:     "/",
	})

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// authMiddleware is a middleware that requires a valid session to access a route.
func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			// No session cookie, redirect to login
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		sessionToken := cookie.Value
		var userID int
		var expiresAt time.Time

		err = db.QueryRow("SELECT user_id, expires_at FROM sessions WHERE token = $1", sessionToken).Scan(&userID, &expiresAt)
		if err != nil || time.Now().After(expiresAt) {
			// Invalid session or expired
			if err != nil && err != sql.ErrNoRows {
				log.Printf("Error validating session: %v", err)
			}
			// Clean up expired cookie and redirect
			http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "", Expires: time.Unix(0, 0), Path: "/"})
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		// User is authenticated, proceed to the next handler
		next.ServeHTTP(w, r)
	})
}

// generateSessionToken creates a cryptographically secure random token.
func generateSessionToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}