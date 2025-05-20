package queue

import (
	"log/slog"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"
)

// TestQueueManager_Basic tests the basic functionality of the queue manager
func TestQueueManager_Basic(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Mock job handlers that just record what jobs were executed
	var archiveJobs, migrationJobs []string
	var jobsMutex sync.Mutex

	archiveHandler := func(job *MigrationJob) error {
		jobsMutex.Lock()
		defer jobsMutex.Unlock()
		archiveJobs = append(archiveJobs, job.Repository)
		return nil
	}

	migrationHandler := func(job *MigrationJob) error {
		jobsMutex.Lock()
		defer jobsMutex.Unlock()
		migrationJobs = append(migrationJobs, job.Repository)
		return nil
	}

	// Create a queue manager with 2 workers each for testing
	qm := NewQueueManager(
		logger,
		100, // maxQueueSize
		2,   // maxArchiveThreads (using 2 for faster testing)
		2,   // maxMigrationThreads (using 2 for faster testing)
		archiveHandler,
		migrationHandler,
	)

	// Start the queue manager
	qm.Start()
	defer qm.Stop()

	// Add some jobs with different priorities
	repoNames := []string{
		"org/repo1", // High priority
		"org/repo2", // Medium priority
		"org/repo3", // Low priority
		"org/repo4", // Medium priority
		"org/repo5", // High priority
	}

	priorities := []int{
		PriorityHigh,
		PriorityMedium,
		PriorityLow,
		PriorityMedium,
		PriorityHigh,
	}

	// Enqueue archive jobs
	for i, repo := range repoNames {
		err := qm.EnqueueArchiveJob(repo, repo, priorities[i])
		if err != nil {
			t.Fatalf("Failed to enqueue job for %s: %v", repo, err)
		}
	}

	// Give some time for jobs to be processed
	time.Sleep(500 * time.Millisecond)

	// Check queue stats
	stats := qm.GetQueueStats()
	t.Logf("Queue stats: %+v", stats)

	// Wait a bit longer to ensure all jobs are processed
	time.Sleep(1 * time.Second)

	// Verify that all jobs were processed
	jobsMutex.Lock()
	defer jobsMutex.Unlock()

	if len(archiveJobs) != len(repoNames) {
		t.Errorf("Expected %d archive jobs to be processed, got %d", len(repoNames), len(archiveJobs))
	}

	// Expect high priority jobs first, then medium, then low
	// This is an approximate test since concurrency can affect the exact order
	highPriorityFirst := false
	for i, repo := range archiveJobs {
		if i == 0 && (repo == "org/repo1" || repo == "org/repo5") {
			highPriorityFirst = true
		}
	}

	if !highPriorityFirst {
		t.Errorf("Expected high priority jobs to be processed first, got order: %v", archiveJobs)
	}
}

// TestQueueManager_PriorityOrdering tests that jobs are processed in priority order
func TestQueueManager_PriorityOrdering(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Use a channel to record the exact order jobs are processed
	processingOrder := make(chan string, 10)

	// Use a single worker to guarantee sequential processing
	archiveHandler := func(job *MigrationJob) error {
		processingOrder <- job.Repository
		return nil
	}

	migrationHandler := func(job *MigrationJob) error {
		return nil // Not testing migration jobs in this test
	}

	// Create a queue manager with just 1 worker to ensure deterministic ordering
	qm := NewQueueManager(
		logger,
		100, // maxQueueSize
		1,   // maxArchiveThreads (only 1 to ensure deterministic ordering)
		1,   // maxMigrationThreads
		archiveHandler,
		migrationHandler,
	)

	// Start the queue manager
	qm.Start()
	defer qm.Stop()

	// Add jobs in reverse priority order to ensure ordering is by priority, not insertion
	err := qm.EnqueueArchiveJob("org/low", "data", PriorityLow)
	if err != nil {
		t.Fatalf("Failed to enqueue low priority job: %v", err)
	}

	err = qm.EnqueueArchiveJob("org/medium", "data", PriorityMedium)
	if err != nil {
		t.Fatalf("Failed to enqueue medium priority job: %v", err)
	}

	err = qm.EnqueueArchiveJob("org/high", "data", PriorityHigh)
	if err != nil {
		t.Fatalf("Failed to enqueue high priority job: %v", err)
	}

	// Collect the order of processing
	var processedJobs []string
	timeout := time.After(2 * time.Second)

	// Collect 3 jobs or timeout
	for i := 0; i < 3; i++ {
		select {
		case job := <-processingOrder:
			processedJobs = append(processedJobs, job)
		case <-timeout:
			t.Fatalf("Timeout waiting for jobs to be processed")
		}
	}

	// Verify the order: high, medium, low
	if len(processedJobs) != 3 {
		t.Fatalf("Expected 3 jobs to be processed, got %d", len(processedJobs))
	}

	expectedOrder := []string{"org/high", "org/medium", "org/low"}
	for i, expected := range expectedOrder {
		if processedJobs[i] != expected {
			t.Errorf("Expected job %d to be %s, got %s", i, expected, processedJobs[i])
		}
	}
}

