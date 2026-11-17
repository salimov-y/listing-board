package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"listingboard/internal/board"
)

func TestStartLeavesTheStepsToTheWorker(t *testing.T) {
	srv, _ := newTestServer(t)
	cookie := signUp(t, srv, "ada@example.com")
	job := createSheet(t, srv, cookie, "Market loft", "10 Market Street")
	uploadFile(t, srv, cookie, job.ID, "brochure.pdf", []byte("%PDF-1.4\n"), "application/pdf")

	res := call(srv, http.MethodPost, "/api/jobs/"+job.ID+"/start", cookie, nil, "")
	if res.Code != http.StatusOK {
		t.Fatalf("status %d: %s", res.Code, res.Body.String())
	}
	got := decodeJob(t, res)
	if got.Status != "running" {
		t.Fatalf("status %s, want running", got.Status)
	}
	if got.RunID == "" {
		t.Fatal("runId is empty")
	}
	for i, step := range got.Checklist {
		if step.Label != board.NewChecklist()[i].Label || step.State != "pending" {
			t.Fatalf("step %d is %q %s, want %q pending", i, step.Label, step.State, board.NewChecklist()[i].Label)
		}
	}

	again := call(srv, http.MethodPost, "/api/jobs/"+job.ID+"/start", cookie, nil, "")
	if again.Code != http.StatusOK {
		t.Fatalf("second start %d: %s", again.Code, again.Body.String())
	}
	if second := decodeJob(t, again); second.RunID != got.RunID || second.Status != "running" {
		t.Fatalf("second start gave %s with runId %q, want running with %q", second.Status, second.RunID, got.RunID)
	}
}

func TestStartNeedsABrochure(t *testing.T) {
	srv, _ := newTestServer(t)
	cookie := signUp(t, srv, "ada@example.com")
	job := createSheet(t, srv, cookie, "Market loft", "10 Market Street")

	res := call(srv, http.MethodPost, "/api/jobs/"+job.ID+"/start", cookie, nil, "")
	if res.Code != http.StatusBadRequest || errorOf(t, res) != "Attach a brochure first" {
		t.Fatalf("status %d: %s", res.Code, res.Body.String())
	}
}

func TestStartConflictsWithAnotherRunningSheet(t *testing.T) {
	srv, _ := newTestServer(t)
	cookie := signUp(t, srv, "ada@example.com")
	first := createSheet(t, srv, cookie, "Market loft", "10 Market Street")
	second := createSheet(t, srv, cookie, "Harbour flat", "2 Quay Road")
	uploadFile(t, srv, cookie, first.ID, "first.txt", []byte("first"), "text/plain")
	uploadFile(t, srv, cookie, second.ID, "second.txt", []byte("second"), "text/plain")
	call(srv, http.MethodPost, "/api/jobs/"+first.ID+"/start", cookie, nil, "")

	res := call(srv, http.MethodPost, "/api/jobs/"+second.ID+"/start", cookie, nil, "")
	if res.Code != http.StatusConflict || errorOf(t, res) == "" {
		t.Fatalf("status %d: %s, want 409", res.Code, res.Body.String())
	}
	if got := getSheet(t, srv, cookie, second.ID); got.Status != "draft" {
		t.Fatalf("status %s, want draft", got.Status)
	}

	other := signUp(t, srv, "grace@example.com")
	job := createSheet(t, srv, other, "Mill house", "5 Mill Lane")
	uploadFile(t, srv, other, job.ID, "mill.txt", []byte("mill"), "text/plain")
	if res := call(srv, http.MethodPost, "/api/jobs/"+job.ID+"/start", other, nil, ""); res.Code != http.StatusOK {
		t.Fatalf("another account's start %d: %s", res.Code, res.Body.String())
	}
}

func TestStartKeepsAFailedSheetFailed(t *testing.T) {
	srv, _ := newTestServer(t)
	cookie := signUp(t, srv, "ada@example.com")
	sheet := createSheet(t, srv, cookie, "Market loft", "10 Market Street")
	uploadFile(t, srv, cookie, sheet.ID, "brochure.txt", []byte("hello"), "text/plain")
	job, err := srv.store.Job(context.Background(), sheet.ID)
	if err != nil {
		t.Fatal(err)
	}
	job.Status = "failed"
	job.RunID = board.NewID()
	job.Error = "Brochure is missing"
	if err := srv.store.UpdateJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}

	res := call(srv, http.MethodPost, "/api/jobs/"+job.ID+"/start", cookie, nil, "")
	got := decodeJob(t, res)
	if res.Code != http.StatusOK || got.Status != "failed" || got.RunID != job.RunID || got.Error != job.Error {
		t.Fatalf("status %d: %s", res.Code, res.Body.String())
	}
}

