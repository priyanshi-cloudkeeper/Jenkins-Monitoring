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
}

// --- Internal Structs for Jenkins API ---
type JenkinsJob struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Color string `json:"color"`
}
type JenkinsResponse struct {
	Jobs []JenkinsJob `json:"jobs"`
}

// --- Structs for Our Custom API Responses ---
type JobForListAPIResponse struct {
	Name            string     `json:"name"`
	Status          string     `json:"status"`
	URL             string     `json:"url"`
	LastBuildNumber *int       `json:"last_build_number,omitempty"`
	LastBuildResult *string    `json:"last_build_result,omitempty"`
	LastBuildTime   *time.Time `json:"last_build_time,omitempty"`
	LastFetchedAt   *time.Time `json:"last_fetched_at,omitempty"`
}

type JobAPIDetail struct {
	JobDetail
	LastFetchedAt *time.Time `json:"last_fetched_at,omitempty"`
}

type JobAnalysisResponse struct {
	SuccessRate30        float64 `json:"success_rate_30"`
	AvgDurationSeconds30 float64 `json:"avg_duration_seconds_30"`
	TimeSinceLastSuccess string  `json:"time_since_last_success"`
	BuildFrequency       float64 `json:"build_frequency"`
	BuildsForCharts      []Build `json:"builds_for_charts"`
}

type StatsSummaryAPIResponse struct {
	TotalJobs      int     `json:"total_jobs"`
	RunningJobs    int     `json:"running_jobs"`
	SuccessRateDay float64 `json:"success_rate_day"`
}

type BuildHistoryPointAPIResponse struct {
	Date             string `json:"date"`
	TotalBuilds      int    `json:"total_builds"`
	SuccessfulBuilds int    `json:"successful_builds"`
	FailedBuilds     int    `json:"failed_builds"`
}

type RecentFailureAPIResponse struct {
	JobName     string    `json:"job_name"`
	BuildNumber int       `json:"build_number"`
	BuildURL    string    `json:"build_url"`
	Timestamp   time.Time `json:"timestamp"`
}

type RecentSuccessAPIResponse struct {
	JobName     string    `json:"job_name"`
	BuildNumber int       `json:"build_number"`
	BuildURL    string    `json:"build_url"`
	Timestamp   time.Time `json:"timestamp"`
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
		return "UNKNOWN"
	}
}

