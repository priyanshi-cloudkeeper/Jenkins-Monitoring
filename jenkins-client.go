// FILE: jenkins-client.go
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

var (
	jenkinsURL   string
	jenkinsUser  string
	jenkinsToken string
)

const (
	backgroundRefreshInterval = 30 * time.Second
)

func init() {
	jenkinsURL = os.Getenv("JENKINS_URL")
	jenkinsUser = os.Getenv("JENKINS_USER")
	jenkinsToken = os.Getenv("JENKINS_TOKEN")

	if jenkinsURL == "" {
		jenkinsURL = "http://localhost:8080"
		log.Println("WARNING: JENKINS_URL not set, using default:", jenkinsURL)
	}
	if jenkinsUser == "" {
		log.Println("WARNING: JENKINS_USER not set. API calls may fail without authentication.")
	}
	if jenkinsToken == "" {
		log.Println("WARNING: JENKINS_TOKEN not set. API calls may fail without authentication.")
	}
}

// JenkinsJob is specific to the /api/json endpoint (list of jobs)
type JenkinsJob struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Color string `json:"color"`
}

// JenkinsResponse is specific to the /api/json endpoint (list of jobs)
type JenkinsResponse struct {
	Jobs []JenkinsJob `json:"jobs"`
}

// fetchJobs fetches the basic list of jobs.
func fetchJobs() ([]JenkinsJob, error) {
	url := fmt.Sprintf("%s/api/json?tree=jobs[name,url,color]", jenkinsURL)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}
	if jenkinsUser != "" && jenkinsToken != "" {
		req.SetBasicAuth(jenkinsUser, jenkinsToken)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error fetching jobs from Jenkins: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("jenkins API error (%s/api/json): status %d, body: %s", jenkinsURL, resp.StatusCode, string(bodyBytes))
	}

	var jenkins JenkinsResponse
	if err := json.NewDecoder(resp.Body).Decode(&jenkins); err != nil {
		return nil, fmt.Errorf("error decoding Jenkins response: %w", err)
	}
	return jenkins.Jobs, nil
}

func mapColorToStatus(color string) string {
	switch color {
	case "blue":
		return "SUCCESS"
	case "red":
		return "FAILURE"
	case "yellow":
		return "UNSTABLE"
	case "aborted":
		return "ABORTED"
	case "disabled", "grey", "notbuilt":
		return "DISABLED"
	case "blue_anime", "red_anime", "yellow_anime", "aborted_anime", "disabled_anime", "grey_anime", "notbuilt_anime":
		return "RUNNING"
	default:
		log.Printf("Unknown job color received: %s, mapping to UNKNOWN", color)
		return "UNKNOWN"
	}
}

