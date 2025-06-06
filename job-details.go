package main

//import "database/sql" // For sql.NullString if needed by any struct here

// Build represents a single build of a Jenkins job.
type Build struct {
	Class     string         `json:"_class,omitempty"` // omitempty for fields that might not always be present
	Number    int            `json:"number"`
	URL       string         `json:"url"`
	Result    *string        `json:"result,omitempty"`    // Jenkins API field: result (e.g., SUCCESS, FAILURE)
	Timestamp int64          `json:"timestamp,omitempty"` // Jenkins API field: timestamp (milliseconds)
	Duration  int64          `json:"duration,omitempty"`  // Jenkins API field: duration (milliseconds)
}

// HealthReport describes the health status of a Jenkins job.
type HealthReport struct {
	Description   string `json:"description,omitempty"`
	IconClassName string `json:"iconClassName,omitempty"`
	IconURL       string `json:"iconUrl,omitempty"`
	Score         int    `json:"score,omitempty"`
}

// JobDetail represents the detailed structure from Jenkins API for a single job.
// This is what the /job/{name}/api/json endpoint typically returns.
type JobDetail struct {
	Class               string         `json:"_class,omitempty"`
	Actions             []interface{}  `json:"actions,omitempty"` // Can be more specific if needed
	Description         string         `json:"description,omitempty"`
	DisplayName         string         `json:"displayName"`
	DisplayNameOrNull   *string        `json:"displayNameOrNull"`
	FullDisplayName     string         `json:"fullDisplayName"`
	FullName            string         `json:"fullName"`
	Name                string         `json:"name"`
	URL                 string         `json:"url"`
	Buildable           bool           `json:"buildable"`
	Builds              []Build        `json:"builds"` // Array of build details
	Color               string         `json:"color"`
	FirstBuild          *Build         `json:"firstBuild,omitempty"`
	HealthReport        []HealthReport `json:"healthReport,omitempty"`
	InQueue             bool           `json:"inQueue"`
	KeepDependencies    bool           `json:"keepDependencies"`
	LastBuild           *Build         `json:"lastBuild,omitempty"`
	LastCompletedBuild  *Build         `json:"lastCompletedBuild,omitempty"`
	LastFailedBuild     *Build         `json:"lastFailedBuild,omitempty"`
	LastStableBuild     *Build         `json:"lastStableBuild,omitempty"`
	LastSuccessfulBuild *Build         `json:"lastSuccessfulBuild,omitempty"`
	LastUnstableBuild   *Build         `json:"lastUnstableBuild,omitempty"` // Might be null
	LastUnsuccessfulBuild *Build     `json:"lastUnsuccessfulBuild,omitempty"` // Might be null
	NextBuildNumber     int            `json:"nextBuildNumber"`
	Property            []interface{}  `json:"property,omitempty"` // Can be more specific
	QueueItem           *string        `json:"queueItem"`          // Might be null
	ConcurrentBuild     bool           `json:"concurrentBuild"`
	ResumeBlocked       bool           `json:"resumeBlocked"`
}
