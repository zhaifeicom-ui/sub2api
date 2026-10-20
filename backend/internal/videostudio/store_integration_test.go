package videostudio

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	_ "github.com/lib/pq"
)

// Tests use a new isolated schema and never touch an existing application's rows.
func integrationStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("VIDEO_TEST_DSN")
	if dsn == "" {
		t.Skip("VIDEO_TEST_DSN is required for PostgreSQL integration tests")
	}
	root, e := sql.Open("postgres", dsn)
	if e != nil {
		t.Fatal(e)
	}
	schema := newID("video_test_")
	if _, e = root.Exec(`CREATE SCHEMA ` + schema); e != nil {
		t.Fatal(e)
	}
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	db, e := sql.Open("postgres", dsn+separator+"search_path="+schema)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close(); _, _ = root.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); root.Close() })
	_, e = db.Exec(`CREATE TABLE users(id bigint PRIMARY KEY,balance numeric(20,8),frozen_balance numeric(20,8) DEFAULT 0,status text DEFAULT 'active',deleted_at timestamptz,updated_at timestamptz DEFAULT now());
 CREATE TABLE groups(id bigint PRIMARY KEY,platform text DEFAULT 'openai',status text DEFAULT 'active',subscription_type text DEFAULT 'standard',allow_image_generation bool DEFAULT true,deleted_at timestamptz);
 CREATE TABLE accounts(id bigint PRIMARY KEY,credentials jsonb,type text DEFAULT 'apikey',platform text DEFAULT 'openai',status text DEFAULT 'active',schedulable bool DEFAULT true,expires_at timestamptz,deleted_at timestamptz);
 CREATE TABLE account_groups(account_id bigint,group_id bigint);
 CREATE TABLE api_keys(id bigint PRIMARY KEY,user_id bigint,group_id bigint,status text DEFAULT 'active',deleted_at timestamptz,expires_at timestamptz,quota numeric DEFAULT 0,quota_used numeric DEFAULT 0,rate_limit_5h numeric DEFAULT 0,rate_limit_1d numeric DEFAULT 0,rate_limit_7d numeric DEFAULT 0,usage_5h numeric DEFAULT 0,usage_1d numeric DEFAULT 0,usage_7d numeric DEFAULT 0,window_5h_start timestamptz,window_1d_start timestamptz,window_7d_start timestamptz);
 INSERT INTO users(id,balance) VALUES(1,100),(2,100);INSERT INTO groups(id) VALUES(1);
 INSERT INTO accounts(id,credentials) VALUES(1,'{"base_url":"https://laneai.dev/v1","api_key":"test-only"}');INSERT INTO account_groups VALUES(1,1);
 INSERT INTO api_keys(id,user_id,group_id) VALUES(1,1,1),(2,2,1);`)
	if e != nil {
		t.Fatal(e)
	}
	migration, e := os.ReadFile("../../migrations/239_video_studio.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(string(migration)); e != nil {
		t.Fatal(e)
	}
	s := &Store{DB: db}
	if e = s.SaveChannel(context.Background(), testChannel()); e != nil {
		t.Fatal(e)
	}
	return s
}
func assertMoney(t *testing.T, s *Store, balance, frozen, quota, reserved float64) {
	t.Helper()
	var b, f, q, r float64
	e := s.DB.QueryRow(`SELECT balance,frozen_balance,quota_used,video_reserved FROM users JOIN api_keys ON api_keys.user_id=users.id WHERE api_keys.id=1`).Scan(&b, &f, &q, &r)
	if e != nil {
		t.Fatal(e)
	}
	if b != balance || f != frozen || q != quota || r != reserved {
		t.Fatalf("money got %.8f %.8f %.8f %.8f; want %.8f %.8f %.8f %.8f", b, f, q, r, balance, frozen, quota, reserved)
	}
}
func reserveTest(t *testing.T, s *Store, idem string) Task {
	t.Helper()
	r := testRequest()
	p, e := Quote(testChannel(), r)
	if e != nil {
		t.Fatal(e)
	}
	got, e := s.Reserve(context.Background(), 1, testChannel(), r, p, idem)
	if e != nil {
		t.Fatal(e)
	}
	return got
}
func TestReserveConcurrentAndSettleExactlyOnce(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	r := testRequest()
	p, _ := Quote(testChannel(), r)
	var wg sync.WaitGroup
	ids := make(chan string, 20)
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			job, e := s.Reserve(ctx, 1, testChannel(), r, p, "same-client-request")
			if e != nil {
				errs <- e
				return
			}
			ids <- job.ID
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	close(ids)
	id := ""
	for got := range ids {
		if id != "" && id != got {
			t.Fatal("duplicate task created")
		}
		id = got
	}
	assertMoney(t, s, 99.12, .88, .88, .88)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := s.Finish(ctx, id, "completed", "", []string{"https://example.com/video.mp4"}); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	assertMoney(t, s, 99.12, 0, .88, 0)
	// Later contradictory fail cannot refund an already completed task.
	if e := s.Finish(ctx, id, "failed", "late failure", nil); e != nil {
		t.Fatal(e)
	}
	assertMoney(t, s, 99.12, 0, .88, 0)
	r.Input.Prompt = "changed"
	if _, e := s.Reserve(ctx, 1, testChannel(), r, p, "same-client-request"); !errors.Is(e, ErrConflict) {
		t.Fatalf("wanted conflict: %v", e)
	}
	if _, e := s.Task(ctx, 2, id); !errors.Is(e, ErrNotFound) {
		t.Fatalf("cross-user lookup accepted: %v", e)
	}
}
func TestFailureRefundAndReservationRollback(t *testing.T) {
	s := integrationStore(t)
	job := reserveTest(t, s, "refund-request")
	if e := s.Finish(context.Background(), job.ID, "failed", "upstream rejected", nil); e != nil {
		t.Fatal(e)
	}
	if e := s.Finish(context.Background(), job.ID, "failed", "again", nil); e != nil {
		t.Fatal(e)
	}
	assertMoney(t, s, 100, 0, 0, 0)
	r := testRequest()
	p, _ := Quote(testChannel(), r)
	bad := testChannel()
	bad.AccountID = 999
	if _, e := s.Reserve(context.Background(), 1, bad, r, p, "bad-account-request"); e == nil {
		t.Fatal("missing account accepted")
	}
	assertMoney(t, s, 100, 0, 0, 0)
	_, _ = s.DB.Exec(`UPDATE api_keys SET quota=.5 WHERE id=1`)
	if _, e := s.Reserve(context.Background(), 1, testChannel(), r, p, "over-budget-request"); !errors.Is(e, ErrFunds) {
		t.Fatalf("quota not enforced: %v", e)
	}
	assertMoney(t, s, 100, 0, 0, 0)
}
func TestWorkerRecoveryAndContract(t *testing.T) {
	s := integrationStore(t)
	job := reserveTest(t, s, "worker-request")
	w := NewWorker(s)
	var submits atomic.Int32
	w.Client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		text := `{"id":"upstream_1","status":"queued"}`
		if r.Method == "POST" {
			submits.Add(1)
			var p struct {
				Model string `json:"model"`
				Input Input  `json:"input"`
			}
			if e := json.NewDecoder(r.Body).Decode(&p); e != nil || p.Input.Prompt == "" {
				t.Errorf("invalid payload %v", e)
			}
		} else {
			text = `{"id":"upstream_1","status":"completed","outputs":["https://example.com/out.mp4"]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(text)), Header: make(http.Header)}, nil
	})
	id, e := w.claim(context.Background())
	if e != nil || id != job.ID {
		t.Fatalf("claim %s %v", id, e)
	}
	w.process(context.Background(), id)
	got, e := s.Task(context.Background(), 1, id)
	if e != nil || got.UpstreamID != "upstream_1" || got.Status != "queued" {
		t.Fatalf("got %+v %v", got, e)
	}
	// Reconstructing a worker models process restart: poll saved ID, never recreate.
	restarted := NewWorker(s)
	restarted.Client = w.Client
	restarted.process(context.Background(), id)
	assertMoney(t, s, 99.12, 0, .88, 0)
	if submits.Load() != 1 {
		t.Fatal("restart recreated upstream task")
	}
}
func TestAmbiguousSubmitRetainsHoldAndNeverRetries(t *testing.T) {
	s := integrationStore(t)
	job := reserveTest(t, s, "timeout-request")
	w := NewWorker(s)
	var n atomic.Int32
	w.Client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		n.Add(1)
		return nil, errors.New("connection reset after write")
	})
	if _, e := w.claim(context.Background()); e != nil {
		t.Fatal(e)
	}
	w.process(context.Background(), job.ID)
	got, e := s.Task(context.Background(), 1, job.ID)
	if e != nil || got.Status != "needs_review" || got.Settled {
		t.Fatalf("%+v %v", got, e)
	}
	assertMoney(t, s, 99.12, .88, .88, .88)
	w.Tick(context.Background())
	if n.Load() != 1 {
		t.Fatal("ambiguous create retried")
	}
}
func TestConcurrentClaimsRespectAccountCapacity(t *testing.T) {
	s := integrationStore(t)
	reserveTest(t, s, "capacity-request-1")
	reserveTest(t, s, "capacity-request-2")
	w := NewWorker(s)
	var claims atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := w.claim(context.Background()); e == nil {
				claims.Add(1)
			} else if !errors.Is(e, sql.ErrNoRows) {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if claims.Load() != 1 {
		t.Fatalf("claimed %d for account capacity 1", claims.Load())
	}
}

func TestBatchAtomicRollbackAndCancel(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	r := testRequest()
	p, _ := Quote(testChannel(), r)
	items := []Reservation{{Channel: testChannel(), Request: r, Price: p, Idempotency: "batch-request:0"}, {Channel: testChannel(), Request: r, Price: p, Idempotency: "batch-request:1"}}
	_, _ = s.DB.Exec(`UPDATE users SET balance=1 WHERE id=1`)
	if _, e := s.ReserveMany(ctx, 1, items); !errors.Is(e, ErrFunds) {
		t.Fatalf("expected atomic insufficient funds: %v", e)
	}
	assertMoney(t, s, 1, 0, 0, 0)
	var n int
	_ = s.DB.QueryRow(`SELECT count(*) FROM video_studio_tasks`).Scan(&n)
	if n != 0 {
		t.Fatal("partially accepted batch")
	}
	_, _ = s.DB.Exec(`UPDATE users SET balance=100 WHERE id=1`)
	out, e := s.ReserveMany(ctx, 1, items)
	if e != nil || len(out) != 2 {
		t.Fatalf("batch %v", e)
	}
	assertMoney(t, s, 98.24, 1.76, 1.76, 1.76)
	if e = s.CancelPending(ctx, 2, out[0].ID); e == nil {
		t.Fatal("cross-user cancellation accepted")
	}
	if e = s.CancelPending(ctx, 1, out[0].ID); e != nil {
		t.Fatal(e)
	}
	assertMoney(t, s, 99.12, .88, .88, .88)
	_ = s.Review(ctx, out[1].ID, "needs inspection")
	if e = s.Attach(ctx, out[1].ID, "verified_task"); e != nil {
		t.Fatal(e)
	}
	got, e := s.Task(ctx, 1, out[1].ID)
	if e != nil || got.Status != "queued" || got.UpstreamID != "verified_task" {
		t.Fatalf("attach %+v %v", got, e)
	}
	if e = s.CancelPending(ctx, 1, out[1].ID); e == nil {
		t.Fatal("accepted upstream task cancelled unsafely")
	}
}
