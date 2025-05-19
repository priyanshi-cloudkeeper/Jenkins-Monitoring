package main

import (
	"encoding/json"
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
	DisplayName          string         `json:"displayName"`
	FullDisplayName      string         `json:"fullDisplayName"`
	Name                 string         `json:"name"`
	URL                  string         `json:"url"`
	Buildable            bool           `json:"buildable"`
	Color                string         `json:"color"`
	Builds               []Build        `json:"builds"`
	FirstBuild           *Build         `json:"firstBuild"`
	HealthReport         []HealthReport `json:"healthReport"`
	InQueue              bool           `json:"inQueue"`
	KeepDependencies     bool           `json:"keepDependencies"`
	LastBuild            *Build         `json:"lastBuild"`
	LastCompletedBuild   *Build         `json:"lastCompletedBuild"`
	LastFailedBuild      *Build         `json:"lastFailedBuild"`
	LastStableBuild      *Build         `json:"lastStableBuild"`
	LastSuccessfulBuild  *Build         `json:"lastSuccessfulBuild"`
	LastUnstableBuild    *Build         `json:"lastUnstableBuild"`
	LastUnsuccessfulBuild *Build        `json:"lastUnsuccessfulBuild"`
	NextBuildNumber      int            `json:"nextBuildNumber"`
}

var jobData JobDetail

func fetchJobData() error {
	url := "http://localhost:8080/job/Test-Job/api/json"

	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}

	req.SetBasicAuth("priyanshi", "mb4uuvXm@1")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return json.NewDecoder(resp.Body).Decode(&jobData)
}

func jobDataHandler(w http.ResponseWriter, r *http.Request) {

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Content-Type", "application/json")


	err := fetchJobData()
	if err != nil {
		http.Error(w, "Failed to fetch job data: "+err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(jobData)
}

func main() {
	http.HandleFunc("/jobdata", jobDataHandler)
	log.Println("Starting server on :8082...")
	log.Fatal(http.ListenAndServe(":8082", nil))
}
