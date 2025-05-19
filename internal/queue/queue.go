// Package queue provides a smart queueing system for GitHub migrations.
// It respects GitHub's concurrency limits and supports priority-based queueing.
package queue

import (
	"container/heap"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/kuhlman-labs/gh-ghes-2-ghec/internal/metrics"
)

const (
	// PriorityHigh represents high priority migrations
	PriorityHigh = 100
	// PriorityMedium represents medium priority migrations
	PriorityMedium = 50
	// PriorityLow represents low priority migrations
	PriorityLow = 10
	// PriorityDefault is the default priority for migrations
	PriorityDefault = PriorityMedium
)

// MigrationJob represents a migration job in the queue
type MigrationJob struct {
	// Repository is the full repository name (org/repo)
	Repository string
	// Added is when the job was added to the queue
	Added time.Time
	// Priority of the job (higher is more important)
	Priority int
	// Data holds additional data needed for the job
	Data interface{}
	// index is used by the heap implementation
	index int
	// IsArchive indicates if this is an archive job (true) or migration job (false)
	IsArchive bool
}

// JobHandler is a function that processes a job
type JobHandler func(job *MigrationJob) error

// PriorityQueue implements a priority queue for migration jobs
type priorityQueue []*MigrationJob

// Len returns the length of the queue
func (pq priorityQueue) Len() int { return len(pq) }

// Less determines if one job has higher priority than another
func (pq priorityQueue) Less(i, j int) bool {
	// Higher priority comes first
	if pq[i].Priority != pq[j].Priority {
		return pq[i].Priority > pq[j].Priority
	}
	// If priorities are equal, older jobs come first (FIFO)
	return pq[i].Added.Before(pq[j].Added)
}

// Swap swaps two jobs in the queue
func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

// Push adds a job to the queue
func (pq *priorityQueue) Push(x interface{}) {
	n := len(*pq)
	job := x.(*MigrationJob)
	job.index = n
	*pq = append(*pq, job)
}

// Pop removes the highest priority job from the queue
func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	job := old[n-1]
	old[n-1] = nil
	job.index = -1
	*pq = old[0 : n-1]
	return job
}

// QueueManager manages the migration job queues
type QueueManager struct {
	// Maximum number of jobs that can be queued
	maxQueueSize int
	// Maximum number of archive generations that can run concurrently
	maxArchiveGenerations int
	// Maximum number of migrations that can run concurrently
	maxMigrations int

	// Current number of active archive generations
	activeArchiveGenerations int
	// Current number of active migrations
	activeMigrations int

	// Queue of jobs waiting to be processed
	queue priorityQueue
	// Map of repository to job for quick lookup
	jobMap map[string]*MigrationJob

	// Mutex to protect the queues and counters
	mu sync.Mutex

	// Logger for the queue manager
	logger *slog.Logger

	// Channel to signal that a new job is available
	jobAvailable chan struct{}
	// Channel to signal shutdown
	shutdown chan struct{}
	// WaitGroup to track active workers
	wg sync.WaitGroup

	// Function to handle archive generation jobs
	archiveHandler JobHandler
	// Function to handle migration jobs
	migrationHandler JobHandler
}

// NewQueueManager creates a new queue manager
func NewQueueManager(
	logger *slog.Logger,
	maxQueueSize, maxArchiveGenerations, maxMigrations int,
	archiveHandler, migrationHandler JobHandler,
) *QueueManager {
	if logger == nil {
		logger = slog.Default()
	}

	// Apply sensible defaults if invalid values are provided
	if maxQueueSize <= 0 {
		maxQueueSize = 1000 // Default to a large queue
	}
	if maxArchiveGenerations <= 0 {
		maxArchiveGenerations = 5 // GitHub's limit
	}
	if maxMigrations <= 0 {
		maxMigrations = 10 // GitHub's limit
	}

	qm := &QueueManager{
		maxQueueSize:          maxQueueSize,
		maxArchiveGenerations: maxArchiveGenerations,
		maxMigrations:         maxMigrations,
		queue:                 make(priorityQueue, 0, maxQueueSize),
		jobMap:                make(map[string]*MigrationJob),
		logger:                logger,
		jobAvailable:          make(chan struct{}, 1),
		shutdown:              make(chan struct{}),
		archiveHandler:        archiveHandler,
		migrationHandler:      migrationHandler,
	}

	// Initialize the heap
	heap.Init(&qm.queue)

	return qm
}

