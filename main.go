package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

type Build struct {
	Class  string `json:"_class"`
	Number int    `json:"number"`
	URL    string `json:"url"`
}

type HealthReport struct {
	Description   string `json:"description"`
	IconClassName string `json:"iconClassName"`
	IconURL       string `json:"iconUrl"`
	Score         int    `json:"score"`
}

type JobDetail struct {
	DisplayName           string         `json:"displayName"`
	FullDisplayName       string         `json:"fullDisplayName"`
	Name                  string         `json:"name"`
	URL                   string         `json:"url"`
	Buildable             bool           `json:"buildable"`
	Color                 string         `json:"color"`
	Builds                []Build        `json:"builds"`
	FirstBuild            *Build         `json:"firstBuild"`
	HealthReport          []HealthReport `json:"healthReport"`
	InQueue               bool           `json:"inQueue"`
	KeepDependencies      bool           `json:"keepDependencies"`
	LastBuild             *Build         `json:"lastBuild"`
	LastCompletedBuild    *Build         `json:"lastCompletedBuild"`
	LastFailedBuild       *Build         `json:"lastFailedBuild"`
	LastStableBuild       *Build         `json:"lastStableBuild"`
	LastSuccessfulBuild   *Build         `json:"lastSuccessfulBuild"`
	LastUnstableBuild     *Build         `json:"lastUnstableBuild"`
	LastUnsuccessfulBuild *Build         `json:"lastUnsuccessfulBuild"`
	NextBuildNumber       int            `json:"nextBuildNumber"`
}

func jobHandler(w http.ResponseWriter, r *http.Request) {
	url := "http://localhost:8080/job/Test-Job/api/json"

	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req.SetBasicAuth("diya", "diya")

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Error fetching job JSON: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var job JobDetail
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		http.Error(w, "Error decoding JSON: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Allow frontend access (CORS)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(job)
}

func main() {
	http.HandleFunc("/api/job", jobHandler)
	http.Handle("/", http.FileServer(http.Dir("./static")))
	fmt.Println("Server started at http://localhost:9090")
	log.Fatal(http.ListenAndServe(":9090", nil))
}
