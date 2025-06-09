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
	"strconv" // Added for parsing query parameters
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

// --- Structs for Jenkins API (Direct Fetch) ---
type JenkinsJob struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Color string `json:"color"`
}

type JenkinsResponse struct {
	Jobs []JenkinsJob `json:"jobs"`
}

// --- Structs for our custom API responses (from DB or aggregated) ---
type JobAPIDetail struct { // For /api/job-details endpoint
	JobDetail             // Embed JobDetail from job-details.go (contains Name, URL, Color, Builds, etc.)
	LastFetchedAt *time.Time `json:"last_fetched_at,omitempty"`
}

// JobForListAPIResponse is for the /api/jobs endpoint (main job list panel)
type JobForListAPIResponse struct {
	Name            string     `json:"name"`
	Status          string     `json:"status"`
	URL             string     `json:"url"`
	LastBuildNumber *int       `json:"last_build_number,omitempty"`
	LastBuildResult *string    `json:"last_build_result,omitempty"`
	LastBuildTime   *time.Time `json:"last_build_time,omitempty"`
	LastFetchedAt   *time.Time `json:"last_fetched_at,omitempty"`
}

// StatsSummaryAPIResponse for /api/stats/summary
type StatsSummaryAPIResponse struct {
	TotalJobs      int     `json:"total_jobs"`
	RunningJobs    int     `json:"running_jobs"`
	SuccessRateDay float64 `json:"success_rate_day"` // Percentage
}

// BuildHistoryPointAPIResponse for /api/stats/build-history
type BuildHistoryPointAPIResponse struct {
	Date             string `json:"date"` // YYYY-MM-DD
	TotalBuilds      int    `json:"total_builds"`
	SuccessfulBuilds int    `json:"successful_builds"`
	FailedBuilds     int    `json:"failed_builds"`
}

// RecentFailureAPIResponse for /api/builds/recent-failures
type RecentFailureAPIResponse struct {
	JobName     string    `json:"job_name"`
	BuildNumber int       `json:"build_number"`
	BuildURL    string    `json:"build_url"`
	Timestamp   time.Time `json:"timestamp"`
}

// --- Helper Functions ---
func fetchJobsFromJenkinsAPI() ([]JenkinsJob, error) {
	url := fmt.Sprintf("%s/api/json?tree=jobs[name,url,color]", jenkinsURL)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil { return nil, fmt.Errorf("creating req: %w", err) }
	if jenkinsUser != "" && jenkinsToken != "" { req.SetBasicAuth(jenkinsUser, jenkinsToken) }
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil { return nil, fmt.Errorf("fetching jobs from Jenkins: %w", err) }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK { bodyBytes, _ := io.ReadAll(resp.Body); return nil, fmt.Errorf("jenkins API err (%d): %s", resp.StatusCode, string(bodyBytes)) }
	var jenkinsResp JenkinsResponse
	if err := json.NewDecoder(resp.Body).Decode(&jenkinsResp); err != nil { return nil, fmt.Errorf("decoding Jenkins resp: %w", err) }
	return jenkinsResp.Jobs, nil
}

func mapColorToStatus(color string) string {
	switch color {
	case "blue": return "SUCCESS"
	case "red": return "FAILURE"
	case "yellow": return "UNSTABLE"
	case "aborted": return "ABORTED"
	case "disabled", "grey", "notbuilt": return "DISABLED"
	case "blue_anime", "red_anime", "yellow_anime", "aborted_anime", "disabled_anime", "grey_anime", "notbuilt_anime":
		return "RUNNING"
	default: return "UNKNOWN"
	}
}

// --- HTTP Handlers ---

