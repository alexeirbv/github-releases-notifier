package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var metricGithubQueriesErrorCounter = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "github_queries_count",
		Help: "Github queires errors count",
	},
	[]string{"repo"},
)

var metricSlackSendErrorCounter = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "slack_send_error_count",
		Help: "Slack message send errors count",
	},
)