// Start starts the queue manager's workers
func (qm *QueueManager) Start() {
	qm.logger.Info("Starting queue manager",
		"max_queue_size", qm.maxQueueSize,
		"max_archive_generations", qm.maxArchiveGenerations,
		"max_migrations", qm.maxMigrations)

	// Start workers for archive generation
	for i := 0; i < qm.maxArchiveGenerations; i++ {
		qm.wg.Add(1)
		go qm.archiveWorker()
	}

	// Start workers for migrations
	for i := 0; i < qm.maxMigrations; i++ {
		qm.wg.Add(1)
		go qm.migrationWorker()
	}

	// Start a goroutine to periodically update metrics
	go qm.updateMetrics()
}

// Stop stops the queue manager
func (qm *QueueManager) Stop() {
	qm.logger.Info("Stopping queue manager")
	close(qm.shutdown)
	qm.wg.Wait()
	qm.logger.Info("Queue manager stopped")
}

// EnqueueArchiveJob adds a repository archive job to the queue
func (qm *QueueManager) EnqueueArchiveJob(repository string, data interface{}, priority int) error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	// Check if the repository is already in the queue
	if _, exists := qm.jobMap[repository]; exists {
		return fmt.Errorf("repository %s is already in the queue", repository)
	}

	// Check if the queue is full
	if len(qm.queue) >= qm.maxQueueSize {
		return fmt.Errorf("queue is full, cannot add more jobs")
	}

	// Create a new job
	job := &MigrationJob{
		Repository: repository,
		Added:      time.Now(),
		Priority:   priority,
		Data:       data,
		IsArchive:  true,
	}

	// Add the job to the queue
	heap.Push(&qm.queue, job)
	qm.jobMap[repository] = job

	// Record metrics
	metrics.QueueSize.Set(float64(len(qm.queue)))
	metrics.QueuedJobs.Inc()

	// Signal that a new job is available
	select {
	case qm.jobAvailable <- struct{}{}:
	default:
		// Channel is already signaled, which is fine
	}

	qm.logger.Info("Enqueued archive job",
		"repository", repository,
		"priority", priority,
		"queue_size", len(qm.queue))

	return nil
}

// EnqueueMigrationJob adds a repository migration job to the queue
func (qm *QueueManager) EnqueueMigrationJob(repository string, data interface{}, priority int) error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	// Check if the repository is already in the queue
	if _, exists := qm.jobMap[repository]; exists {
		return fmt.Errorf("repository %s is already in the queue", repository)
	}

	// Check if the queue is full
	if len(qm.queue) >= qm.maxQueueSize {
		return fmt.Errorf("queue is full, cannot add more jobs")
	}

	// Create a new job
	job := &MigrationJob{
		Repository: repository,
		Added:      time.Now(),
		Priority:   priority,
		Data:       data,
		IsArchive:  false,
	}

	// Add the job to the queue
	heap.Push(&qm.queue, job)
	qm.jobMap[repository] = job

	// Record metrics
	metrics.QueueSize.Set(float64(len(qm.queue)))
	metrics.QueuedJobs.Inc()

	// Signal that a new job is available
	select {
	case qm.jobAvailable <- struct{}{}:
	default:
		// Channel is already signaled, which is fine
	}

	qm.logger.Info("Enqueued migration job",
		"repository", repository,
		"priority", priority,
		"queue_size", len(qm.queue))

	return nil
}

// archiveWorker processes archive generation jobs
func (qm *QueueManager) archiveWorker() {
	defer qm.wg.Done()

	for {
		select {
		case <-qm.shutdown:
			return
		case <-qm.jobAvailable:
			qm.processNextJob(true)
		case <-time.After(100 * time.Millisecond):
			// Periodically check for jobs even if not signaled
			qm.processNextJob(true)
		}
	}
}