func TestUploadStoresTheBrochureAsBody(t *testing.T) {
	srv, dir := newTestServer(t)
	cookie := signUp(t, srv, "ada@example.com")
	job := createSheet(t, srv, cookie, "Market loft", "10 Market Street")
	saved := uploadFile(t, srv, cookie, job.ID, "brochure.pdf", []byte("%PDF-1.4\n"), "application/pdf")

	if saved.Filename != "brochure.pdf" {
		t.Fatalf("filename %q, want brochure.pdf", saved.Filename)
	}
	path := filepath.Join(dir, accountID(t, srv, cookie), job.ID, "body")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("brochure was not stored at %s: %v", path, err)
	}
	if string(data) != "%PDF-1.4\n" {
		t.Fatalf("stored %q", data)
	}
	if files := storedFiles(t, dir); len(files) != 1 {
		t.Fatalf("stored %v, want only %s", files, path)
	}
}

func TestUploadIgnoresDirectoriesInTheFilename(t *testing.T) {
	srv, dir := newTestServer(t)
	cookie := signUp(t, srv, "ada@example.com")
	job := createSheet(t, srv, cookie, "Market loft", "10 Market Street")
	saved := uploadFile(t, srv, cookie, job.ID, "../../escaped.txt", []byte("hello"), "text/plain")

	if saved.Filename != "escaped.txt" {
		t.Fatalf("filename %q, want escaped.txt", saved.Filename)
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "escaped.txt")); !os.IsNotExist(err) {
		t.Fatal("the upload wrote outside the data directory")
	}
	want := filepath.Join(dir, accountID(t, srv, cookie), job.ID, "body")
	if files := storedFiles(t, dir); len(files) != 1 || files[0] != want {
		t.Fatalf("stored %v, want only %s", files, want)
	}
}

func TestUploadDecidesTheTypeFromTheBytes(t *testing.T) {
	srv, dir := newTestServer(t)
	cookie := signUp(t, srv, "ada@example.com")
	user := accountID(t, srv, cookie)
	cases := []struct {
		name string
		data []byte
		want int
	}{
		{"pdf", []byte("%PDF-1.7\n\x00\xff"), http.StatusOK},
		{"text", []byte("Two bedrooms, café downstairs.\n"), http.StatusOK},
		{"empty", nil, http.StatusBadRequest},
		{"binary", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, http.StatusBadRequest},
		{"nul byte", []byte("hello\x00world"), http.StatusBadRequest},
		{"invalid utf-8", []byte("caf\xe9"), http.StatusBadRequest},
	}
	for _, c := range cases {
		job := createSheet(t, srv, cookie, c.name, "10 Market Street")
		res := upload(srv, cookie, job.ID, "photo.png", c.data, "image/png")
		if res.Code != c.want {
			t.Errorf("%s: status %d, want %d", c.name, res.Code, c.want)
		}
		_, err := os.Stat(filepath.Join(dir, user, job.ID, "body"))
		if c.want != http.StatusOK && !os.IsNotExist(err) {
			t.Errorf("%s: rejected brochure was left on disk", c.name)
		}
	}
}

func TestUploadRejectsFilesOverOneMebibyte(t *testing.T) {
	srv, dir := newTestServer(t)
	cookie := signUp(t, srv, "ada@example.com")
	job := createSheet(t, srv, cookie, "Market loft", "10 Market Street")

	res := upload(srv, cookie, job.ID, "big.txt", bytes.Repeat([]byte("a"), 1<<20+1), "text/plain")
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", res.Code)
	}
	if files := storedFiles(t, dir); len(files) != 0 {
		t.Fatalf("rejected brochure was left on disk: %v", files)
	}
	uploadFile(t, srv, cookie, job.ID, "exact.txt", bytes.Repeat([]byte("a"), 1<<20), "text/plain")
}

