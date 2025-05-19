package main

import (
	"fmt"
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/api/jobs", jobsHandler)
	http.HandleFunc("/api/job-details", jobDetailHandler)

	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/", fs)

	fmt.Println("Server running at http://localhost:9090")
	log.Fatal(http.ListenAndServe(":9090", nil))
}