// jobsHandler handles the /api/jobs endpoint for on-demand requests.
func jobsHandler(w http.ResponseWriter, r *http.Request) {
	fetchedJenkinsJobs, err := fetchJobs() // Call the defined fetchJobs
	if err != nil {
		log.Printf("Error in fetchJobs for jobsHandler: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var responseJobs []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		URL    string `json:"url"`
	}

	// Database update part for on-demand API call
	tx, err := db.Begin()
	if err != nil {
		log.Printf("jobsHandler: Error starting transaction: %v", err)
		http.Error(w, "Database transaction error", http.StatusInternalServerError)
		return
	}

	stmt, err := tx.Prepare(`
        INSERT INTO jobs (name, url, color, status, last_fetched_at)
        VALUES ($1, $2, $3, $4, $5)
        ON CONFLICT (name) DO UPDATE
        SET url = EXCLUDED.url,
            color = EXCLUDED.color,
            status = EXCLUDED.status,
            last_fetched_at = EXCLUDED.last_fetched_at;
    `)
	if err != nil {
		tx.Rollback()
		log.Printf("jobsHandler: Error preparing jobs upsert statement: %v", err)
		http.Error(w, "Database statement error", http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	dbSuccess := true
	for _, job := range fetchedJenkinsJobs {
		status := mapColorToStatus(job.Color) // Call the defined mapColorToStatus
		responseJobs = append(responseJobs, struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			URL    string `json:"url"`
		}{
			Name:   job.Name,
			Status: status,
			URL:    job.URL,
		})

		_, err := stmt.Exec(job.Name, job.URL, job.Color, status, time.Now())
		if err != nil {
			log.Printf("jobsHandler: Error upserting job %s: %v", job.Name, err)
			dbSuccess = false
			break
		}
	}

	if dbSuccess {
		if err := tx.Commit(); err != nil {
			log.Printf("jobsHandler: Error committing transaction for jobs: %v", err)
			http.Error(w, "Database commit error", http.StatusInternalServerError)
			return
		}
	} else {
		tx.Rollback()
		http.Error(w, "jobsHandler: Error processing jobs, transaction rolled back", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(responseJobs)
}

// fetchAndStoreJobDetails fetches details for a single job and its builds, then stores them.
func fetchAndStoreJobDetails(jobName string) error {
	log.Printf("Background: Fetching details for job %s", jobName)
	fetchURL := fmt.Sprintf("%s/job/%s/api/json?tree=actions,description,displayName,displayNameOrNull,fullDisplayName,fullName,name,url,buildable,builds[_class,number,url,result,timestamp,duration],color,firstBuild[_class,number,url],healthReport[description,iconClassName,iconUrl,score],inQueue,keepDependencies,lastBuild[_class,number,url,result,timestamp,duration],lastCompletedBuild[_class,number,url],lastFailedBuild[_class,number,url],lastStableBuild[_class,number,url],lastSuccessfulBuild[_class,number,url],lastUnstableBuild[_class,number,url],lastUnsuccessfulBuild[_class,number,url],nextBuildNumber,property,queueItem,concurrentBuild,resumeBlocked", jenkinsURL, jobName)

	req, err := http.NewRequest("GET", fetchURL, nil)
	if err != nil {
		return fmt.Errorf("error creating request for job detail %s: %w", jobName, err)
	}
	if jenkinsUser != "" && jenkinsToken != "" {
		req.SetBasicAuth(jenkinsUser, jenkinsToken)
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("error fetching job detail for %s from Jenkins: %w", jobName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("jenkins API error for job detail %s: status %d, body: %s", jobName, resp.StatusCode, string(bodyBytes))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response body for job detail %s: %w", jobName, err)
	}

	var jobDetail JobDetail
	if err := json.Unmarshal(bodyBytes, &jobDetail); err != nil {
		return fmt.Errorf("error decoding Jenkins job detail JSON for %s: %w. Body: %s", jobName, err, string(bodyBytes))
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("error starting transaction for job detail %s: %w", jobName, err)
	}

	jobStatus := mapColorToStatus(jobDetail.Color) // Call the defined mapColorToStatus
	_, err = tx.Exec(`
        INSERT INTO jobs (name, url, color, status, last_fetched_at)
        VALUES ($1, $2, $3, $4, $5)
        ON CONFLICT (name) DO UPDATE
        SET url = EXCLUDED.url,
            color = EXCLUDED.color,
            status = EXCLUDED.status,
            last_fetched_at = EXCLUDED.last_fetched_at;
    `, jobDetail.Name, jobDetail.URL, jobDetail.Color, jobStatus, time.Now())
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("error upserting job detail for %s in jobs table: %w", jobName, err)
	}

	buildStmt, err := tx.Prepare(`
        INSERT INTO builds (job_name, build_number, url, result, timestamp, duration, fetched_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7)
        ON CONFLICT (job_name, build_number) DO UPDATE
        SET url = EXCLUDED.url,
            result = EXCLUDED.result,
            timestamp = EXCLUDED.timestamp,
            duration = EXCLUDED.duration,
            fetched_at = EXCLUDED.fetched_at;
    `)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("error preparing builds upsert statement for job %s: %w", jobName, err)
	}
	defer buildStmt.Close()

	for _, build := range jobDetail.Builds {
		var dbBuildResult sql.NullString
		if build.Result != nil {
			dbBuildResult.String = *build.Result
			dbBuildResult.Valid = true
		} else {
			dbBuildResult.Valid = false
		}
		_, err := buildStmt.Exec(jobDetail.Name, build.Number, build.URL, dbBuildResult, build.Timestamp, build.Duration, time.Now())
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("error upserting build #%d for job %s: %w", build.Number, jobName, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("error committing transaction for job detail %s: %w", jobName, err)
	}
	log.Printf("Background: Successfully updated details for job %s", jobName)
	return nil
}

// fetchAndStoreAllJobs fetches the list of all jobs and stores/updates them in the DB.
func fetchAndStoreAllJobs() ([]string, error) {
	log.Println("Background: Fetching all jobs list...")
	fetchedJenkinsJobs, err := fetchJobs() // Call the defined fetchJobs
	if err != nil {
		return nil, fmt.Errorf("background fetchJobs failed: %w", err)
	}

	var jobNames []string
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("background: error starting transaction for all jobs: %w", err)
	}

	stmt, err := tx.Prepare(`
        INSERT INTO jobs (name, url, color, status, last_fetched_at)
        VALUES ($1, $2, $3, $4, $5)
        ON CONFLICT (name) DO UPDATE
        SET url = EXCLUDED.url,
            color = EXCLUDED.color,
            status = EXCLUDED.status,
            last_fetched_at = EXCLUDED.last_fetched_at;
    `)
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("background: error preparing jobs upsert statement: %w", err)
	}
	defer stmt.Close()

	for _, job := range fetchedJenkinsJobs {
		jobNames = append(jobNames, job.Name)
		status := mapColorToStatus(job.Color) // Call the defined mapColorToStatus
		_, err := stmt.Exec(job.Name, job.URL, job.Color, status, time.Now())
		if err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("background: error upserting job %s: %w", job.Name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("background: error committing transaction for all jobs: %w", err)
	}
	log.Printf("Background: Successfully updated/stored %d jobs in the list.", len(jobNames))
	return jobNames, nil
}

// StartBackgroundRefresher starts a goroutine to periodically refresh Jenkins data.
func StartBackgroundRefresher() {
	log.Printf("Starting background Jenkins data refresher every %v", backgroundRefreshInterval)
	go func() {
		performFullRefresh() // Initial fetch

		ticker := time.NewTicker(backgroundRefreshInterval)
		defer ticker.Stop()

		for range ticker.C {
			performFullRefresh()
		}
	}()
}

func performFullRefresh() {
	log.Println("Background Refresher: Starting full data refresh cycle.")
	jobNames, err := fetchAndStoreAllJobs()
	if err != nil {
		log.Printf("Background Refresher: Error fetching and storing all jobs: %v", err)
		return
	}

	if len(jobNames) == 0 {
		log.Println("Background Refresher: No jobs found to refresh details for.")
		return
	}

	log.Printf("Background Refresher: Will attempt to refresh details for %d jobs.", len(jobNames))
	var wg sync.WaitGroup
	concurrencyLimit := 5
	semaphore := make(chan struct{}, concurrencyLimit)

	for _, name := range jobNames {
		wg.Add(1)
		semaphore <- struct{}{}

		go func(jobName string) {
			defer wg.Done()
			defer func() { <-semaphore }()

			if err := fetchAndStoreJobDetails(jobName); err != nil {
				log.Printf("Background Refresher: Error fetching/storing details for job %s: %v", jobName, err)
			}
		}(name)
	}
	wg.Wait()
	log.Println("Background Refresher: Full data refresh cycle completed.")
}

// jobDetailHandler handles on-demand requests for specific job details.
func jobDetailHandler(w http.ResponseWriter, r *http.Request) {
	jobName := r.URL.Query().Get("name")
	if jobName == "" {
		http.Error(w, "Missing job name", http.StatusBadRequest)
		return
	}

	fetchURL := fmt.Sprintf("%s/job/%s/api/json?tree=actions,description,displayName,displayNameOrNull,fullDisplayName,fullName,name,url,buildable,builds[_class,number,url,result,timestamp,duration],color,firstBuild[_class,number,url],healthReport[description,iconClassName,iconUrl,score],inQueue,keepDependencies,lastBuild[_class,number,url,result,timestamp,duration],lastCompletedBuild[_class,number,url],lastFailedBuild[_class,number,url],lastStableBuild[_class,number,url],lastSuccessfulBuild[_class,number,url],lastUnstableBuild[_class,number,url],lastUnsuccessfulBuild[_class,number,url],nextBuildNumber,property,queueItem,concurrentBuild,resumeBlocked", jenkinsURL, jobName)

	req, err := http.NewRequest("GET", fetchURL, nil)
	if err != nil {
		log.Printf("Error creating request for job detail %s: %v", jobName, err)
		http.Error(w, "Error creating request", http.StatusInternalServerError)
		return
	}
	if jenkinsUser != "" && jenkinsToken != "" {
		req.SetBasicAuth(jenkinsUser, jenkinsToken)
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error fetching job detail for %s from Jenkins: %v", jobName, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("Jenkins API error for job detail %s: status %d, body: %s", jobName, resp.StatusCode, string(bodyBytes))
		http.Error(w, fmt.Sprintf("Error fetching job details from Jenkins: %s", http.StatusText(resp.StatusCode)), resp.StatusCode)
		return
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response body for job detail %s: %v", jobName, err)
		http.Error(w, "Failed to read response body", http.StatusInternalServerError)
		return
	}

	var jobDetail JobDetail
	if err := json.Unmarshal(bodyBytes, &jobDetail); err != nil {
		log.Printf("Error decoding Jenkins job detail JSON for %s: %v. Body: %s", jobName, err, string(bodyBytes))
		http.Error(w, "Error decoding Jenkins job detail JSON", http.StatusInternalServerError)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		log.Printf("jobDetailHandler: Error starting transaction for job detail %s: %v", jobName, err)
		http.Error(w, "Database transaction error", http.StatusInternalServerError)
		return
	}

	jobStatus := mapColorToStatus(jobDetail.Color) // Call defined mapColorToStatus
	_, err = tx.Exec(`
        INSERT INTO jobs (name, url, color, status, last_fetched_at)
        VALUES ($1, $2, $3, $4, $5)
        ON CONFLICT (name) DO UPDATE
        SET url = EXCLUDED.url,
            color = EXCLUDED.color,
            status = EXCLUDED.status,
            last_fetched_at = EXCLUDED.last_fetched_at;
    `, jobDetail.Name, jobDetail.URL, jobDetail.Color, jobStatus, time.Now())
	if err != nil {
		tx.Rollback()
		log.Printf("jobDetailHandler: Error upserting job detail for %s in jobs table: %v", jobName, err)
		http.Error(w, "Database error saving job detail", http.StatusInternalServerError)
		return
	}

	buildStmt, err := tx.Prepare(`
        INSERT INTO builds (job_name, build_number, url, result, timestamp, duration, fetched_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7)
        ON CONFLICT (job_name, build_number) DO UPDATE
        SET url = EXCLUDED.url,
            result = EXCLUDED.result,
            timestamp = EXCLUDED.timestamp,
            duration = EXCLUDED.duration,
            fetched_at = EXCLUDED.fetched_at;
    `)
	if err != nil {
		tx.Rollback()
		log.Printf("jobDetailHandler: Error preparing builds upsert statement for job %s: %v", jobName, err)
		http.Error(w, "Database statement error for builds", http.StatusInternalServerError)
		return
	}
	defer buildStmt.Close()

	dbSuccess := true
	for _, build := range jobDetail.Builds {
		var dbBuildResult sql.NullString
		if build.Result != nil {
			dbBuildResult.String = *build.Result
			dbBuildResult.Valid = true
		} else {
			dbBuildResult.Valid = false
		}
		_, err := buildStmt.Exec(jobDetail.Name, build.Number, build.URL, dbBuildResult, build.Timestamp, build.Duration, time.Now())
		if err != nil {
			log.Printf("jobDetailHandler: Error upserting build #%d for job %s: %v", build.Number, jobName, err)
			dbSuccess = false
			break
		}
	}

	if dbSuccess {
		if err := tx.Commit(); err != nil {
			log.Printf("jobDetailHandler: Error committing transaction for job detail %s: %v", jobName, err)
			http.Error(w, "Database commit error for job detail", http.StatusInternalServerError)
		}
	} else {
		tx.Rollback()
		http.Error(w, "jobDetailHandler: Error processing builds, transaction rolled back", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(bodyBytes)
}