func TestSecondUploadIsRejected(t *testing.T) {
	srv, dir := newTestServer(t)
	cookie := signUp(t, srv, "ada@example.com")
	job := createSheet(t, srv, cookie, "Market loft", "10 Market Street")
	uploadFile(t, srv, cookie, job.ID, "first.txt", []byte("first"), "text/plain")

	res := upload(srv, cookie, job.ID, "second.txt", []byte("second"), "text/plain")
	if res.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", res.Code)
	}
	data, err := os.ReadFile(filepath.Join(dir, accountID(t, srv, cookie), job.ID, "body"))
	if err != nil || string(data) != "first" {
		t.Fatalf("stored %q, %v; want the first brochure", data, err)
	}
	if got := getSheet(t, srv, cookie, job.ID); got.Filename != "first.txt" {
		t.Fatalf("filename %q, want first.txt", got.Filename)
	}
}

func TestListHidesOtherUsersJobs(t *testing.T) {
	srv, _ := newTestServer(t)
	ada := signUp(t, srv, "ada@example.com")
	grace := signUp(t, srv, "grace@example.com")
	job := createSheet(t, srv, ada, "Market loft", "10 Market Street")

	res := call(srv, http.MethodGet, "/api/jobs", grace, nil, "")
	if res.Code != http.StatusOK {
		t.Fatalf("status %d: %s", res.Code, res.Body.String())
	}
	var jobs []board.Job
	if err := json.Unmarshal(res.Body.Bytes(), &jobs); err != nil {
		t.Fatal(err)
	}
	for _, item := range jobs {
		if item.ID == job.ID {
			t.Fatal("another account can see this sheet in the list")
		}
	}
}

func TestStartRequiresASession(t *testing.T) {
	srv, _ := newTestServer(t)
	res := call(srv, http.MethodPost, "/api/jobs/"+board.NewID()+"/start", "", nil, "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", res.Code)
	}
}

func TestOtherUsersSheetsAreNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	ada := signUp(t, srv, "ada@example.com")
	grace := signUp(t, srv, "grace@example.com")
	job := createSheet(t, srv, ada, "Market loft", "10 Market Street")
	uploadFile(t, srv, ada, job.ID, "brochure.pdf", []byte("%PDF-1.4\n"), "application/pdf")

	routes := []struct{ method, path, cookie string }{
		{http.MethodGet, "/api/jobs/" + job.ID, grace},
		{http.MethodPost, "/api/jobs/" + job.ID + "/file", grace},
		{http.MethodPost, "/api/jobs/" + job.ID + "/start", grace},
		{http.MethodGet, "/api/jobs/" + job.ID + "/file", grace},
		{http.MethodGet, "/api/jobs/" + board.NewID(), ada},
	}
	for _, route := range routes {
		res := call(srv, route.method, route.path, route.cookie, nil, "")
		if res.Code != http.StatusNotFound {
			t.Errorf("%s %s: status %d, want 404", route.method, route.path, res.Code)
		}
	}
	if got := getSheet(t, srv, ada, job.ID); got.Status != "draft" {
		t.Fatalf("status %s after another account tried to start it, want draft", got.Status)
	}
}

func newTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	return New(newMem(), dir, ""), dir
}

func signUp(t *testing.T, srv *Server, email string) string {
	t.Helper()
	body := strings.NewReader(`{"email":"` + email + `","password":"password123"}`)
	res := call(srv, http.MethodPost, "/api/register", "", body, "application/json")
	if res.Code != http.StatusCreated {
		t.Fatalf("register %d: %s", res.Code, res.Body.String())
	}
	for _, cookie := range res.Result().Cookies() {
		if cookie.Name == sessionCookie {
			return cookie.Value
		}
	}
	t.Fatal("register did not set a session")
	return ""
}

func createSheet(t *testing.T, srv *Server, cookie, title, address string) board.Job {
	t.Helper()
	body := strings.NewReader(`{"title":"` + title + `","address":"` + address + `"}`)
	res := call(srv, http.MethodPost, "/api/jobs", cookie, body, "application/json")
	if res.Code != http.StatusCreated {
		t.Fatalf("create %d: %s", res.Code, res.Body.String())
	}
	return decodeJob(t, res)
}

func getSheet(t *testing.T, srv *Server, cookie, id string) board.Job {
	t.Helper()
	res := call(srv, http.MethodGet, "/api/jobs/"+id, cookie, nil, "")
	if res.Code != http.StatusOK {
		t.Fatalf("get %d: %s", res.Code, res.Body.String())
	}
	return decodeJob(t, res)
}

