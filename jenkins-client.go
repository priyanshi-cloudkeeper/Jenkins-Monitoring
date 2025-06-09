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

// --- Structs for API responses from our DB ---
type JobAPIDetail struct {
	JobDetail              // Embed JobDetail from job-details.go
	LastFetchedAt *time.Time `json:"last_fetched_at,omitempty"`
}

type BuildAPIDetail struct {
	Build              // Embed Build from job-details.go
	FetchedAt *time.Time `json:"fetched_at,omitempty"`
}

type JobWithLastBuildAPIResponse struct {
	Name             string     `json:"name"`
	Status           string     `json:"status"`
	URL              string     `json:"url"`
	LastBuildNumber  *int       `json:"last_build_number,omitempty"`
	LastBuildResult  *string    `json:"last_build_result,omitempty"`
	LastBuildTime    *time.Time `json:"last_build_time,omitempty"` // Use time.Time for better handling
	LastFetchedAt    *time.Time `json:"last_fetched_at,omitempty"`
}


// fetchJobs fetches the basic list of jobs from Jenkins API.
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
		// log.Printf("Unknown job color received: %s, mapping to UNKNOWN", color) // Can be noisy
		return "UNKNOWN"
	}
}

// jobsHandler handles the /api/jobs endpoint for on-demand requests.
// It will now fetch from DB and include last build info.
func jobsHandler(w http.ResponseWriter, r *http.Request) {
	// For this on-demand endpoint, we'll fetch directly from our database.
	// The background refresher keeps the DB up-to-date.
	rows, err := db.Query(`
		SELECT 
			j.name, j.status, j.url, j.last_fetched_at,
			lb.build_number, lb.result, lb.timestamp
		FROM jobs j
		LEFT JOIN (
			SELECT 
				job_name, build_number, result, timestamp,
				ROW_NUMBER() OVER (PARTITION BY job_name ORDER BY build_number DESC) as rn
			FROM builds
		) lb ON j.name = lb.job_name AND lb.rn = 1
		ORDER BY j.name;
	`)
	if err != nil {
		log.Printf("jobsHandler: Error querying jobs from DB: %v", err)
		http.Error(w, "Failed to retrieve jobs from database", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var responseJobs []JobWithLastBuildAPIResponse
	for rows.Next() {
		var job JobWithLastBuildAPIResponse
		var lastBuildTimestamp sql.NullInt64 // Jenkins timestamp is int64
		
		err := rows.Scan(
			&job.Name, &job.Status, &job.URL, &job.LastFetchedAt,
			&job.LastBuildNumber, &job.LastBuildResult, &lastBuildTimestamp,
		)
		if err != nil {
			log.Printf("jobsHandler: Error scanning job row: %v", err)
			http.Error(w, "Failed to process job data", http.StatusInternalServerError)
			return
		}
		if lastBuildTimestamp.Valid {
			// Convert Unix milliseconds to time.Time
			t := time.Unix(0, lastBuildTimestamp.Int64*int64(time.Millisecond))
			job.LastBuildTime = &t
		}
		responseJobs = append(responseJobs, job)
	}

	if err = rows.Err(); err != nil {
		log.Printf("jobsHandler: Error after iterating job rows: %v", err)
		http.Error(w, "Error processing job results", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(responseJobs)
}


// jobDetailsFromDBHandler serves detailed job information from the database.
func jobDetailsFromDBHandler(w http.ResponseWriter, r *http.Request) {
	jobName := r.URL.Query().Get("name")
	if jobName == "" {
		http.Error(w, "Missing job name", http.StatusBadRequest)
		return
	}

	// 1. Fetch Job details from 'jobs' table (we'll assume the JobDetail struct can be populated)
	// For simplicity, we'll fetch the Jenkins API JSON from the /api/job endpoint,
	// which also updates the DB. Then, we can reconstruct what we need.
	// A more optimized way would be to query DB directly for all fields of JobDetail if they are stored.
	// Since JobDetail is complex and mirrors Jenkins API, we use the original jobDetailHandler
	// to get the full structure and ensure DB is up-to-date for this specific call.

	// Call the original jobDetailHandler's core logic but don't write response yet.
	// This ensures the DB is updated with the latest from Jenkins for this job.
	err := fetchAndStoreJobDetails(jobName) // This updates the DB.
	if err != nil {
		// Log the error but try to fetch from DB anyway if data exists.
		log.Printf("jobDetailsFromDBHandler: Error during Jenkins sync for %s: %v. Attempting to serve from DB.", jobName, err)
		// If Jenkins fetch fails, we might still have data in the DB.
	}
	
	// Now fetch the (potentially updated) job and its builds from DB
	var apiJobDetail JobAPIDetail
	
	// Fetch core job data (assuming JobDetail struct mirrors Jenkins API, not all fields are in 'jobs' table)
	// We will rely on what's in 'jobs' and 'builds' tables primarily.
	// For a true "from DB" experience, you'd have more columns in 'jobs' or a separate 'job_details' table.
	// Let's build a response from what we have:
	row := db.QueryRow(`SELECT name, url, color, status, last_fetched_at FROM jobs WHERE name = $1`, jobName)
	var fetchedJob struct {
		Name          string
		URL           string
		Color         string
		Status        string
		LastFetchedAt sql.NullTime
	}
	err = row.Scan(&fetchedJob.Name, &fetchedJob.URL, &fetchedJob.Color, &fetchedJob.Status, &fetchedJob.LastFetchedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Job not found in database", http.StatusNotFound)
		} else {
			log.Printf("jobDetailsFromDBHandler: Error fetching job '%s' from DB: %v", jobName, err)
			http.Error(w, "Error fetching job details from database", http.StatusInternalServerError)
		}
		return
	}

	// Populate the parts of JobDetail we have
	apiJobDetail.Name = fetchedJob.Name
	apiJobDetail.DisplayName = fetchedJob.Name // Or fetch from Jenkins response if available
	apiJobDetail.URL = fetchedJob.URL
	apiJobDetail.Color = fetchedJob.Color
	// apiJobDetail.Status = fetchedJob.Status // Status is not a direct field in JobDetail struct
	if fetchedJob.LastFetchedAt.Valid {
		apiJobDetail.LastFetchedAt = &fetchedJob.LastFetchedAt.Time
	}


	// Fetch Builds for this job
	buildRows, err := db.Query(`
		SELECT build_number, url, result, timestamp, duration, fetched_at 
		FROM builds 
		WHERE job_name = $1 
		ORDER BY build_number DESC
	`, jobName)
	if err != nil {
		log.Printf("jobDetailsFromDBHandler: Error fetching builds for '%s' from DB: %v", jobName, err)
		http.Error(w, "Error fetching build details", http.StatusInternalServerError)
		return
	}
	defer buildRows.Close()

	var builds []BuildAPIDetail
	for buildRows.Next() {
		var b BuildAPIDetail
		var buildResult sql.NullString
		var buildTimestamp sql.NullInt64
		var buildDuration sql.NullInt64
		var buildFetchedAt sql.NullTime

		err := buildRows.Scan(&b.Number, &b.URL, &buildResult, &buildTimestamp, &buildDuration, &buildFetchedAt)
		if err != nil {
			log.Printf("jobDetailsFromDBHandler: Error scanning build row for '%s': %v", jobName, err)
			continue // Skip this build
		}
		if buildResult.Valid {
			b.Result = &buildResult.String
		}
		if buildTimestamp.Valid {
			b.Timestamp = buildTimestamp.Int64
		}
		if buildDuration.Valid {
			b.Duration = buildDuration.Int64
		}
		if buildFetchedAt.Valid {
			b.FetchedAt = &buildFetchedAt.Time
		}
		builds = append(builds, b)
	}
	apiJobDetail.Builds = make([]Build, len(builds)) // Convert BuildAPIDetail to Build
    for i, b := range builds {
        apiJobDetail.Builds[i] = b.Build // Assign the embedded Build struct
    }


	// For other JobDetail fields (like healthReport, description, etc.),
	// they are not directly in our 'jobs' or 'builds' tables.
	// The original jobDetailHandler would be better if the UI needs *all* Jenkins API fields.
	// This handler is now more of a "what we have in our DB" view.
	// To get full details, the UI *could* still call the old `/api/job` if needed for missing fields.

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(apiJobDetail)
}


// fetchAndStoreJobDetails, fetchAndStoreAllJobs, StartBackgroundRefresher, performFullRefresh
// AND the original jobDetailHandler (for /api/job)
// remain the same as in your latest provided jenkins-client.go file.
// I will include them here for completeness.

// fetchAndStoreJobDetails fetches details for a single job and its builds, then stores them.
func fetchAndStoreJobDetails(jobName string) error {
	log.Printf("Background/Sync: Fetching details for job %s", jobName)
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

	var jobDetail JobDetail // From job-details.go
	if err := json.Unmarshal(bodyBytes, &jobDetail); err != nil {
		return fmt.Errorf("error decoding Jenkins job detail JSON for %s: %w. Body: %s", jobName, err, string(bodyBytes))
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("error starting transaction for job detail %s: %w", jobName, err)
	}

	jobStatus := mapColorToStatus(jobDetail.Color)
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

	for _, build := range jobDetail.Builds { // build.Result is *string
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
	log.Printf("Background/Sync: Successfully updated details for job %s in DB", jobName)
	return nil
}

// fetchAndStoreAllJobs fetches the list of all jobs and stores/updates them in the DB.
func fetchAndStoreAllJobs() ([]string, error) {
	log.Println("Background: Fetching all jobs list from Jenkins API...")
	fetchedJenkinsJobs, err := fetchJobs()
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
		status := mapColorToStatus(job.Color)
		_, err := stmt.Exec(job.Name, job.URL, job.Color, status, time.Now())
		if err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("background: error upserting job %s: %w", job.Name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("background: error committing transaction for all jobs: %w", err)
	}
	log.Printf("Background: Successfully updated/stored %d jobs in the DB.", len(jobNames))
	return jobNames, nil
}

// StartBackgroundRefresher starts a goroutine to periodically refresh Jenkins data.
func StartBackgroundRefresher() {
	log.Printf("Starting background Jenkins data refresher every %v", backgroundRefreshInterval)
	go func() {
		performFullRefresh()

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

// jobDetailHandler is the original handler that fetches directly from Jenkins and updates DB
// It returns the full Jenkins API response. The UI uses /api/job-details (new name)
// This original is still named jobDetailHandler internally but its route changed in main.go
func jobDetailHandler(w http.ResponseWriter, r *http.Request) {
	jobName := r.URL.Query().Get("name")
	if jobName == "" {
		http.Error(w, "Missing job name", http.StatusBadRequest)
		return
	}

	// This function will fetch from Jenkins, update DB, AND return full Jenkins payload.
	// This can be used if the UI needs the absolute latest or fields not in our custom DB struct.
	log.Printf("API /api/job (original): Fetching live details for %s from Jenkins and updating DB.", jobName)

	fetchURL := fmt.Sprintf("%s/job/%s/api/json?tree=actions,description,displayName,displayNameOrNull,fullDisplayName,fullName,name,url,buildable,builds[_class,number,url,result,timestamp,duration],color,firstBuild[_class,number,url],healthReport[description,iconClassName,iconUrl,score],inQueue,keepDependencies,lastBuild[_class,number,url,result,timestamp,duration],lastCompletedBuild[_class,number,url],lastFailedBuild[_class,number,url],lastStableBuild[_class,number,url],lastSuccessfulBuild[_class,number,url],lastUnstableBuild[_class,number,url],lastUnsuccessfulBuild[_class,number,url],nextBuildNumber,property,queueItem,concurrentBuild,resumeBlocked", jenkinsURL, jobName)
	req, err := http.NewRequest("GET", fetchURL, nil)
	if err != nil {
		log.Printf("API /api/job: Error creating request for %s: %v", jobName, err)
		http.Error(w, "Error creating request", http.StatusInternalServerError)
		return
	}
	if jenkinsUser != "" && jenkinsToken != "" {
		req.SetBasicAuth(jenkinsUser, jenkinsToken)
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("API /api/job: Error fetching job detail for %s from Jenkins: %v", jobName, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("API /api/job: Jenkins API error for %s: status %d, body: %s", jobName, resp.StatusCode, string(bodyBytes))
		http.Error(w, fmt.Sprintf("Error fetching job details from Jenkins: %s", http.StatusText(resp.StatusCode)), resp.StatusCode)
		return
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("API /api/job: Error reading response body for %s: %v", jobName, err)
		http.Error(w, "Failed to read response body", http.StatusInternalServerError)
		return
	}

	// Attempt to unmarshal and store in DB (this part is like fetchAndStoreJobDetails)
	var jobDetailFromJenkins JobDetail
	if err := json.Unmarshal(bodyBytes, &jobDetailFromJenkins); err != nil {
		log.Printf("API /api/job: Error decoding Jenkins JSON for %s: %v. DB will not be updated by this call.", jobName, err)
		// Still return the raw Jenkins JSON to the client
		w.Header().Set("Content-Type", "application/json")
		w.Write(bodyBytes)
		return
	}
	
	// Update DB with the freshly fetched data
	// Encapsulate DB update logic to avoid repetition (or call fetchAndStoreJobDetails which does this)
	// For simplicity here, we'll repeat a condensed version of the DB update.
	// In a real app, you'd call fetchAndStoreJobDetails(jobName) here if its return wasn't an issue.
	go func() { // Update DB in background to not slow down API response
		errDbUpdate := fetchAndStoreJobDetails(jobName) // This is fine as it fetches again, or refactor to take JobDetail
		if errDbUpdate != nil {
			log.Printf("API /api/job: Background DB update failed for %s after successful Jenkins fetch: %v", jobName, errDbUpdate)
		}
	}()


	w.Header().Set("Content-Type", "application/json")
	w.Write(bodyBytes) // Send the original Jenkins response back to client
}