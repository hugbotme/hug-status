package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"golang.org/x/oauth2"
	"io/ioutil"
	"log"
	netUrl "net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/google/go-github/github"
	"github.com/hugbotme/hug-status/config"

	"github.com/hugbotme/hug-status/twitter"
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

	tweet_thanks_pr_open   = "- @%%s Thanks for the link. We checked the repository and filed a PR: %s"
	tweet_thanks_pr_merged = "- @%%s Thanks for the link. Our changes are already merged: %s"
	tweet_thanks_pr_closed = "- @%%s Thanks for the link, but our pull request was already was closed: %s"

	tweet_pr_open   = "I fixed some typos in %s and filed a PR: %s"
	tweet_pr_merged = "I fixed some typos in %s and the PR got merged: %s"
	tweet_pr_closed = "I fixed some typos in %s but the PR was closed :( %s"
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

func pullrequestStatus(config *config.Configuration) {
	client := GitHubClient(config.Github.APIToken)

	red, err := redis.Dial("tcp", ":6379")
	if err != nil {
		fmt.Println("error", err)
		return
	}
	defer red.Close()

	for {
		fmt.Println("Checking for things...")

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

type Hug struct {
	TweetID       string
	URL           string
	PullRequestId int
}

type GitHubURL struct {
	URL        *netUrl.URL
	Owner      string
	Repository string
}

func ParseGitHubURL(rawurl string) (*GitHubURL, error) {
	parsed, err := netUrl.Parse(rawurl)
	if err != nil {
		return nil, err
	}

	if parsed.Host != "github.com" {
		return nil, errors.New("Not a GitHub URL")
	}

	splitted := strings.Split(parsed.Path, "/")
	owner := splitted[1]
	repository := splitted[2]

	return &GitHubURL{
		URL:        parsed,
		Owner:      owner,
		Repository: repository,
	}, nil
}

func FetchFromQueue(client redis.Conn) (*Hug, error) {
	values, err := redis.Values(client.Do("BLPOP", "hug:finished", 0))
	if err != nil {
		return nil, err
	}
	fmt.Println("values", values)
	bytes, _ := redis.Bytes(values[1], nil)
	if len(bytes) == 0 {
		return nil, errors.New("No job")
	}

	var hug Hug
	err = json.Unmarshal(bytes, &hug)
	if err != nil {
		return nil, err
	}

	return &hug, nil
}

const (
	PrIsOpen = iota
	PrIsClosed
	PrIsMerged
)

func (pr *PullRequest) FullUrl() string {
	return fmt.Sprintf("http://github.com/%s/%s/pulls/%d",
		pr.Owner,
		pr.Repository,
		pr.Id)
}

func tweet(twitter_client *twitter.Twitter, pr PullRequest, hug *Hug, state int) {
	var text string

	pr_url := pr.FullUrl()

	if len(hug.TweetID) == 0 {
		switch state {
		case PrIsOpen:
			text = fmt.Sprintf(tweet_pr_open, pr.Repository, pr_url)
		case PrIsClosed:
			text = fmt.Sprintf(tweet_pr_closed, pr.Repository, pr_url)
		case PrIsMerged:
			text = fmt.Sprintf(tweet_pr_merged, pr.Repository, pr_url)
		}

		twitter_client.Post(text)
	} else {
		switch state {
		case PrIsOpen:
			text = fmt.Sprintf(tweet_thanks_pr_open, pr_url)
		case PrIsClosed:
			text = fmt.Sprintf(tweet_thanks_pr_closed, pr_url)
		case PrIsMerged:
			text = fmt.Sprintf(tweet_thanks_pr_merged, pr_url)
		}

		twitter_client.PostReply(text, hug.TweetID)
	}
}

func finishedRepos(config *config.Configuration) {
	client := GitHubClient(config.Github.APIToken)
	twitter_client := twitter.NewClient(config)

	red, err := redis.Dial("tcp", ":6379")
	if err != nil {
		fmt.Println("error", err)
		return
	}
	defer red.Close()

	fmt.Println("Waiting for finished repositories...")
	for {
		hug, err := FetchFromQueue(red)
		if err != nil {
			fmt.Println("Something went wrong. Horribly wrong", err)
			continue
		}

		fmt.Println("Finished job", hug)

		parsed, err := ParseGitHubURL(hug.URL)
		if err != nil {
			fmt.Println("Something went wrong parsing. Really horribly wrong", err)
			continue
		}
		state := GitHubPRStatus(client, parsed.Owner, parsed.Repository, hug.PullRequestId)
		pr := PullRequest{
			Id:         hug.PullRequestId,
			Owner:      parsed.Owner,
			Repository: parsed.Repository,
			State:      state,
		}

		data, err := json.Marshal(pr)
		if err != nil {
			fmt.Println("something went wrong :(")
			continue
		}

		if pr.State == "merged" {
			fmt.Println("PR is merged, will move")
			red.Do("RPUSH", "hug:pullrequests:merged", data)
			tweet(twitter_client, pr, hug, PrIsMerged)
		} else if pr.State == "closed" {
			fmt.Println("PR is closed, will move")
			red.Do("RPUSH", "hug:pullrequests:closed", data)
			tweet(twitter_client, pr, hug, PrIsClosed)
		} else if pr.State == "open" {
			fmt.Println("PR is open")
			red.Do("RPUSH", "hug:pullrequests", data)
			tweet(twitter_client, pr, hug, PrIsOpen)
		}
	}
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

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	// run as long as no interrupt is sent
	go func() {
		for sig := range c {
			fmt.Printf("captured %v, notifying everyone...\n", sig)
			fmt.Println("exiting...")
			os.Exit(1)
		}
	}()

	go pullrequestStatus(config)

	finishedRepos(config)

	for {
		// Got nothing to do here...
		time.Sleep(100 * waitTime * time.Second)
	}
}
