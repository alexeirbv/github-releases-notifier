package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	githubql "github.com/shurcooL/githubql"
	"golang.org/x/oauth2"
)

// Repos from JSON file
type Repositories struct {
	Names []string `json:"repos"`
}

// Config of env and args
type Config struct {
	GithubToken     string        `arg:"env:GITHUB_TOKEN"`
	Interval        time.Duration `arg:"env:INTERVAL"`
	LogLevel        string        `arg:"env:LOG_LEVEL"`
	SlackHook       string        `arg:"env:SLACK_HOOK"`
	IgnoreNonstable bool          `arg:"env:IGNORE_NONSTABLE"`
	Repositories    []string      `arg:"-r,separate"`
	ReposFilePath   string        `arg:"env:REPOS_FILE_PATH"`
	MetricsPort     int           `arg:"env:METRICS_PORT"`
}

// Token returns an oauth2 token or an error.
func (c Config) Token() *oauth2.Token {
	return &oauth2.Token{AccessToken: c.GithubToken}
}

func main() {
	_ = godotenv.Load()

	c := Config{
		Interval: time.Hour,
		LogLevel: "info",
	}
	arg.MustParse(&c)

	if c.MetricsPort == 0 {
		c.MetricsPort = 8080
	}

	logger := log.NewJSONLogger(log.NewSyncWriter(os.Stdout))
	logger = log.With(logger,
		"ts", log.DefaultTimestampUTC,
		"caller", log.Caller(5),
	)

	// level.SetKey("severity")
	switch strings.ToLower(c.LogLevel) {
	case "debug":
		logger = level.NewFilter(logger, level.AllowDebug())
	case "warn":
		logger = level.NewFilter(logger, level.AllowWarn())
	case "error":
		logger = level.NewFilter(logger, level.AllowError())
	default:
		logger = level.NewFilter(logger, level.AllowInfo())
	}
	var repos Repositories

	if c.ReposFilePath != "" {
		// Reading repos from JSON file
		jsonFromFile, err := os.Open(c.ReposFilePath)

		if err != nil {
			level.Error(logger).Log("Can't load JSON file at path:", c.ReposFilePath)
			os.Exit(1)
		}
		defer jsonFromFile.Close()

		content, err := ioutil.ReadAll(jsonFromFile)

		if err != nil {
			level.Error(logger).Log(err)
			os.Exit(1)
		}

		err = json.Unmarshal(content, &repos)

		if err != nil {
			level.Error(logger).Log(err)
			os.Exit(1)
		}
	}
	if len(c.Repositories) == 0 && len(repos.Names) == 0 {
		level.Error(logger).Log("msg", "no repositories to watch")
		os.Exit(1)
	}

	tokenSource := oauth2.StaticTokenSource(c.Token())
	client := oauth2.NewClient(context.Background(), tokenSource)
	checker := &Checker{
		logger: logger,
		client: githubql.NewClient(client),
	}

	level.Info(logger).Log("msg", "Starting Prometheus metrics handler", "port", c.MetricsPort)

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(fmt.Sprintf(":%d", c.MetricsPort), nil)

	// TODO: releases := make(chan Repository, len(c.Repositories))
	releases := make(chan Repository)
	go checker.Run(c.Interval, append(repos.Names, c.Repositories...), releases)

	slack := SlackSender{Hook: c.SlackHook}

	level.Info(logger).Log("msg", "waiting for new releases")
	for repository := range releases {
		if c.IgnoreNonstable && repository.Release.IsNonstable() {
			level.Debug(logger).Log("msg", "not notifying about non-stable version", "version", repository.Release.Name)
			continue
		}
		if err := slack.Send(repository); err != nil {
			metricSlackSendErrorCounter.Inc()
			level.Warn(logger).Log(
				"msg", "failed to send release to messenger",
				"err", err,
			)
			continue
		}
	}
}
