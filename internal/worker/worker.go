package worker

import (
	"context"
	"log"
	"os"
	"slices"
	"time"

	"listingboard/internal/board"
)

// Store is the part of the database the worker uses.
type Store interface {
	RunningJobs(ctx context.Context) ([]board.Job, error)
	UpdateJob(ctx context.Context, job board.Job) error
}

// Run waits pause, then advances every running sheet by one step, until ctx is done.
func Run(ctx context.Context, st Store, dataDir string, pause time.Duration) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(pause):
		}
		jobs, err := st.RunningJobs(ctx)
		if err != nil {
			log.Printf("load running sheets: %v", err)
		}
		for _, job := range jobs {
			next := Advance(job, dataDir)
			if err := st.UpdateJob(ctx, next); err != nil {
				log.Printf("save sheet %s: %v", job.ID, err)
				continue
			}
			log.Printf("sheet %s run %s: %s, %d of %d steps done", next.ID, next.RunID, next.Status, doneSteps(next), len(next.Checklist))
		}
	}
}

// Advance marks the first pending step done, or fails the sheet when its brochure cannot be read.
func Advance(job board.Job, dataDir string) board.Job {
	job.Checklist = slices.Clone(job.Checklist)
	job.UpdatedAt = time.Now().UTC()
	if _, err := os.ReadFile(board.BrochurePath(dataDir, job)); err != nil {
		job.Status = "failed"
		job.Error = "Brochure is missing"
		return job
	}
	for i, step := range job.Checklist {
		if step.State == "pending" {
			job.Checklist[i].State = "done"
			break
		}
	}
	if doneSteps(job) == len(job.Checklist) {
		job.Status = "success"
	}
	return job
}

func doneSteps(job board.Job) int {
	done := 0
	for _, step := range job.Checklist {
		if step.State == "done" {
			done++
		}
	}
	return done
}
