// Command snapshots-migrate-gen generates SQL migration files for snapshots table.
//
// Usage:
//
//	go run github.com/getpup/pupsourcing/cmd/snapshots-migrate-gen -output migrations -filename add_snapshots.sql
//
// Or with go generate:
//
//	//go:generate go run github.com/getpup/pupsourcing/cmd/snapshots-migrate-gen -output migrations
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/getpup/pupsourcing/es/migrations"
)

func main() {
	var (
		outputFolder   = flag.String("output", "migrations", "Output folder for migration file")
		outputFilename = flag.String("filename", "", "Output filename (default: timestamp-based)")
		snapshotsTable = flag.String("snapshots-table", "snapshots", "Name of snapshots table")
	)

	flag.Parse()

	config := migrations.DefaultSnapshotsConfig()
	config.OutputFolder = *outputFolder
	config.SnapshotsTable = *snapshotsTable

	if *outputFilename != "" {
		config.OutputFilename = *outputFilename
	}

	err := migrations.GenerateSnapshotsPostgres(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating snapshots migration: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Generated snapshots migration: %s/%s\n", config.OutputFolder, config.OutputFilename)
}
