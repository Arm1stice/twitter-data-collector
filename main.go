package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/dghubble/go-twitter/twitter"
	"github.com/dghubble/oauth1"
	"github.com/robfig/cron/v3"
)

var currentTotalTweets = 0
var currentTweetNumber = 0
var currentMinute = 1

type tweet struct {
	Text  string `json:"text"`
	IDStr string `json:"id"`
}

type stats struct {
	Minute int `json:"minute"`
	Tweets int `json:"tweets"`
}

var tweetList = make([]tweet, 10000)
var statsList = make([]stats, 151)

// Used to prevent race conditions
var mux sync.Mutex

func main() {
	twitterConsumerConfig := oauth1.NewConfig(os.Getenv("CONSUMER_KEY"), os.Getenv("CONSUMER_SECRET"))
	twitterToken := oauth1.NewToken(os.Getenv("ACCESS_TOKEN"), os.Getenv("ACCESS_SECRET"))
	twitterHTTPClient := twitterConsumerConfig.Client(oauth1.NoContext, twitterToken)

	twitterClient := twitter.NewClient(twitterHTTPClient)

	// Test our authentication
	user, _, err := twitterClient.Users.Show(&twitter.UserShowParams{
		ScreenName: "wcalandro",
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(user.Name)

	// Demux will help sort out the messages and cast our types automatically
	demux := twitter.NewSwitchDemux()

	demux.Tweet = func(t *twitter.Tweet) {
		mux.Lock()
		newTweet := tweet{
			Text:  t.Text,
			IDStr: t.IDStr,
		}
		currentTweetNumber++
		tweetList = append(tweetList, newTweet)
		mux.Unlock()

		fmt.Println(t.Text)
	}
	demux.Warning = func(warning *twitter.StallWarning) {
		fmt.Println(warning.Message)
	}

	// Create a stream about tweets related to football worldwide
	// No use of creating a bounding box because that limits our data to only
	// those who tag their tweets with a location.
	streamParams := &twitter.StreamFilterParams{
		Track:         []string{"football"},
		StallWarnings: twitter.Bool(true),
	}
	tweetStream, err := twitterClient.Streams.Filter(streamParams)
	if err != nil {
		panic(err)
	}

	// Create cron job to activate every minute
	c := cron.New()
	c.AddFunc("*/1 * * * *", func() {
		mux.Lock()
		// Add tweets for the current minute to the total amount
		currentTotalTweets += currentTweetNumber

		// Write this minutes' Tweet data to a json file
		jsonBytes, err := json.Marshal(tweetList)
		err = ioutil.WriteFile(fmt.Sprintf("%d.json", currentMinute), jsonBytes, 0777)
		if err != nil {
			panic(err)
		}

		// Create statistics for the minute
		newStats := stats{
			Minute: currentMinute,
			Tweets: currentTweetNumber,
		}
		statsList = append(statsList, newStats)

		// Write data to the file
		statsBytes, err := json.Marshal(statsList)
		if err != nil {
			panic(err)
		}
		err = ioutil.WriteFile("stats.json", statsBytes, 0777)
		if err != nil {
			panic(err)
		}

		// Set up for next minute
		currentMinute++
		currentTweetNumber = 0
		tweetList = make([]tweet, 10000) // Probably not the best way to do this but oh well

		// Exit after we have completed 150 minutes
		if currentMinute == 151 {
			// Append statistics for the total to the data
			newStats = stats{
				Minute: 151,
				Tweets: currentTotalTweets,
			}
			statsList = append(statsList, newStats)

			// Write data to the file
			statsBytes, err := json.Marshal(statsList)
			if err != nil {
				panic(err)
			}
			err = ioutil.WriteFile("stats.json", statsBytes, 0777)
			if err != nil {
				panic(err)
			}

			os.Exit(0)
		}
		mux.Unlock()
	})

	go demux.HandleChan(tweetStream.Messages)

	// Taken from go-twitter documentation:
	// Wait for SIGINT and SIGTERM (HIT CTRL-C)
	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	log.Println(<-ch)

	tweetStream.Stop()
}
