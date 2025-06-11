package main
type Build struct {
	Class     string         `json:"_class,omitempty"` 
	Number    int            `json:"number"`
	URL       string         `json:"url"`
	Result    *string        `json:"result,omitempty"`   
	Timestamp int64          `json:"timestamp,omitempty"` 
	Duration  int64          `json:"duration,omitempty"`  
}


type HealthReport struct {
	Description   string `json:"description,omitempty"`
	IconClassName string `json:"iconClassName,omitempty"`
	IconURL       string `json:"iconUrl,omitempty"`
	Score         int    `json:"score,omitempty"`
}

type JobDetail struct {
	Class               string         `json:"_class,omitempty"`
	Actions             []interface{}  `json:"actions,omitempty"` 
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
	LastUnstableBuild   *Build         `json:"lastUnstableBuild,omitempty"`
	LastUnsuccessfulBuild *Build     `json:"lastUnsuccessfulBuild,omitempty"` 
	NextBuildNumber     int            `json:"nextBuildNumber"`
	Property            []interface{}  `json:"property,omitempty"` 
	QueueItem           *string        `json:"queueItem"`          
	ConcurrentBuild     bool           `json:"concurrentBuild"`
	ResumeBlocked       bool           `json:"resumeBlocked"`
}