// TestQueueManager_Concurrency tests that the queue manager respects concurrency limits
func TestQueueManager_Concurrency(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Track concurrent jobs
	var activeCount int
	var maxActive int
	var countMutex sync.Mutex

	// Use a handler that sleeps to simulate work and tracks concurrency
	archiveHandler := func(job *MigrationJob) error {
		countMutex.Lock()
		activeCount++
		if activeCount > maxActive {
			maxActive = activeCount
		}
		countMutex.Unlock()

		// Simulate work
		time.Sleep(100 * time.Millisecond)

		countMutex.Lock()
		activeCount--
		countMutex.Unlock()
		return nil
	}

	migrationHandler := func(job *MigrationJob) error {
		return nil // Not testing migration jobs in this test
	}

	// Create a queue manager with 3 workers
	maxConcurrent := 3
	qm := NewQueueManager(
		logger,
		100,           // maxQueueSize
		maxConcurrent, // maxArchiveThreads
		1,             // maxMigrationThreads
		archiveHandler,
		migrationHandler,
	)

	// Start the queue manager
	qm.Start()
	defer qm.Stop()

	// Add 10 jobs simultaneously to test concurrency
	for i := 0; i < 10; i++ {
		repoName := "org/repo" + strconv.Itoa(i)
		err := qm.EnqueueArchiveJob(repoName, "data", PriorityDefault)
		if err != nil {
			t.Fatalf("Failed to enqueue job %s: %v", repoName, err)
		}
	}

	// Wait for jobs to be processed
	time.Sleep(500 * time.Millisecond)

	// Check that concurrency was respected
	countMutex.Lock()
	defer countMutex.Unlock()

	if maxActive > maxConcurrent {
		t.Errorf("Concurrency limit exceeded: expected max %d concurrent jobs, got %d", maxConcurrent, maxActive)
	}

	// More thorough validation could be done by inspecting queue metrics
	stats := qm.GetQueueStats()
	t.Logf("Queue stats: %+v", stats)
}

// TestGetQueuedRepositories verifies that GetQueuedRepositories returns the correct set of queued jobs
func TestGetQueuedRepositories(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	archiveHandler := func(job *MigrationJob) error {
		// Simulate work
		time.Sleep(10 * time.Millisecond)
		return nil
	}
	migrationHandler := func(job *MigrationJob) error { return nil }

	qm := NewQueueManager(
		logger,
		10, // maxQueueSize
		1,  // maxArchiveThreads
		1,  // maxMigrationThreads
		archiveHandler,
		migrationHandler,
	)

	// Enqueue 3 jobs
	repos := []string{"org/repoA", "org/repoB", "org/repoC"}
	for _, repo := range repos {
		err := qm.EnqueueArchiveJob(repo, "data", PriorityDefault)
		if err != nil {
			t.Fatalf("Failed to enqueue job for %s: %v", repo, err)
		}
	}

	queued := qm.GetQueuedRepositories()
	if len(queued) != 3 {
		t.Errorf("Expected 3 queued repositories, got %d", len(queued))
	}
	// Check that all repos are present
	found := make(map[string]bool)
	for _, repo := range queued {
		found[repo] = true
	}
	for _, repo := range repos {
		if !found[repo] {
			t.Errorf("Expected repo %s to be in queued repositories", repo)
		}
	}

	// Start the queue manager and let a worker pick up a job
	qm.Start()
	defer qm.Stop()

	// Wait up to 500ms for the number of queued repositories to decrease
	var queuedAfter []string
	for i := 0; i < 10; i++ {
		queuedAfter = qm.GetQueuedRepositories()
		if len(queuedAfter) < len(repos) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if len(queuedAfter) >= len(repos) {
		t.Errorf("Expected fewer than %d queued repositories after processing, got %d", len(repos), len(queuedAfter))
	}
}
