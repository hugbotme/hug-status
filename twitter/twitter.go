package twitter

import (
	"fmt"
	"github.com/ChimeraCoder/anaconda"
	"github.com/hugbotme/hug-status/config"
	"net/url"
	"strconv"
)

type Twitter struct {
	API *anaconda.TwitterApi
}

type Hug struct {
	TweetID string
	URL     string
}

func NewClient(config *config.Configuration) *Twitter {
	anaconda.SetConsumerKey(config.Twitter.ConsumerKey)
	anaconda.SetConsumerSecret(config.Twitter.ConsumerSecret)
	api := anaconda.NewTwitterApi(config.Twitter.AccessToken, config.Twitter.AccessTokenSecret)

	client := Twitter{
		API: api,
	}

	return &client
}

func (client *Twitter) PostReply(msg string, id_str string) {
	id, _ := strconv.ParseInt(id_str, 10, 64)
	tweet, err := client.API.GetTweet(id, nil)
	if err != nil {
		fmt.Println("twitter failed", err)
		return
	}
	username := tweet.User.ScreenName
	text := fmt.Sprintf(msg, username)

	fmt.Println("posting tweet:", text)

	v := url.Values{}
	v.Set("in_reply_to_status_id", id_str)
	_, err = client.API.PostTweet(text, v)
	if err != nil {
		fmt.Println("twitter failed", err)
		return
	}
}

func (client *Twitter) Post(msg string) {
	text := fmt.Sprintf(msg)

	fmt.Println("posting tweet:", text)

	v := url.Values{}
	_, err := client.API.PostTweet(text, v)
	if err != nil {
		fmt.Println("twitter failed", err)
		return
	}
}