// jobsHandler serves data for the main "All Jobs" list panel. Fetches from DB.
func jobsHandler(w http.ResponseWriter, r *http.Request) {
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
		http.Error(w, "Failed to retrieve jobs", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var responseJobs []JobForListAPIResponse
	for rows.Next() {
		var job JobForListAPIResponse
		var lastBuildTimestampSQL sql.NullInt64
		var lastFetchedAtSQL sql.NullTime
		
		err := rows.Scan(
			&job.Name, &job.Status, &job.URL, &lastFetchedAtSQL,
			&job.LastBuildNumber, &job.LastBuildResult, &lastBuildTimestampSQL,
		)
		if err != nil {
			log.Printf("jobsHandler: Error scanning job row: %v", err)
			http.Error(w, "Failed to process job data", http.StatusInternalServerError)
			return
		}
		if lastBuildTimestampSQL.Valid {
			t := time.Unix(0, lastBuildTimestampSQL.Int64*int64(time.Millisecond)).UTC()
			job.LastBuildTime = &t
		}
		if lastFetchedAtSQL.Valid {
			job.LastFetchedAt = &lastFetchedAtSQL.Time
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

// jobDetailsFromDBHandler serves detailed job information for the detail view panel.
func jobDetailsFromDBHandler(w http.ResponseWriter, r *http.Request) {
	jobName := r.URL.Query().Get("name")
	if jobName == "" { http.Error(w, "Missing job name", http.StatusBadRequest); return }

	// Ensure DB is fresh for this specific job by syncing with Jenkins FIRST
	err := fetchAndStoreJobDetailsFromJenkins(jobName)
	if err != nil {
		log.Printf("jobDetailsFromDBHandler: Error during Jenkins sync for %s: %v. Attempting to serve from DB anyway.", jobName, err)
	}
	
	var apiJobDetail JobAPIDetail // This will hold the response
	var jobLastFetched sql.NullTime

	// Fetch the core Job fields (Name, URL, Color) from 'jobs' table
	// Other fields in embedded JobDetail (like Description, HealthReport) are NOT directly in 'jobs' table.
	// They would have been populated into the DB by fetchAndStoreJobDetailsFromJenkins if that process stored more,
	// or they would be zero-valued in the embedded struct if not found/queried.
	// For simplicity, we are not storing the full JobDetail JSON in the DB.
	err = db.QueryRow("SELECT name, url, color, last_fetched_at FROM jobs WHERE name = $1", jobName).Scan(
		&apiJobDetail.Name, &apiJobDetail.URL, &apiJobDetail.Color, &jobLastFetched,
	)
	if err != nil {
		if err == sql.ErrNoRows { http.Error(w, "Job not found in database", http.StatusNotFound); return }
		log.Printf("Error fetching core job details for %s from DB: %v", jobName, err)
		http.Error(w, "DB error fetching job details", http.StatusInternalServerError); return
	}
	apiJobDetail.DisplayName = apiJobDetail.Name // Default
	if jobLastFetched.Valid {
		apiJobDetail.LastFetchedAt = &jobLastFetched.Time
	}

	// Fetch Builds for this job
	buildRows, err := db.Query(`
		SELECT build_number, url, result, "timestamp", duration 
		FROM builds WHERE job_name = $1 ORDER BY build_number DESC LIMIT 20`, jobName)
	if err != nil {
		log.Printf("Error fetching builds for %s from DB: %v", jobName, err)
		http.Error(w, "DB error fetching builds", http.StatusInternalServerError); return
	}
	defer buildRows.Close()

	var buildsFromDB []Build 
	for buildRows.Next() {
		var b Build
		var buildResult, buildURL sql.NullString
		var buildTimestamp, buildDuration sql.NullInt64
		
		err := buildRows.Scan(&b.Number, &buildURL, &buildResult, &buildTimestamp, &buildDuration)
		if err != nil { log.Printf("Error scanning build for %s: %v", jobName, err); continue }
		
		if buildURL.Valid { b.URL = buildURL.String }
		if buildResult.Valid { b.Result = &buildResult.String } 
		if buildTimestamp.Valid { b.Timestamp = buildTimestamp.Int64 }
		if buildDuration.Valid { b.Duration = buildDuration.Int64 }
		buildsFromDB = append(buildsFromDB, b)
	}
	apiJobDetail.Builds = buildsFromDB
	
	// Note: To get Description and HealthReport, we'd need to either store them expanded in DB
	// or fetch the raw Jenkins JSON again. For now, they'll be empty in the response from this handler
	// if not directly part of the `jobs` table or explicitly queried.
	// The `fetchAndStoreJobDetailsFromJenkins` updates them in DB if that func stored them.

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(apiJobDetail)
}

// --- STATS HANDLERS (Now with DB Queries) ---
func statsSummaryHandler(w http.ResponseWriter, r *http.Request) {
	var totalJobs, runningJobs int
	var successRateDay sql.NullFloat64 // Use NullFloat64 for safety

	err := db.QueryRow("SELECT COUNT(*) FROM jobs").Scan(&totalJobs)
	if err != nil { log.Printf("statsSummary: DB err totalJobs: %v", err); http.Error(w, "DB err", 500); return }

	err = db.QueryRow("SELECT COUNT(*) FROM jobs WHERE status = 'RUNNING'").Scan(&runningJobs)
	if err != nil { log.Printf("statsSummary: DB err runningJobs: %v", err); http.Error(w, "DB err", 500); return }

	// Success rate for builds created "today" (based on server's current date)
	// Timestamps in DB are Unix ms.
	// We need to convert current time to Unix ms for the start of the day.
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	startOfDayMs := startOfDay.UnixNano() / int64(time.Millisecond)

	err = db.QueryRow(`
		SELECT CASE WHEN COUNT(*) = 0 THEN NULL 
		            ELSE CAST(SUM(CASE WHEN result = 'SUCCESS' THEN 1 ELSE 0 END) AS FLOAT) * 100.0 / COUNT(*) 
		       END
		FROM builds
		WHERE "timestamp" >= $1
	`, startOfDayMs).Scan(&successRateDay)
	if err != nil && err != sql.ErrNoRows { // ErrNoRows is fine if scan target is nullable
		log.Printf("statsSummary: DB err successRate: %v", err)
		http.Error(w, "DB err", 500); return
	}

	response := StatsSummaryAPIResponse{
		TotalJobs:   totalJobs,
		RunningJobs: runningJobs,
	}
	if successRateDay.Valid {
		response.SuccessRateDay = successRateDay.Float64
	} else {
		response.SuccessRateDay = 0 // Or handle as "N/A" in frontend if 0 is ambiguous
	}

	log.Println("API: /api/stats/summary served from DB")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func buildHistoryHandler(w http.ResponseWriter, r *http.Request) {
	daysRangeStr := r.URL.Query().Get("range")
	daysRange := 7 // Default to 7 days
	if daysRangeStr != "" {
		parsedDays, err := strconv.Atoi(daysRangeStr)
		if err == nil && parsedDays > 0 && parsedDays <= 90 { // Max 90 days for sanity
			daysRange = parsedDays
		}
	}

	// Calculate the start timestamp for the range
	startTime := time.Now().AddDate(0, 0, -daysRange)
	// Get the Unix millisecond timestamp for the start of that day
	startRangeMs := time.Date(startTime.Year(), startTime.Month(), startTime.Day(), 0, 0, 0, 0, startTime.Location()).UnixNano() / int64(time.Millisecond)

	query := `
		SELECT 
			TO_CHAR(TO_TIMESTAMP("timestamp" / 1000), 'YYYY-MM-DD') as build_date,
			COUNT(*) as total_builds,
			SUM(CASE WHEN result = 'SUCCESS' THEN 1 ELSE 0 END) as successful_builds,
			SUM(CASE WHEN result = 'FAILURE' THEN 1 ELSE 0 END) as failed_builds
		FROM builds
		WHERE "timestamp" >= $1
		GROUP BY build_date
		ORDER BY build_date ASC;
	`
	rows, err := db.Query(query, startRangeMs)
	if err != nil {
		log.Printf("buildHistoryHandler: DB err: %v", err)
		http.Error(w, "DB error", 500); return
	}
	defer rows.Close()

	var history []BuildHistoryPointAPIResponse
	for rows.Next() {
		var p BuildHistoryPointAPIResponse
		var successful, failed sql.NullInt64 // Results might be null if no builds of that type
		err := rows.Scan(&p.Date, &p.TotalBuilds, &successful, &failed)
		if err != nil { log.Printf("buildHistoryHandler: Scan err: %v", err); continue }
		if successful.Valid { p.SuccessfulBuilds = int(successful.Int64) }
		if failed.Valid { p.FailedBuilds = int(failed.Int64) }
		history = append(history, p)
	}
	if err = rows.Err(); err != nil { log.Printf("buildHistoryHandler: Rows err: %v", err); http.Error(w, "DB processing err", 500); return }

	log.Printf("API: /api/stats/build-history served %d points from DB for last %d days", len(history), daysRange)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}

func recentFailuresHandler(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 5 
	if limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 && parsedLimit <= 20 {
			limit = parsedLimit
		}
	}

	query := `
		SELECT job_name, build_number, url, TO_TIMESTAMP("timestamp" / 1000) as build_time
		FROM builds
		WHERE result = 'FAILURE'
		ORDER BY "timestamp" DESC
		LIMIT $1;
	`
	rows, err := db.Query(query, limit)
	if err != nil { log.Printf("recentFailuresHandler: DB err: %v", err); http.Error(w, "DB err", 500); return }
	defer rows.Close()

	var failures []RecentFailureAPIResponse
	for rows.Next() {
		var f RecentFailureAPIResponse
		var buildTime sql.NullTime // Timestamp from DB might be null, though unlikely for completed builds
		err := rows.Scan(&f.JobName, &f.BuildNumber, &f.BuildURL, &buildTime)
		if err != nil { log.Printf("recentFailuresHandler: Scan err: %v", err); continue }
		if buildTime.Valid { f.Timestamp = buildTime.Time }
		failures = append(failures, f)
	}
	if err = rows.Err(); err != nil { log.Printf("recentFailuresHandler: Rows err: %v", err); http.Error(w, "DB processing err", 500); return }
	
	log.Printf("API: /api/builds/recent-failures served %d failures from DB", len(failures))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(failures)
}


// --- Background Refresh Logic & Jenkins Interaction (mostly same as your last version) ---

// fetchAndStoreJobDetailsFromJenkins fetches details for a single job from Jenkins API and stores them in DB.
func fetchAndStoreJobDetailsFromJenkins(jobName string) error {
	log.Printf("SYNC: Fetching details for job '%s' from Jenkins API for DB update.", jobName)
	fetchURL := fmt.Sprintf("%s/job/%s/api/json?tree=actions,description,displayName,displayNameOrNull,fullDisplayName,fullName,name,url,buildable,builds[_class,number,url,result,timestamp,duration],color,firstBuild[_class,number,url],healthReport[description,iconClassName,iconUrl,score],inQueue,keepDependencies,lastBuild[_class,number,url,result,timestamp,duration],lastCompletedBuild[_class,number,url],lastFailedBuild[_class,number,url],lastStableBuild[_class,number,url],lastSuccessfulBuild[_class,number,url],lastUnstableBuild[_class,number,url],lastUnsuccessfulBuild[_class,number,url],nextBuildNumber,property,queueItem,concurrentBuild,resumeBlocked", jenkinsURL, jobName)
	req, err := http.NewRequest("GET", fetchURL, nil)
	if err != nil { return fmt.Errorf("creating req for %s: %w", jobName, err) }
	if jenkinsUser != "" && jenkinsToken != "" { req.SetBasicAuth(jenkinsUser, jenkinsToken) }
	client := &http.Client{Timeout: 20 * time.Second}; resp, err := client.Do(req)
	if err != nil { return fmt.Errorf("fetching %s from Jenkins: %w", jobName, err) }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK { bodyBytes, _ := io.ReadAll(resp.Body); return fmt.Errorf("jenkins API err for %s (%d): %s", jobName, resp.StatusCode, string(bodyBytes)) }
	bodyBytes, err := io.ReadAll(resp.Body); if err != nil { return fmt.Errorf("reading body for %s: %w", jobName, err) }
	
	var jobDetail JobDetail // From job-details.go
	if err := json.Unmarshal(bodyBytes, &jobDetail); err != nil { return fmt.Errorf("decoding JSON for %s: %w. Body: %s", jobName, err, string(bodyBytes)) }

	tx, err := db.Begin(); if err != nil { return fmt.Errorf("starting tx for %s: %w", jobName, err) }
	jobStatus := mapColorToStatus(jobDetail.Color)
	
	// Upsert into jobs table
	// Here, we are NOT storing jobDetail.Description or jobDetail.HealthReport into the `jobs` table
	// because the table schema doesn't have columns for them. If you want to persist these,
	// you'd need to alter the `jobs` table or create a related table.
	_, err = tx.Exec(`
        INSERT INTO jobs (name, url, color, status, last_fetched_at)
        VALUES ($1, $2, $3, $4, $5) ON CONFLICT (name) DO UPDATE SET 
        url = EXCLUDED.url, color = EXCLUDED.color, status = EXCLUDED.status, last_fetched_at = EXCLUDED.last_fetched_at;
    `, jobDetail.Name, jobDetail.URL, jobDetail.Color, jobStatus, time.Now())
	if err != nil { tx.Rollback(); return fmt.Errorf("upserting job %s: %w", jobName, err) }
	
	buildStmt, err := tx.Prepare(`
        INSERT INTO builds (job_name, build_number, url, result, "timestamp", duration, fetched_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (job_name, build_number) DO UPDATE SET
        url = EXCLUDED.url, result = EXCLUDED.result, "timestamp" = EXCLUDED."timestamp", duration = EXCLUDED.duration, fetched_at = EXCLUDED.fetched_at;
    `); if err != nil { tx.Rollback(); return fmt.Errorf("preparing build stmt for %s: %w", jobName, err) }
	defer buildStmt.Close()
	
	for _, build := range jobDetail.Builds { // build.Result is *string
		var dbBuildResult sql.NullString
		if build.Result != nil { dbBuildResult.String = *build.Result; dbBuildResult.Valid = true } else { dbBuildResult.Valid = false }
		_, err := buildStmt.Exec(jobDetail.Name, build.Number, build.URL, dbBuildResult, build.Timestamp, build.Duration, time.Now())
		if err != nil { tx.Rollback(); return fmt.Errorf("upserting build #%d for %s: %w", build.Number, jobName, err) }
	}
	if err := tx.Commit(); err != nil { return fmt.Errorf("committing tx for %s: %w", jobName, err) }
	log.Printf("SYNC: Successfully updated details for job '%s' in DB from Jenkins API.", jobName)
	return nil
}

func fetchAndStoreAllJobsFromJenkinsAPI() ([]string, error) {
	log.Println("Background SYNC: Fetching all jobs list from Jenkins API for DB update...")
	fetchedJenkinsJobs, err := fetchJobsFromJenkinsAPI()
	if err != nil { return nil, fmt.Errorf("background fetchJobsFromJenkinsAPI failed: %w", err) }
	
	var jobNames []string; tx, err := db.Begin(); if err != nil { return nil, fmt.Errorf("bg: starting tx for all jobs: %w", err) }
	stmt, err := tx.Prepare(`INSERT INTO jobs (name, url, color, status, last_fetched_at) VALUES ($1, $2, $3, $4, $5)
        ON CONFLICT (name) DO UPDATE SET url = EXCLUDED.url, color = EXCLUDED.color, status = EXCLUDED.status, last_fetched_at = EXCLUDED.last_fetched_at;`)
	if err != nil { tx.Rollback(); return nil, fmt.Errorf("bg: preparing jobs upsert: %w", err) }
	defer stmt.Close()
	
	for _, job := range fetchedJenkinsJobs {
		jobNames = append(jobNames, job.Name); status := mapColorToStatus(job.Color)
		_, err := stmt.Exec(job.Name, job.URL, job.Color, status, time.Now())
		if err != nil { tx.Rollback(); return nil, fmt.Errorf("bg: upserting job %s: %w", job.Name, err) }
	}
	if err := tx.Commit(); err != nil { return nil, fmt.Errorf("bg: committing tx for all jobs: %w", err) }
	log.Printf("Background SYNC: Successfully updated/stored %d jobs in the DB from Jenkins API.", len(jobNames))
	return jobNames, nil
}

func StartBackgroundRefresher() {
	log.Printf("Starting background Jenkins data refresher every %v", backgroundRefreshInterval)
	go func() {
		performFullRefresh()
		ticker := time.NewTicker(backgroundRefreshInterval)
		defer ticker.Stop()
		for range ticker.C { performFullRefresh() }
	}()
}

func performFullRefresh() {
	log.Println("Background Refresher: Starting full data refresh cycle.")
	jobNames, err := fetchAndStoreAllJobsFromJenkinsAPI()
	if err != nil { log.Printf("Background Refresher: Error fetching and storing all jobs from Jenkins API: %v", err); return }
	if len(jobNames) == 0 { log.Println("Background Refresher: No jobs found to refresh details for."); return }
	
	log.Printf("Background Refresher: Will attempt to refresh details for %d jobs from Jenkins API.", len(jobNames))
	var wg sync.WaitGroup; concurrencyLimit := 3; semaphore := make(chan struct{}, concurrencyLimit)
	for _, name := range jobNames {
		wg.Add(1); semaphore <- struct{}{}
		go func(jobName string) {
			defer wg.Done(); defer func() { <-semaphore }()
			if err := fetchAndStoreJobDetailsFromJenkins(jobName); err != nil {
				log.Printf("Background Refresher: Error fetching/storing details for job %s from Jenkins API: %v", jobName, err)
			}
		}(name)
	}
	wg.Wait(); log.Println("Background Refresher: Full data refresh cycle completed.")
}

// originalJobDetailHandlerFromJenkins fetches live from Jenkins, updates DB, and returns full Jenkins payload.
func originalJobDetailHandlerFromJenkins(w http.ResponseWriter, r *http.Request) {
	jobName := r.URL.Query().Get("name")
	if jobName == "" { http.Error(w, "Missing job name", http.StatusBadRequest); return }
	log.Printf("API /api/job (original Jenkins sync): Fetching live details for %s from Jenkins.", jobName)
	
	fetchURL := fmt.Sprintf("%s/job/%s/api/json?tree=actions,description,displayName,displayNameOrNull,fullDisplayName,fullName,name,url,buildable,builds[_class,number,url,result,timestamp,duration],color,firstBuild[_class,number,url],healthReport[description,iconClassName,iconUrl,score],inQueue,keepDependencies,lastBuild[_class,number,url,result,timestamp,duration],lastCompletedBuild[_class,number,url],lastFailedBuild[_class,number,url],lastStableBuild[_class,number,url],lastSuccessfulBuild[_class,number,url],lastUnstableBuild[_class,number,url],lastUnsuccessfulBuild[_class,number,url],nextBuildNumber,property,queueItem,concurrentBuild,resumeBlocked", jenkinsURL, jobName)
	req, err := http.NewRequest("GET", fetchURL, nil)
	if err != nil { log.Printf("API /api/job: Error creating req for %s: %v", jobName, err); http.Error(w, "Req creation error", 500); return}
	if jenkinsUser != "" && jenkinsToken != "" { req.SetBasicAuth(jenkinsUser, jenkinsToken) }
	
	client := &http.Client{Timeout: 20 * time.Second}; resp, err := client.Do(req)
	if err != nil { log.Printf("API /api/job: Error fetching %s: %v", jobName, err); http.Error(w, "Jenkins fetch error", 500); return}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK { 
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("API /api/job: Jenkins API err %s (%d): %s", jobName, resp.StatusCode, string(bodyBytes))
		http.Error(w, "Jenkins API error: "+string(bodyBytes), resp.StatusCode)
		return 
	}
	bodyBytes, err := io.ReadAll(resp.Body); 
	if err != nil { log.Printf("API /api/job: Error reading body for %s: %v", jobName, err); http.Error(w, "Response read error", 500); return}

	// Asynchronously update DB with this fresh data
	go func(name string) {
		// This re-fetches and stores. Could be optimized to pass bodyBytes if confident.
		errDb := fetchAndStoreJobDetailsFromJenkins(name) 
		if errDb != nil {
			log.Printf("API /api/job: Async DB update failed for %s after successful Jenkins fetch: %v", name, errDb)
		}
	}(jobName)

	w.Header().Set("Content-Type", "application/json")
	w.Write(bodyBytes)
}