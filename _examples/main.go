package main

import (
	"ddquery"
	"fmt"
	"log"
)

func main() {
	query, err := ddquery.Parse("avg:metric.name{env:prod} by {host}.as_count()")
	if err != nil {
		log.Fatal(err)
	}

	mq, ok := query.(*ddquery.MetricQuery)
	if !ok {
		log.Fatal("expected metric query")
	}

	fmt.Printf("Aggregator: %s\n", *mq.Aggregator)
	fmt.Printf("Metric: %s\n", mq.Metric)
	fmt.Printf("GroupBy: %v\n", mq.GroupBy)
}
