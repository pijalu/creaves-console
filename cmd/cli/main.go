package main

import (
	"creaves-console/actions"
	"creaves-console/models"
	"flag"
	"fmt"
	"log"
	"os"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "serve":
		runServer()
	case "import":
		runImport()
	case "process":
		runProcess()
	case "rebuild":
		runRebuild()
	case "stats":
		runStats()
	case "history":
		runHistory()
	case "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Consolidation CLI")
	fmt.Println()
	fmt.Println("Usage: consolidation-cli <command> [options]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  serve     Start the web server")
	fmt.Println("  import    Import events from all sources and process them")
	fmt.Println("  process   Process unprocessed events only")
	fmt.Println("  rebuild   Rebuild consolidated view from scratch")
	fmt.Println("  stats     Show consolidation statistics")
	fmt.Println("  history   Show recent import runs")
	fmt.Println("  help      Show this help message")
	fmt.Println()
	fmt.Println("Options for import:")
	fmt.Println("  -dry-run          Show what would be imported without importing")
	fmt.Println()
	fmt.Println("Environment variables:")
	fmt.Println("  DATABASE_URL      Database connection string")
	fmt.Println("  GO_ENV            Environment (development/production)")
}

func runServer() {
	app := actions.App()
	if err := app.Serve(); err != nil {
		log.Fatal(err)
	}
}

func runImport() {
	importFlags := flag.NewFlagSet("import", flag.ExitOnError)
	dryRun := importFlags.Bool("dry-run", false, "Show what would be imported without importing")
	importFlags.Parse(os.Args[2:])

	fmt.Println("Starting import...")
	startTime := time.Now()

	// Use the main DB connection (transactions handled within runner)
	runner := actions.NewConsolidationRunner(models.DB)

	if *dryRun {
		fmt.Println("DRY RUN - No changes were made")
		fmt.Println("Would process all unprocessed events")
		return
	}

	result, err := runner.Run()
	if err != nil {
		log.Fatalf("Import failed: %v", err)
	}

	duration := time.Since(startTime)
	fmt.Printf("\nImport completed in %v\n", duration)
	fmt.Printf("Import Run ID: %s\n", result.ImportRunID)
	fmt.Printf("Events processed: %d\n", result.EventsProcessed)

	if len(result.Errors) > 0 {
		fmt.Println("\nErrors:")
		for _, err := range result.Errors {
			fmt.Printf("  - %s\n", err)
		}
	}
}

func runProcess() {
	fmt.Println("Processing unprocessed events...")
	startTime := time.Now()

	processor := actions.NewEventProcessor(models.DB)
	count, err := processor.ProcessUnprocessedEvents()
	if err != nil {
		log.Fatalf("Processing failed: %v", err)
	}

	duration := time.Since(startTime)
	fmt.Printf("Processed %d events in %v\n", count, duration)
}

func runRebuild() {
	fmt.Println("Rebuilding consolidated view...")
	startTime := time.Now()

	processor := actions.NewEventProcessor(models.DB)
	count, err := processor.ProcessAllEvents()
	if err != nil {
		log.Fatalf("Rebuild failed: %v", err)
	}

	duration := time.Since(startTime)
	fmt.Printf("Rebuilt consolidated view from %d events in %v\n", count, duration)
}

func runStats() {
	processor := actions.NewEventProcessor(models.DB)
	stats, err := processor.GetConsolidatedStats()
	if err != nil {
		log.Fatalf("Failed to get stats: %v", err)
	}

	fmt.Println("Consolidation Statistics")
	fmt.Println("========================")
	fmt.Printf("Total animals: %d\n", stats["total_animals"])
	fmt.Printf("Unprocessed events: %d\n", stats["unprocessed_events"])

	if byStatus, ok := stats["by_status"].(map[string]int); ok {
		fmt.Println("\nBy status:")
		for status, count := range byStatus {
			fmt.Printf("  %s: %d\n", status, count)
		}
	}

	if byInstance, ok := stats["by_instance"].(map[string]int); ok {
		fmt.Println("\nBy instance:")
		for instance, count := range byInstance {
			fmt.Printf("  %s: %d\n", instance, count)
		}
	}
}

func runHistory() {
	runner := actions.NewConsolidationRunner(models.DB)
	runs, err := runner.GetRunHistory(10)
	if err != nil {
		log.Fatalf("Failed to get history: %v", err)
	}

	fmt.Println("Recent Import Runs")
	fmt.Println("==================")
	fmt.Printf("%-36s %-20s %-10s %-10s %-10s %-10s\n",
		"ID", "Started", "Status", "Imported", "Processed", "Duration")
	fmt.Println(string(make([]byte, 100)))

	for _, run := range runs {
		duration := "N/A"
		if run.CompletedAt != nil {
			duration = run.CompletedAt.Sub(run.StartedAt).Round(time.Second).String()
		}
		fmt.Printf("%-36s %-20s %-10s %-10d %-10d %-10s\n",
			run.ID,
			run.StartedAt.Format("2006-01-02 15:04:05"),
			run.Status,
			run.EventsImported,
			run.EventsProcessed,
			duration)
	}
}