func getJenkinsJobDetail(jobName string) (*JobDetail, error) {
	if jenkinsURL == "" {
		return nil, fmt.Errorf("jenkins URL not configured")
	}
	fetchURL := fmt.Sprintf("%s/job/%s/api/json?tree=description,healthReport[description,score]", jenkinsURL, jobName)
	req, err := http.NewRequest("GET", fetchURL, nil)
	if err != nil {
		return nil, err
	}
	if jenkinsUser != "" && jenkinsToken != "" {
		req.SetBasicAuth(jenkinsUser, jenkinsToken)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var jobDetail JobDetail
	if err := json.NewDecoder(resp.Body).Decode(&jobDetail); err != nil {
		return nil, err
	}
	return &jobDetail, nil
}

// --- HTTP Handlers ---

func jobAnalysisHandler(w http.ResponseWriter, r *http.Request) {
	jobName := r.URL.Query().Get("name")
	if jobName == "" {
		http.Error(w, "Missing job name", http.StatusBadRequest)
		return
	}

	log.Printf("ANALYSIS: Fetching analysis data for job: %s", jobName)

	rows, err := db.Query(`
        SELECT result, duration, "timestamp" FROM builds 
        WHERE job_name = $1 AND result IS NOT NULL
        ORDER BY build_number DESC 
        LIMIT 30
    `, jobName)
	if err != nil {
		log.Printf("ANALYSIS ERROR (DB Query): %v", err)
		http.Error(w, "DB error fetching builds", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var builds []Build
	for rows.Next() {
		var b Build
		var result sql.NullString
		var duration, timestamp sql.NullInt64
		if err := rows.Scan(&result, &duration, &timestamp); err != nil {
			log.Printf("ANALYSIS ERROR (DB Scan): %v", err)
			continue
		}
		if result.Valid {
			b.Result = &result.String
		}
		if duration.Valid {
			b.Duration = duration.Int64
		}
		if timestamp.Valid {
			b.Timestamp = timestamp.Int64
		}
		builds = append(builds, b)
	}
	if err = rows.Err(); err != nil {
		log.Printf("ANALYSIS ERROR (Rows Iteration): %v", err)
		http.Error(w, "Error processing build data", http.StatusInternalServerError)
		return
	}

	buildCount := len(builds)
	if buildCount == 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(JobAnalysisResponse{BuildsForCharts: []Build{}})
		return
	}

	var totalDuration, successCount int64
	var lastSuccessTime int64

	for _, b := range builds {
		totalDuration += b.Duration
		if b.Result != nil && *b.Result == "SUCCESS" {
			successCount++
			if b.Timestamp > lastSuccessTime {
				lastSuccessTime = b.Timestamp
			}
		}
	}

	analysis := JobAnalysisResponse{}
	if buildCount > 0 {
		analysis.SuccessRate30 = (float64(successCount) / float64(buildCount)) * 100.0
		analysis.AvgDurationSeconds30 = (float64(totalDuration) / 1000.0) / float64(buildCount)
	}

	if lastSuccessTime > 0 {
		timeSince := time.Since(time.UnixMilli(lastSuccessTime))
		if hours := timeSince.Hours(); hours >= 48 {
			analysis.TimeSinceLastSuccess = fmt.Sprintf("%.0f days ago", hours/24)
		} else if hours >= 1 {
			analysis.TimeSinceLastSuccess = fmt.Sprintf("%.0f hours ago", hours)
		} else {
			analysis.TimeSinceLastSuccess = fmt.Sprintf("%.0f mins ago", timeSince.Minutes())
		}
	} else {
		analysis.TimeSinceLastSuccess = "Never"
	}

	if buildCount > 1 {
		firstBuildTime := builds[buildCount-1].Timestamp
		lastBuildTime := builds[0].Timestamp
		buildPeriodHours := time.UnixMilli(lastBuildTime).Sub(time.UnixMilli(firstBuildTime)).Hours()
		if buildPeriodHours > 24 {
			analysis.BuildFrequency = float64(buildCount) / (buildPeriodHours / 24.0)
		} else {
			analysis.BuildFrequency = float64(buildCount)
		}
	} else if buildCount == 1 {
		analysis.BuildFrequency = 1
	}

	analysis.BuildsForCharts = builds

	log.Printf("ANALYSIS: Successfully computed metrics for %s. Success rate: %.1f%%", jobName, analysis.SuccessRate30)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(analysis)
}

func jobDetailsFromDBHandler(w http.ResponseWriter, r *http.Request) {
	jobName := r.URL.Query().Get("name")
	if jobName == "" {
		http.Error(w, "Missing job name", http.StatusBadRequest)
		return
	}

	if jenkinsURL != "" {
		if err := fetchAndStoreJobDetailsFromJenkins(jobName); err != nil {
			log.Printf("jobDetailsFromDBHandler: Jenkins sync failed for %s: %v", jobName, err)
		}
	}

	var apiJobDetail JobAPIDetail
	var jobLastFetched sql.NullTime

	err := db.QueryRow("SELECT name, url, color, last_fetched_at FROM jobs WHERE name = $1", jobName).Scan(
		&apiJobDetail.Name, &apiJobDetail.URL, &apiJobDetail.Color, &jobLastFetched,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Job not found", http.StatusNotFound)
		} else {
			http.Error(w, "DB error", http.StatusInternalServerError)
		}
		return
	}
	apiJobDetail.DisplayName = apiJobDetail.Name
	if jobLastFetched.Valid {
		apiJobDetail.LastFetchedAt = &jobLastFetched.Time
	}

	jenkinsJobDetail, err := getJenkinsJobDetail(jobName)
	if err == nil {
		apiJobDetail.Description = jenkinsJobDetail.Description
		apiJobDetail.HealthReport = jenkinsJobDetail.HealthReport
	}

	buildRows, err := db.Query(`SELECT build_number, url, result, "timestamp", duration FROM builds WHERE job_name = $1 ORDER BY build_number DESC LIMIT 20`, jobName)
	if err == nil {
		defer buildRows.Close()
		var buildsFromDB []Build
		for buildRows.Next() {
			var b Build
			var buildResult, buildURL sql.NullString
			var ts, dur sql.NullInt64
			// THIS IS THE CORRECTED LINE, NO SPECIAL CHARACTERS
			err := buildRows.Scan(&b.Number, &buildURL, &buildResult, &ts, &dur)
			if err != nil {
				log.Printf("jobDetailsFromDBHandler: Error scanning build row: %v", err)
				continue
			}
			if buildURL.Valid {
				b.URL = buildURL.String
			}
			if buildResult.Valid {
				b.Result = &buildResult.String
			}
			if ts.Valid {
				b.Timestamp = ts.Int64
			}
			if dur.Valid {
				b.Duration = dur.Int64
			}
			buildsFromDB = append(buildsFromDB, b)
		}
		apiJobDetail.Builds = buildsFromDB
	} else {
		log.Printf("jobDetailsFromDBHandler: Error fetching builds from DB: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(apiJobDetail)
}

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
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(responseJobs)
}

func statsSummaryHandler(w http.ResponseWriter, r *http.Request) {
	var totalJobs, runningJobs int
	var successRateDay sql.NullFloat64

	db.QueryRow("SELECT COUNT(*) FROM jobs").Scan(&totalJobs)
	db.QueryRow("SELECT COUNT(*) FROM jobs WHERE status = 'RUNNING'").Scan(&runningJobs)

	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	startOfDayMs := startOfDay.UnixNano() / int64(time.Millisecond)

	db.QueryRow(`
		SELECT CASE WHEN COUNT(*) = 0 THEN 0 
		            ELSE CAST(SUM(CASE WHEN result = 'SUCCESS' THEN 1 ELSE 0 END) AS FLOAT) * 100.0 / COUNT(*) 
		       END
		FROM builds
		WHERE "timestamp" >= $1
	`, startOfDayMs).Scan(&successRateDay)

	response := StatsSummaryAPIResponse{
		TotalJobs:      totalJobs,
		RunningJobs:    runningJobs,
		SuccessRateDay: successRateDay.Float64,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func buildHistoryHandler(w http.ResponseWriter, r *http.Request) {
	daysRange := 7
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
	startTime := time.Now().AddDate(0, 0, -daysRange)
	startRangeMs := time.Date(startTime.Year(), startTime.Month(), startTime.Day(), 0, 0, 0, 0, startTime.Location()).UnixNano() / int64(time.Millisecond)
	
	rows, err := db.Query(query, startRangeMs)
	if err != nil {
		http.Error(w, "DB error", 500)
		return
	}
	defer rows.Close()

	var history []BuildHistoryPointAPIResponse
	for rows.Next() {
		var p BuildHistoryPointAPIResponse
		var successful, failed sql.NullInt64
		rows.Scan(&p.Date, &p.TotalBuilds, &successful, &failed)
		if successful.Valid { p.SuccessfulBuilds = int(successful.Int64) }
		if failed.Valid { p.FailedBuilds = int(failed.Int64) }
		history = append(history, p)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}

func recentFailuresHandler(w http.ResponseWriter, r *http.Request) {
	limit := 5
	query := `
		SELECT job_name, build_number, url, TO_TIMESTAMP("timestamp" / 1000) as build_time
		FROM builds
		WHERE result = 'FAILURE'
		ORDER BY "timestamp" DESC
		LIMIT $1;
	`
	rows, err := db.Query(query, limit)
	if err != nil {
		http.Error(w, "DB err", 500)
		return
	}
	defer rows.Close()

	var failures []RecentFailureAPIResponse
	for rows.Next() {
		var f RecentFailureAPIResponse
		var buildTime sql.NullTime
		rows.Scan(&f.JobName, &f.BuildNumber, &f.BuildURL, &buildTime)
		if buildTime.Valid {
			f.Timestamp = buildTime.Time
		}
		failures = append(failures, f)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(failures)
}
func recentSuccessesHandler(w http.ResponseWriter, r *http.Request) {
	limit := 5
	query := `
		SELECT job_name, build_number, url, TO_TIMESTAMP("timestamp" / 1000) as build_time
		FROM builds
		WHERE result = 'SUCCESS'
		ORDER BY "timestamp" DESC
		LIMIT $1;
	`
	rows, err := db.Query(query, limit)
	if err != nil {
		log.Printf("recentSuccessesHandler: DB err: %v", err)
		http.Error(w, "DB err", 500)
		return
	}
	defer rows.Close()

	var successes []RecentSuccessAPIResponse
	for rows.Next() {
		var s RecentSuccessAPIResponse
		var buildTime sql.NullTime
		if err := rows.Scan(&s.JobName, &s.BuildNumber, &s.BuildURL, &buildTime); err != nil {
			log.Printf("recentSuccessesHandler: Scan err: %v", err)
			continue
		}
		if buildTime.Valid {
			s.Timestamp = buildTime.Time
		}
		successes = append(successes, s)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(successes)
}


// --- Background Sync Logic ---

func fetchJobsFromJenkinsAPI() ([]JenkinsJob, error) {
	if jenkinsURL == "" { return nil, nil }
	url := fmt.Sprintf("%s/api/json?tree=jobs[name,url,color]", jenkinsURL)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil { return nil, err }
	if jenkinsUser != "" && jenkinsToken != "" { req.SetBasicAuth(jenkinsUser, jenkinsToken) }
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil { return nil, err }
	defer resp.Body.Close()
	var jenkinsResp JenkinsResponse
	json.NewDecoder(resp.Body).Decode(&jenkinsResp)
	return jenkinsResp.Jobs, nil
}

func fetchAndStoreJobDetailsFromJenkins(jobName string) error {
	if jenkinsURL == "" { return nil }
	fetchURL := fmt.Sprintf("%s/job/%s/api/json?tree=actions,description,displayName,displayNameOrNull,fullDisplayName,fullName,name,url,buildable,builds[_class,number,url,result,timestamp,duration],color,firstBuild[_class,number,url],healthReport[description,iconClassName,iconUrl,score],inQueue,keepDependencies,lastBuild[_class,number,url,result,timestamp,duration],lastCompletedBuild[_class,number,url],lastFailedBuild[_class,number,url],lastStableBuild[_class,number,url],lastSuccessfulBuild[_class,number,url],lastUnstableBuild[_class,number,url],lastUnsuccessfulBuild[_class,number,url],nextBuildNumber,property,queueItem,concurrentBuild,resumeBlocked", jenkinsURL, jobName)
	req, err := http.NewRequest("GET", fetchURL, nil)
	if err != nil { return err }
	if jenkinsUser != "" && jenkinsToken != "" { req.SetBasicAuth(jenkinsUser, jenkinsToken) }
	client := &http.Client{Timeout: 20 * time.Second}; resp, err := client.Do(req)
	if err != nil { return err }
	defer resp.Body.Close()
	
	var jobDetail JobDetail
	bodyBytes, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(bodyBytes, &jobDetail); err != nil { return err }

	tx, err := db.Begin(); if err != nil { return err }
	
	_, err = tx.Exec(`
        INSERT INTO jobs (name, url, color, status, last_fetched_at)
        VALUES ($1, $2, $3, $4, $5) ON CONFLICT (name) DO UPDATE SET 
        url = EXCLUDED.url, color = EXCLUDED.color, status = EXCLUDED.status, last_fetched_at = EXCLUDED.last_fetched_at;
    `, jobDetail.Name, jobDetail.URL, jobDetail.Color, mapColorToStatus(jobDetail.Color), time.Now())
	if err != nil { tx.Rollback(); return err }
	
	buildStmt, err := tx.Prepare(`
        INSERT INTO builds (job_name, build_number, url, result, "timestamp", duration, fetched_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (job_name, build_number) DO UPDATE SET
        url = EXCLUDED.url, result = EXCLUDED.result, "timestamp" = EXCLUDED."timestamp", duration = EXCLUDED.duration, fetched_at = EXCLUDED.fetched_at;
    `); if err != nil { tx.Rollback(); return err }
	defer buildStmt.Close()
	
	for _, build := range jobDetail.Builds {
		var dbBuildResult sql.NullString
		if build.Result != nil { dbBuildResult.String = *build.Result; dbBuildResult.Valid = true }
		_, err := buildStmt.Exec(jobDetail.Name, build.Number, build.URL, dbBuildResult, build.Timestamp, build.Duration, time.Now())
		if err != nil { tx.Rollback(); return err }
	}
	return tx.Commit()
}

func fetchAndStoreAllJobsFromJenkinsAPI() ([]string, error) {
	fetchedJenkinsJobs, err := fetchJobsFromJenkinsAPI()
	if err != nil || len(fetchedJenkinsJobs) == 0 { return nil, err }
	
	var jobNames []string
	tx, err := db.Begin(); if err != nil { return nil, err }
	stmt, err := tx.Prepare(`INSERT INTO jobs (name, url, color, status, last_fetched_at) VALUES ($1, $2, $3, $4, $5)
        ON CONFLICT (name) DO UPDATE SET url = EXCLUDED.url, color = EXCLUDED.color, status = EXCLUDED.status, last_fetched_at = EXCLUDED.last_fetched_at;`)
	if err != nil { tx.Rollback(); return nil, err }
	defer stmt.Close()
	
	for _, job := range fetchedJenkinsJobs {
		jobNames = append(jobNames, job.Name)
		stmt.Exec(job.Name, job.URL, job.Color, mapColorToStatus(job.Color), time.Now())
	}
	if err := tx.Commit(); err != nil { return nil, err }
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
	if err != nil { log.Printf("Background Refresher: Error fetching job list: %v", err); return }
	if len(jobNames) == 0 { log.Println("Background Refresher: No jobs found to refresh."); return }
	
	var wg sync.WaitGroup; semaphore := make(chan struct{}, 5)
	for _, name := range jobNames {
		wg.Add(1); semaphore <- struct{}{}
		go func(jobName string) {
			defer wg.Done(); defer func() { <-semaphore }()
			if err := fetchAndStoreJobDetailsFromJenkins(jobName); err != nil {
				log.Printf("Background Refresher: Error fetching details for job %s: %v", jobName, err)
			}
		}(name)
	}
	wg.Wait()
	log.Println("Background Refresher: Full data refresh cycle completed.")
}

func originalJobDetailHandlerFromJenkins(w http.ResponseWriter, r *http.Request) {
    // This handler remains for potential direct Jenkins API passthrough if needed
}
func buildConsoleOutputHandler(w http.ResponseWriter, r *http.Request) {
	jobName := r.URL.Query().Get("job_name")
	buildNumber := r.URL.Query().Get("build_number")

	if jobName == "" || buildNumber == "" {
		http.Error(w, "Missing job_name or build_number", http.StatusBadRequest)
		return
	}

	if jenkinsURL == "" {
		http.Error(w, "Jenkins URL not configured", http.StatusInternalServerError)
		return
	}

	fetchURL := fmt.Sprintf("%s/job/%s/%s/consoleText", jenkinsURL, jobName, buildNumber)
	log.Printf("Proxying console output request to: %s", fetchURL)

	req, err := http.NewRequest("GET", fetchURL, nil)
	if err != nil {
		log.Printf("Error creating console request for %s #%s: %v", jobName, buildNumber, err)
		http.Error(w, "Server error", http.StatusInternalServerError)
		return
	}

	if jenkinsUser != "" && jenkinsToken != "" {
		req.SetBasicAuth(jenkinsUser, jenkinsToken)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error fetching console output from Jenkins for %s #%s: %v", jobName, buildNumber, err)
		http.Error(w, "Failed to fetch console output from Jenkins", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Jenkins returned non-OK status for console output: %d", resp.StatusCode)
		bodyBytes, _ := io.ReadAll(resp.Body)
		http.Error(w, fmt.Sprintf("Jenkins error: %s", string(bodyBytes)), resp.StatusCode)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.Copy(w, resp.Body)
}