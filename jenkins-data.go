// package main

// import (
// 	"encoding/json"
// 	"fmt"
// 	"net/http"
// )

// func main() {
// 	url := "http://localhost:8080/api/json?tree=jobs[name,color,url,builds[number,url],lastBuild[number,url]]"
// 	username := "priyanshi"
// 	password := "mb4uuvXm@1"

// 	req, _ := http.NewRequest("GET", url, nil)
// 	req.SetBasicAuth(username, password)

// 	resp, err := http.DefaultClient.Do(req)
// 	if err != nil {
// 		fmt.Println("Request error:", err)
// 		return
// 	}
// 	defer resp.Body.Close()

// 	var result map[string]interface{}
// 	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
// 		fmt.Println("JSON decode error:", err)
// 		return
// 	}

// 	jobs, ok := result["jobs"].([]interface{})
// 	if !ok {
// 		fmt.Println("Failed to get jobs list")
// 		return
// 	}

// 	for _, j := range jobs {
// 		job, ok := j.(map[string]interface{})
// 		if !ok {
// 			continue
// 		}

// 		name, _ := job["name"].(string)
// 		color, _ := job["color"].(string)
// 		url, _ := job["url"].(string)

// 		builds, _ := job["builds"].([]interface{})
// 		totalBuilds := len(builds)

// 		lastBuild := job["lastBuild"].(map[string]interface{})
// 		lastBuildNumber := int(lastBuild["number"].(float64))
// 		lastBuildURL, _ := lastBuild["url"].(string)


// 		fmt.Println("Job Name      :", name)
// 		fmt.Println("Color/Status  :", color)
// 		fmt.Println("Total Builds  :", totalBuild;s)
// 		fmt.Printf("Last Build    : #%d (%s)\n", lastBuildNumber, lastBuildURL)
// 		fmt.Println("Job URL       :", url)
// 	}
// }

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
	DisplayName         string         `json:"displayName"`
	FullDisplayName     string         `json:"fullDisplayName"`
	Name                string         `json:"name"`
	URL                 string         `json:"url"`
	Buildable           bool           `json:"buildable"`
	Color               string         `json:"color"`
	Builds              []Build        `json:"builds"`
	FirstBuild          *Build         `json:"firstBuild"`
	HealthReport        []HealthReport `json:"healthReport"`
	InQueue             bool           `json:"inQueue"`
	KeepDependencies    bool           `json:"keepDependencies"`
	LastBuild           *Build         `json:"lastBuild"`
	LastCompletedBuild  *Build         `json:"lastCompletedBuild"`
	LastFailedBuild     *Build         `json:"lastFailedBuild"`
	LastStableBuild     *Build         `json:"lastStableBuild"`
	LastSuccessfulBuild *Build         `json:"lastSuccessfulBuild"`
	LastUnstableBuild   *Build         `json:"lastUnstableBuild"`
	LastUnsuccessfulBuild *Build       `json:"lastUnsuccessfulBuild"`
	NextBuildNumber     int            `json:"nextBuildNumber"`
}

func main() {
	url := "http://localhost:8080/job/Test-Job/api/json"

	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatal(err)
	}

	req.SetBasicAuth("priyanshi", "mb4uuvXm@1")

	resp, err := client.Do(req)
	if err != nil {
		log.Fatal("Error fetching job JSON:", err)
	}
	defer resp.Body.Close()

	var job JobDetail
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		log.Fatal("Error decoding JSON:", err)
	}

	fmt.Println("Job Name:", job.Name)
	fmt.Println("Display Name:", job.DisplayName)
	fmt.Println("URL:", job.URL)
	fmt.Println("Buildable:", job.Buildable)
	fmt.Println("Color:", job.Color)
	fmt.Println("Next Build Number:", job.NextBuildNumber)
	fmt.Println("Last Failed built:", job.LastFailedBuild)
	fmt.Println("Last Stable built:", job.LastStableBuild)
	fmt.Println("Last Successful built:" , job.LastSuccessfulBuild)

	fmt.Println("Last Build:")
	if job.LastBuild != nil {
		fmt.Printf("  Number: %d\n  URL: %s\n", job.LastBuild.Number, job.LastBuild.URL)
	}

	fmt.Println("Health Reports:")
	for _, hr := range job.HealthReport {
		fmt.Printf("  Description: %s\n  Score: %d\n", hr.Description, hr.Score)
	}
}

