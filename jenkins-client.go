package main

import (
    "encoding/json"
    "fmt"
    "net/http"
)

type JenkinsJob struct {
    Name  string `json:"name"`
    URL   string `json:"url"`
    Color string `json:"color"`
}

type JenkinsResponse struct {
    Jobs []JenkinsJob `json:"jobs"`
}

func fetchJobs() ([]JenkinsJob, error) {
    url := "http://localhost:8080/api/json"
    req, _ := http.NewRequest("GET", url, nil)
    req.SetBasicAuth("diya", "diya")
    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    var jenkins JenkinsResponse
    err = json.NewDecoder(resp.Body).Decode(&jenkins)
    return jenkins.Jobs, err
}

func jobsHandler(w http.ResponseWriter, r *http.Request) {
    jobs, err := fetchJobs()
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    var response []struct {
        Name   string `json:"name"`
        Status string `json:"status"`
        URL    string `json:"url"`
    }

    for _, job := range jobs {
        status := ""
        switch job.Color {
        case "blue":
            status = "SUCCESS"
        case "red":
            status = "FAILURE"
        case "blue_anime", "red_anime":
            status = "RUNNING"
        default:
            status = "DISABLED"
        }
        response = append(response, struct {
            Name   string `json:"name"`
            Status string `json:"status"`
            URL    string `json:"url"`
        }{
            Name:   job.Name,
            Status: status,
            URL:    job.URL,
        })
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(response)
}

func jobDetailHandler(w http.ResponseWriter, r *http.Request) {
    name := r.URL.Query().Get("name")
    if name == "" {
        http.Error(w, "Missing job name", http.StatusBadRequest)
        return
    }

    url := fmt.Sprintf("http://localhost:8080/job/%s/api/json", name)
    req, _ := http.NewRequest("GET", url, nil)
    req.SetBasicAuth("diya", "diya")
    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    defer resp.Body.Close()

    var jobDetail JobDetail
    err = json.NewDecoder(resp.Body).Decode(&jobDetail)
    if err != nil {
        http.Error(w, "Failed to decode Jenkins response: "+err.Error(), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    err = json.NewEncoder(w).Encode(jobDetail)
    if err != nil {
        http.Error(w, "Failed to encode response: "+err.Error(), http.StatusInternalServerError)
    }
}