func upload(srv *Server, cookie, id, name string, data []byte, contentType string) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, name))
	header.Set("Content-Type", contentType)
	part, _ := writer.CreatePart(header)
	_, _ = part.Write(data)
	_ = writer.Close()
	return call(srv, http.MethodPost, "/api/jobs/"+id+"/file", cookie, &buf, writer.FormDataContentType())
}

func uploadFile(t *testing.T, srv *Server, cookie, id, name string, data []byte, contentType string) board.Job {
	t.Helper()
	res := upload(srv, cookie, id, name, data, contentType)
	if res.Code != http.StatusOK {
		t.Fatalf("upload %d: %s", res.Code, res.Body.String())
	}
	return decodeJob(t, res)
}

func accountID(t *testing.T, srv *Server, cookie string) string {
	t.Helper()
	res := call(srv, http.MethodGet, "/api/me", cookie, nil, "")
	var me account
	if err := json.Unmarshal(res.Body.Bytes(), &me); err != nil || me.ID == "" {
		t.Fatalf("me %d: %s", res.Code, res.Body.String())
	}
	return me.ID
}

func storedFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			files = append(files, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func errorOf(t *testing.T, res *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body is not JSON: %s", res.Body.String())
	}
	return body.Error
}

func decodeJob(t *testing.T, res *httptest.ResponseRecorder) board.Job {
	t.Helper()
	var job board.Job
	if err := json.Unmarshal(res.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	return job
}

func call(srv *Server, method, path, cookie string, body io.Reader, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, body)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	return res
}

type mem struct {
	mu       sync.Mutex
	users    map[string]board.User
	byEmail  map[string]string
	sessions map[string]memSession
	jobs     map[string]board.Job
}

type memSession struct {
	userID  string
	expires time.Time
}

func newMem() *mem {
	return &mem{
		users:    map[string]board.User{},
		byEmail:  map[string]string{},
		sessions: map[string]memSession{},
		jobs:     map[string]board.Job{},
	}
}

func (m *mem) CreateUser(_ context.Context, email, passwordHash string) (board.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byEmail[email]; ok {
		return board.User{}, board.ErrDuplicate
	}
	user := board.User{ID: board.NewID(), Email: email, PasswordHash: passwordHash}
	m.users[user.ID] = user
	m.byEmail[email] = user.ID
	return user, nil
}

func (m *mem) UserByEmail(_ context.Context, email string) (board.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.byEmail[email]
	if !ok {
		return board.User{}, board.ErrNotFound
	}
	return m.users[id], nil
}

func (m *mem) CreateSession(_ context.Context, userID, token string, expires time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[token] = memSession{userID: userID, expires: expires}
	return nil
}

func (m *mem) DeleteSession(_ context.Context, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, token)
	return nil
}

func (m *mem) UserByToken(_ context.Context, token string) (board.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[token]
	if !ok || time.Now().After(session.expires) {
		return board.User{}, board.ErrNotFound
	}
	user, ok := m.users[session.userID]
	if !ok {
		return board.User{}, board.ErrNotFound
	}
	return user, nil
}

func (m *mem) ListJobs(_ context.Context, userID string) ([]board.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var jobs []board.Job
	for _, job := range m.jobs {
		if job.UserID == userID {
			jobs = append(jobs, cloneJob(job))
		}
	}
	if jobs == nil {
		jobs = []board.Job{}
	}
	return jobs, nil
}

func (m *mem) CreateJob(_ context.Context, job board.Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs[job.ID] = cloneJob(job)
	return nil
}

func (m *mem) Job(_ context.Context, id string) (board.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok {
		return board.Job{}, board.ErrNotFound
	}
	return cloneJob(job), nil
}

func (m *mem) UpdateJob(_ context.Context, job board.Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.jobs[job.ID]; !ok {
		return board.ErrNotFound
	}
	for _, other := range m.jobs {
		if job.Status == "running" && other.Status == "running" && other.UserID == job.UserID && other.ID != job.ID {
			return board.ErrDuplicate
		}
	}
	m.jobs[job.ID] = cloneJob(job)
	return nil
}

func cloneJob(job board.Job) board.Job {
	job.Checklist = append([]board.Step(nil), job.Checklist...)
	return job
}
