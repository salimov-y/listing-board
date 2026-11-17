package worker

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

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

func TestRunSavesOneStepPerPause(t *testing.T) {
	dir := t.TempDir()
	st := &fakeStore{job: runningSheet(t, dir)}
	pause := 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	began := time.Now()
	go func() {
		Run(ctx, st, dir, pause)
		close(stopped)
	}()
	for deadline := time.Now().Add(2 * time.Second); len(st.saved()) < 3 && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(3 * pause)
	cancel()
	<-stopped

	saves := st.saved()
	if len(saves) != 3 {
		t.Fatalf("%d saves, want one per step", len(saves))
	}
	last := began
	for i, save := range saves {
		if done := doneSteps(save.job); done != i+1 {
			t.Fatalf("save %d has %d steps done, want %d", i, done, i+1)
		}
		if gap := save.at.Sub(last); gap < pause {
			t.Fatalf("save %d came %s after the previous one, want at least %s", i, gap, pause)
		}
		last = save.at
	}
	if saves[2].job.Status != "success" {
		t.Fatalf("status %s after the last step, want success", saves[2].job.Status)
	}
}

type fakeStore struct {
	mu    sync.Mutex
	job   board.Job
	saves []save
}

type save struct {
	at  time.Time
	job board.Job
}

func (f *fakeStore) RunningJobs(context.Context) ([]board.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.job.Status != "running" {
		return nil, nil
	}
	return []board.Job{f.job}, nil
}

func (f *fakeStore) UpdateJob(_ context.Context, job board.Job) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.job = job
	f.saves = append(f.saves, save{at: time.Now(), job: job})
	return nil
}

func (f *fakeStore) saved() []save {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.saves)
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
