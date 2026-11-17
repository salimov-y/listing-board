package worker

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"listingboard/internal/board"
)

func TestAdvanceDoesOneStepAtATime(t *testing.T) {
	dir := t.TempDir()
	job := runningSheet(t, dir)

	for done := 1; done <= 3; done++ {
		job = Advance(job, dir)
		for i, step := range job.Checklist {
			if (i < done) != (step.State == "done") {
				t.Fatalf("after %d steps, step %q is %s", done, step.Label, step.State)
			}
		}
		want := "running"
		if done == 3 {
			want = "success"
		}
		if job.Status != want || job.RunID != "run-1" || job.Error != "" {
			t.Fatalf("after %d steps: status %s, runId %q, error %q", done, job.Status, job.RunID, job.Error)
		}
	}
}

func TestAdvanceResumesFromTheFirstPendingStep(t *testing.T) {
	dir := t.TempDir()
	job := runningSheet(t, dir)
	job.Checklist[0].State = "done"
	before := slices.Clone(job.Checklist)

	next := Advance(job, dir)
	states := []string{next.Checklist[0].State, next.Checklist[1].State, next.Checklist[2].State}
	if !slices.Equal(states, []string{"done", "done", "pending"}) || next.Status != "running" {
		t.Fatalf("status %s, steps %v", next.Status, states)
	}
	if !slices.Equal(job.Checklist, before) {
		t.Fatal("Advance changed the checklist it was given")
	}
}

func TestAdvanceFailsWhenTheBrochureIsMissing(t *testing.T) {
	dir := t.TempDir()
	job := runningSheet(t, dir)
	if err := os.Remove(board.BrochurePath(dir, job)); err != nil {
		t.Fatal(err)
	}

	next := Advance(job, dir)
	if next.Status != "failed" || next.Error != "Brochure is missing" || doneSteps(next) != 0 {
		t.Fatalf("status %s, error %q, %d steps done", next.Status, next.Error, doneSteps(next))
	}
}

func runningSheet(t *testing.T, dir string) board.Job {
	t.Helper()
	job := board.Job{
		ID:        board.NewID(),
		UserID:    board.NewID(),
		Status:    "running",
		RunID:     "run-1",
		Filename:  "brochure.txt",
		Checklist: board.NewChecklist(),
	}
	path := board.BrochurePath(dir, job)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	return job
}
