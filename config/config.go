package config

import (
	"encoding/json"
	"io/ioutil"
)

type Configuration struct {
	GitHub githubConfiguration `json:"github"`
}

type githubConfiguration struct {
	AccessTokenSecret string `json:"access-token-secret"`
}

func NewConfiguration(configFile *string) (*Configuration, error) {
	fileContent, err := ioutil.ReadFile(*configFile)
	if err != nil {
		return nil, err
	}

	var config Configuration
	err = json.Unmarshal(fileContent, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}
