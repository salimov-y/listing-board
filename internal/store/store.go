package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"listingboard/internal/board"
)

type Store struct {
	db *sql.DB
}

func Open(dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(5)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Wait retries Open until the database answers or the timeout passes.
func Wait(dsn string, timeout time.Duration) (*Store, error) {
	deadline := time.Now().Add(timeout)
	for {
		st, err := Open(dsn)
		if err == nil || time.Now().After(deadline) {
			return st, err
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, schemaSQL)
	return err
}

func (s *Store) CreateUser(ctx context.Context, email, passwordHash string) (board.User, error) {
	user := board.User{ID: board.NewID(), Email: email, PasswordHash: passwordHash}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (id, email, password_hash, created_at)
		VALUES ($1, $2, $3, $4)
	`, user.ID, user.Email, user.PasswordHash, time.Now().UTC())
	if isUnique(err) {
		return board.User{}, board.ErrDuplicate
	}
	if err != nil {
		return board.User{}, err
	}
	return user, nil
}

func (s *Store) UserByEmail(ctx context.Context, email string) (board.User, error) {
	var user board.User
	err := s.db.QueryRowContext(ctx, `
		SELECT id, email, password_hash FROM users WHERE email = $1
	`, email).Scan(&user.ID, &user.Email, &user.PasswordHash)
	return user, mapNotFound(err)
}

func (s *Store) CreateSession(ctx context.Context, userID, token string, expires time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (token, user_id, expires_at) VALUES ($1, $2, $3)
	`, token, userID, expires.UTC())
	return err
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token = $1`, token)
	return err
}

func (s *Store) UserByToken(ctx context.Context, token string) (board.User, error) {
	var user board.User
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.email
		FROM users u
		JOIN sessions s ON s.user_id = u.id
		WHERE s.token = $1 AND s.expires_at > now()
	`, token).Scan(&user.ID, &user.Email)
	return user, mapNotFound(err)
}

func (s *Store) ListJobs(ctx context.Context, userID string) ([]board.Job, error) {
	return s.queryJobs(ctx, ` WHERE user_id = $1 ORDER BY created_at DESC`, userID)
}

func (s *Store) RunningJobs(ctx context.Context) ([]board.Job, error) {
	return s.queryJobs(ctx, ` WHERE status = 'running' ORDER BY updated_at`)
}

func (s *Store) queryJobs(ctx context.Context, where string, args ...any) ([]board.Job, error) {
	rows, err := s.db.QueryContext(ctx, jobSelect+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []board.Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if jobs == nil {
		jobs = []board.Job{}
	}
	return jobs, rows.Err()
}

func (s *Store) CreateJob(ctx context.Context, job board.Job) error {
	raw, err := json.Marshal(job.Checklist)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO jobs (
			id, user_id, title, address, status, error, run_id, filename, checklist, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, job.ID, job.UserID, job.Title, job.Address, job.Status, job.Error, job.RunID, job.Filename, string(raw), job.CreatedAt.UTC(), job.UpdatedAt.UTC())
	return err
}

func (s *Store) Job(ctx context.Context, id string) (board.Job, error) {
	row := s.db.QueryRowContext(ctx, jobSelect+` WHERE id = $1`, id)
	job, err := scanJob(row)
	return job, mapNotFound(err)
}

func (s *Store) UpdateJob(ctx context.Context, job board.Job) error {
	raw, err := json.Marshal(job.Checklist)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE jobs
		SET status = $1, error = $2, run_id = $3, filename = $4, checklist = $5, updated_at = $6
		WHERE id = $7
	`, job.Status, job.Error, job.RunID, job.Filename, string(raw), job.UpdatedAt.UTC(), job.ID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return board.ErrNotFound
	}
	return nil
}

// StartJob moves a draft to running with runID. A sheet that already left draft comes back unchanged.
func (s *Store) StartJob(ctx context.Context, id, runID string) (board.Job, error) {
	row := s.db.QueryRowContext(ctx, `
		UPDATE jobs SET status = 'running', run_id = $1, updated_at = $2
		WHERE id = $3 AND status = 'draft'
		RETURNING `+jobColumns, runID, time.Now().UTC(), id)
	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return s.Job(ctx, id)
	}
	if isUnique(err) {
		return board.Job{}, board.ErrDuplicate
	}
	return job, err
}

// AttachBrochure sets the filename of a draft that has none yet. Any other sheet is ErrDuplicate.
func (s *Store) AttachBrochure(ctx context.Context, id, filename string) (board.Job, error) {
	row := s.db.QueryRowContext(ctx, `
		UPDATE jobs SET filename = $1, updated_at = $2
		WHERE id = $3 AND status = 'draft' AND filename = ''
		RETURNING `+jobColumns, filename, time.Now().UTC(), id)
	job, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return board.Job{}, board.ErrDuplicate
	}
	return job, err
}

const jobColumns = `id, user_id, title, address, status, error, run_id, filename, checklist, created_at, updated_at`

const jobSelect = `
	SELECT ` + jobColumns + `
	FROM jobs`

type scanner interface {
	Scan(dest ...any) error
}

func scanJob(row scanner) (board.Job, error) {
	var job board.Job
	var raw string
	err := row.Scan(
		&job.ID, &job.UserID, &job.Title, &job.Address, &job.Status, &job.Error,
		&job.RunID, &job.Filename, &raw, &job.CreatedAt, &job.UpdatedAt,
	)
	if err != nil {
		return board.Job{}, err
	}
	if err := json.Unmarshal([]byte(raw), &job.Checklist); err != nil {
		return board.Job{}, err
	}
	if job.Checklist == nil {
		job.Checklist = []board.Step{}
	}
	return job, nil
}

func mapNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return board.ErrNotFound
	}
	return err
}

func isUnique(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}
