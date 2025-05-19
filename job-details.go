package main

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
