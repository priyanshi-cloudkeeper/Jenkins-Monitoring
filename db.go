// FILE: db.go
package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq" // PostgreSQL driver
)

var db *sql.DB // Keep db as a package-level variable

// initDB initializes the database connection and creates tables if they don't exist.
// This function is called from main() in main.go
func initDB() {
	err1 := godotenv.Load()
	if err1 != nil {
		log.Println("WARNING: Could not load .env file. Using environment variables directly.")
	}
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		//connStr = "postgres://jenkins:jenkins@localhost:5432/postgres?sslmode=disable"
		log.Println("WARNING: DATABASE_URL environment variable not set.")
		//log.Println("Example: postgres://youruser:yourpassword@yourhost:yourport/yourdatabase?sslmode=require")
	}

	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatalf("Error connecting to the database: %v", err)
	}

	err = db.Ping()
	if err != nil {
		log.Fatalf("Error pinging the database: %v", err)
	}

	fmt.Println("Successfully connected to PostgreSQL!")
	createTables()
}

// createTables creates the necessary database tables if they don't already exist.
func createTables() {
	usersTableSQL := `
	CREATE TABLE IF NOT EXISTS users (
		id SERIAL PRIMARY KEY,
		username VARCHAR(255) UNIQUE NOT NULL,
		password_hash VARCHAR(255) NOT NULL,
		created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
	);`
	_, err := db.Exec(usersTableSQL)
	if err != nil {
		log.Fatalf("Error creating users table: %v", err)
	}
	fmt.Println("Table 'users' checked/created successfully.")

	sessionsTableSQL := `
	CREATE TABLE IF NOT EXISTS sessions (
		id SERIAL PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		token VARCHAR(255) UNIQUE NOT NULL,
		expires_at TIMESTAMPTZ NOT NULL
	);`
	_, err = db.Exec(sessionsTableSQL)
	if err != nil {
		log.Fatalf("Error creating sessions table: %v", err)
	}
	fmt.Println("Table 'sessions' checked/created successfully.")

	// ... (rest of createTables for jobs and builds remains the same)
	jobsTableSQL := `
    CREATE TABLE IF NOT EXISTS jobs (
        id SERIAL PRIMARY KEY,
        name VARCHAR(255) UNIQUE NOT NULL,
        url VARCHAR(255),
        color VARCHAR(50),
        status VARCHAR(50),
        last_fetched_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
    );`

	_, err = db.Exec(jobsTableSQL)
	if err != nil {
		log.Fatalf("Error creating jobs table: %v", err)
	}
	fmt.Println("Table 'jobs' checked/created successfully.")

	buildsTableSQL := `
    CREATE TABLE IF NOT EXISTS builds (
        id SERIAL PRIMARY KEY,
        job_name VARCHAR(255) NOT NULL REFERENCES jobs(name) ON DELETE CASCADE,
        build_number INTEGER,
        url VARCHAR(255),
        result VARCHAR(50) NULL,
        timestamp BIGINT,
        duration BIGINT,
        fetched_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
        UNIQUE (job_name, build_number)
    );`

	_, err = db.Exec(buildsTableSQL)
	if err != nil {
		log.Fatalf("Error creating builds table: %v", err)
	}
	fmt.Println("Table 'builds' checked/created successfully.")

	buildsJobNameIndexSQL := `CREATE INDEX IF NOT EXISTS idx_builds_job_name ON builds (job_name);`
	_, err = db.Exec(buildsJobNameIndexSQL)
	if err != nil {
		log.Fatalf("Error creating index on builds.job_name: %v", err)
	}
	fmt.Println("Index 'idx_builds_job_name' on 'builds' table checked/created successfully.")
}