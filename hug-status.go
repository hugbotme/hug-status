package main

import (
	"flag"
	"fmt"
	"golang.org/x/oauth2"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"time"

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

	fmt.Println("title", *pr.Title)
	fmt.Println("merged", *pr.Merged)
	fmt.Println("merged at", pr.MergedAt)
	fmt.Println("closed at", pr.ClosedAt)

	if *pr.Merged {
		return "merged"
	}

	return *pr.State
}

func main() {
	flag.Parse()

	// Output the version and exit
	if *flagVersion {
		fmt.Printf("hug v%d.%d.%d\n", majorVersion, minorVersion, patchVersion)
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

	for {
		fmt.Println(GitHubPRStatus(client, "antirez", "redis", 2580))
		time.Sleep(60 * time.Second)
	}
}
