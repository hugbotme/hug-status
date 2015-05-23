package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"golang.org/x/oauth2"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/google/go-github/github"
	"github.com/hugbotme/hug-status/config"
)

var (
	flagConfigFile *string
	flagPidFile    *string
	flagVersion    *bool
)

const (
	majorVersion = 1
	minorVersion = 0
	patchVersion = 0

	waitTime = 30
)

// Init function to define arguments
func init() {
	flagConfigFile = flag.String("config", "", "Configuration file")
	flagPidFile = flag.String("pidfile", "", "Write the process id into a given file")
	flagVersion = flag.Bool("version", false, "Outputs the version number and exits")
}

func GitHubClient(access_token string) *github.Client {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: access_token},
	)
	tc := oauth2.NewClient(oauth2.NoContext, ts)

	return github.NewClient(tc)
}

func GitHubPRStatus(gh *github.Client, owner, repo string, id int) string {
	pr, _, err := gh.PullRequests.Get(owner, repo, id)
	if err != nil {
		return "unknown"
	}

	if *pr.Merged {
		return "merged"
	}

	return *pr.State
}

type PullRequest struct {
	Id         int    `json:"id"`
	Owner      string `json:"owner"`
	Repository string `json:"repository"`
	Title      string `json:"title"`
	State      string `json:"state"`
}

func moveIfPossible(red redis.Conn, pr PullRequest) bool {
	data, err := json.Marshal(pr)
	if err != nil {
		return false
	}

	if pr.State == "merged" {
		fmt.Println("PR is merged, will move")
		red.Do("RPUSH", "hug:pullrequests:merged", data)
		return true
	}
	if pr.State == "closed" {
		fmt.Println("PR is closed, will move")
		red.Do("RPUSH", "hug:pullrequests:closed", data)
		return true
	}

	return false
}

func main() {
	flag.Parse()

	// Output the version and exit
	if *flagVersion {
		fmt.Printf("hug-status v%d.%d.%d\n", majorVersion, minorVersion, patchVersion)
		return
	}

	// Check for configuration file
	if len(*flagConfigFile) <= 0 {
		log.Fatal("No configuration file found. Please add the --config parameter")
	}

	// PID-File
	if len(*flagPidFile) > 0 {
		ioutil.WriteFile(*flagPidFile, []byte(strconv.Itoa(os.Getpid())), 0644)
	}

	config, err := config.NewConfiguration(flagConfigFile)
	if err != nil {
		log.Fatal("Configuration initialisation failed:", err)
	}

	client := GitHubClient(config.GitHub.AccessTokenSecret)

	red, err := redis.Dial("tcp", ":6379")
	if err != nil {
		fmt.Println("error", err)
		return
	}
	defer red.Close()

	for {
		fmt.Println("Checking for things…")

		values, err := redis.Values(red.Do("BLPOP", "hug:pullrequests", 0))
		if err != nil {
			fmt.Println("Redis failed", err)
			time.Sleep(waitTime * time.Second)
			continue
		}

		var pr PullRequest
		bytes, err := redis.Bytes(values[1], nil)
		if err != nil {
			fmt.Println("Something broke in bytes", err)
			time.Sleep(waitTime * time.Second)
			continue
		}

		err = json.Unmarshal(bytes, &pr)
		if err != nil {
			fmt.Println("Something broke", err)
			time.Sleep(waitTime * time.Second)
			continue
		}

		if moveIfPossible(red, pr) {
			continue
		}

		fmt.Println("id", pr.Id)
		fmt.Println("old state", pr.State)
		pr.State = GitHubPRStatus(client, pr.Owner, pr.Repository, pr.Id)
		fmt.Println("new state", pr.State)

		if moveIfPossible(red, pr) {
			continue
		}

		data, err := json.Marshal(pr)
		if err != nil {
			fmt.Println("Something broke", err)
			time.Sleep(waitTime * time.Second)
			continue
		}

		red.Do("RPUSH", "hug:pullrequests", data)
		fmt.Println("going to sleep")
		time.Sleep(waitTime * time.Second)
	}
}