// migrationWorker processes migration jobs
func (qm *QueueManager) migrationWorker() {
	defer qm.wg.Done()

	for {
		select {
		case <-qm.shutdown:
			return
		case <-qm.jobAvailable:
			qm.processNextJob(false)
		case <-time.After(100 * time.Millisecond):
			// Periodically check for jobs even if not signaled
			qm.processNextJob(false)
		}
	}
}

// processNextJob processes the next job in the queue
func (qm *QueueManager) processNextJob(isArchive bool) {
	qm.mu.Lock()

	// Check if there are any jobs in the queue
	if qm.queue.Len() == 0 {
		qm.mu.Unlock()
		return
	}

	// Check if we've hit the concurrency limit
	if isArchive && qm.activeArchiveGenerations >= qm.maxArchiveGenerations {
		qm.mu.Unlock()
		return
	}
	if !isArchive && qm.activeMigrations >= qm.maxMigrations {
		qm.mu.Unlock()
		return
	}

	// Find the next job of the right type
	var nextJob *MigrationJob
	for i := 0; i < qm.queue.Len(); i++ {
		job := qm.queue[i]
		if job.IsArchive == isArchive {
			nextJob = job
			heap.Remove(&qm.queue, i)
			break
		}
	}

	// If no job of the right type was found, unlock and return
	if nextJob == nil {
		qm.mu.Unlock()
		return
	}

	// Update active job counters
	if isArchive {
		qm.activeArchiveGenerations++
	} else {
		qm.activeMigrations++
	}

	// Remove from job map
	delete(qm.jobMap, nextJob.Repository)

	// Update metrics
	metrics.QueueSize.Set(float64(len(qm.queue)))
	if isArchive {
		metrics.ActiveArchives.Set(float64(qm.activeArchiveGenerations))
	} else {
		metrics.ActiveMigrations.Set(float64(qm.activeMigrations))
	}

	qm.mu.Unlock()

	// Process the job
	qm.logger.Info("Processing job",
		"repository", nextJob.Repository,
		"is_archive", isArchive)

	var err error
	if isArchive {
		err = qm.archiveHandler(nextJob)
	} else {
		err = qm.migrationHandler(nextJob)
	}

	// Update metrics and counters after job completion
	qm.mu.Lock()
	if isArchive {
		qm.activeArchiveGenerations--
		metrics.ActiveArchives.Set(float64(qm.activeArchiveGenerations))
		metrics.CompletedArchives.Inc()
	} else {
		qm.activeMigrations--
		metrics.ActiveMigrations.Set(float64(qm.activeMigrations))
		metrics.CompletedMigrations.Inc()
	}
	qm.mu.Unlock()

	if err != nil {
		qm.logger.Error("Job processing failed",
			"repository", nextJob.Repository,
			"is_archive", isArchive,
			"error", err)
	}

	// Signal that more jobs can be processed
	select {
	case qm.jobAvailable <- struct{}{}:
	default:
		// Channel is already signaled, which is fine
	}
}

// GetQueueStats returns statistics about the queue
func (qm *QueueManager) GetQueueStats() map[string]interface{} {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	return map[string]interface{}{
		"queue_size":                 len(qm.queue),
		"max_queue_size":             qm.maxQueueSize,
		"active_archive_generations": qm.activeArchiveGenerations,
		"max_archive_generations":    qm.maxArchiveGenerations,
		"active_migrations":          qm.activeMigrations,
		"max_migrations":             qm.maxMigrations,
	}
}

// updateMetrics periodically updates queue metrics
func (qm *QueueManager) updateMetrics() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-qm.shutdown:
			return
		case <-ticker.C:
			stats := qm.GetQueueStats()
			metrics.QueueSize.Set(float64(stats["queue_size"].(int)))
			metrics.ActiveArchives.Set(float64(stats["active_archive_generations"].(int)))
			metrics.ActiveMigrations.Set(float64(stats["active_migrations"].(int)))
		}
	}
}
