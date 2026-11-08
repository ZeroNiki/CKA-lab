package api

import (
	"context"
	"encoding/json"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"cka-lab/internal/check"
	"cka-lab/internal/content"
	"cka-lab/internal/game"
	"cka-lab/internal/progress"
)

type fakeChecker struct{ err error }

func (f *fakeChecker) Check(context.Context, *content.Task) ([]check.Result, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []check.Result{{Description: "ok", Pass: true}}, nil
}

type nopPreparer struct{}

func (nopPreparer) Prepare(context.Context, *content.Task) error { return nil }

type blockingSnap struct {
	started, release chan struct{}
	missing          bool
}

func (b *blockingSnap) Exists(context.Context) (bool, error) { return !b.missing, nil }

func (b *blockingSnap) Restore(context.Context) error {
	if b.started != nil {
		close(b.started)
		<-b.release
	}
	return nil
}

type env struct {
	srv   *httptest.Server
	store *progress.Store
	ch    *fakeChecker
	snap  *blockingSnap
}

func newEnv(t *testing.T) *env {
	t.Helper()
	c, err := content.Load("../content/testdata/content")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := game.LoadConfig("../../config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	st, err := progress.Open(filepath.Join(t.TempDir(), "p.json"))
	if err != nil {
		t.Fatal(err)
	}
	e := &env{store: st, ch: &fakeChecker{}, snap: &blockingSnap{}}
	svc := game.NewService(c, cfg, st, e.ch, nopPreparer{}, e.snap, rand.New(rand.NewPCG(1, 1)), time.Now)
	static := fstest.MapFS{"index.html": {Data: []byte("<h1>CKA Lab</h1>")}}
	e.srv = httptest.NewServer(New(svc, static))
	t.Cleanup(e.srv.Close)
	return e
}

func (e *env) do(t *testing.T, method, path, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var m map[string]any
	if len(b) > 0 && b[0] == '{' {
		json.Unmarshal(b, &m)
	}
	return resp.StatusCode, m
}

func (e *env) passTheory() {
	e.store.Update(func(s *progress.State) error { s.TheoryPassed["demo"] = true; return nil })
}

func TestAPI_ServesIndex(t *testing.T) {
	e := newEnv(t)
	resp, err := http.Get(e.srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(b), "CKA Lab") {
		t.Fatalf("status %d body %q", resp.StatusCode, b)
	}
}

func TestAPI_Levels(t *testing.T) {
	e := newEnv(t)
	resp, _ := http.Get(e.srv.URL + "/api/levels")
	var levels []map[string]any
	json.NewDecoder(resp.Body).Decode(&levels)
	if len(levels) != 1 || levels[0]["id"] != "demo" || levels[0]["total"] != float64(7) {
		t.Fatalf("levels = %v", levels)
	}
	if tasks := levels[0]["tasks"].([]any); len(tasks) != 7 {
		t.Fatalf("tasks = %v", tasks)
	}
}

func TestAPI_TaskHidesSolutionUntilSolved(t *testing.T) {
	e := newEnv(t)
	code, task := e.do(t, "GET", "/api/tasks/demo-t1", "")
	if code != 200 || task["statement"] == "" {
		t.Fatalf("code %d task %v", code, task)
	}
	if _, ok := task["solution"]; ok {
		t.Fatal("solution must be hidden before solving")
	}
	if _, ok := task["docs"]; ok {
		t.Fatal("docs must be hidden before solving")
	}
	e.do(t, "POST", "/api/tasks/demo-t1/start", `{"mode":"practice"}`)
	e.do(t, "POST", "/api/tasks/demo-t1/check", "")
	_, task = e.do(t, "GET", "/api/tasks/demo-t1", "")
	if task["solution"] == nil || task["docs"] == nil {
		t.Fatalf("solution should be visible after solving: %v", task)
	}
}

func TestAPI_StartLevelTaskWithoutTheory400(t *testing.T) {
	e := newEnv(t)
	code, body := e.do(t, "POST", "/api/tasks/demo-t1/start", `{"mode":"levels"}`)
	if code != 400 || !strings.Contains(body["error"].(string), "theory") {
		t.Fatalf("code %d body %v", code, body)
	}
}

func TestAPI_StartBadMode400(t *testing.T) {
	e := newEnv(t)
	if code, _ := e.do(t, "POST", "/api/tasks/demo-t1/start", `{"mode":"god"}`); code != 400 {
		t.Fatalf("code %d", code)
	}
}

func TestAPI_CheckUnavailable503(t *testing.T) {
	e := newEnv(t)
	e.do(t, "POST", "/api/tasks/demo-t1/start", `{"mode":"practice"}`)
	e.ch.err = check.ErrUnavailable
	code, body := e.do(t, "POST", "/api/tasks/demo-t1/check", "")
	if code != 503 || !strings.Contains(body["error"].(string), "./lab up") {
		t.Fatalf("code %d body %v", code, body)
	}
}

func TestAPI_CheckOutcome(t *testing.T) {
	e := newEnv(t)
	e.passTheory()
	e.do(t, "POST", "/api/tasks/demo-t3/start", `{"mode":"levels"}`)
	code, out := e.do(t, "POST", "/api/tasks/demo-t3/check", "")
	if code != 200 || out["pass"] != true || out["coins"] != float64(30) {
		t.Fatalf("code %d out %v", code, out)
	}
}

func TestAPI_BusyReturns409(t *testing.T) {
	e := newEnv(t)
	e.snap.started, e.snap.release = make(chan struct{}), make(chan struct{})
	e.do(t, "POST", "/api/tasks/demo-t7/start", `{"mode":"practice"}`)
	done := make(chan struct{})
	go func() {
		e.do(t, "POST", "/api/tasks/demo-t7/check", "")
		close(done)
	}()
	<-e.snap.started
	code, _ := e.do(t, "POST", "/api/tasks/demo-t1/start", `{"mode":"practice"}`)
	_, state := e.do(t, "GET", "/api/state", "")
	close(e.snap.release)
	<-done
	if code != 409 {
		t.Fatalf("code %d", code)
	}
	if state["busy"] != true {
		t.Fatalf("state = %v", state)
	}
}

func TestAPI_BuyInsufficient402(t *testing.T) {
	e := newEnv(t)
	code, body := e.do(t, "POST", "/api/shop/break-5/buy", "")
	if code != 402 || body["error"] == "" {
		t.Fatalf("code %d body %v", code, body)
	}
}

func TestAPI_QuizFlow(t *testing.T) {
	e := newEnv(t)
	code, qz := e.do(t, "POST", "/api/levels/demo/theory", "")
	if code != 200 {
		t.Fatalf("code %d", code)
	}
	id := qz["quizId"].(string)
	var last map[string]any
	for _, raw := range qz["questions"].([]any) {
		q := raw.(map[string]any)
		right := 0
		for i, o := range q["options"].([]any) {
			if o == "Right" {
				right = i
			}
		}
		body, _ := json.Marshal(map[string]any{"questionId": q["id"], "option": right})
		code, last = e.do(t, "POST", "/api/quiz/"+id+"/answer", string(body))
		if code != 200 {
			t.Fatalf("answer code %d %v", code, last)
		}
	}
	if last["passed"] != true {
		t.Fatalf("last = %v", last)
	}
	if code, _ := e.do(t, "POST", "/api/tasks/demo-t1/start", `{"mode":"levels"}`); code != 204 {
		t.Fatalf("start after theory: %d", code)
	}
}

func TestAPI_UnknownTask404(t *testing.T) {
	e := newEnv(t)
	if code, _ := e.do(t, "GET", "/api/tasks/nope", ""); code != 404 {
		t.Fatalf("code %d", code)
	}
}

func TestAPI_MixedNextAndAnswer(t *testing.T) {
	e := newEnv(t)
	for range 50 {
		code, item := e.do(t, "GET", "/api/mixed/next?practice=0&snapshot=0", "")
		if code != 200 {
			t.Fatalf("code %d", code)
		}
		if item["kind"] == "question" {
			q := item["question"].(map[string]any)
			body, _ := json.Marshal(map[string]any{"questionId": q["id"], "option": 0})
			if code, _ := e.do(t, "POST", "/api/mixed/answer", string(body)); code != 200 {
				t.Fatalf("answer code %d", code)
			}
			return
		}
	}
	t.Fatal("no question in 50 tries")
}

func TestAPI_TaskShowsActiveAttempt(t *testing.T) {
	e := newEnv(t)
	_, task := e.do(t, "GET", "/api/tasks/demo-t1", "")
	if task["active"] != "" {
		t.Fatalf("active before start = %v", task["active"])
	}
	e.do(t, "POST", "/api/tasks/demo-t1/start", `{"mode":"practice"}`)
	_, task = e.do(t, "GET", "/api/tasks/demo-t1", "")
	if task["active"] != "practice" {
		t.Fatalf("active after start = %v", task["active"])
	}
}

func TestAPI_SnapshotMissing409(t *testing.T) {
	e := newEnv(t)
	e.snap.missing = true
	code, body := e.do(t, "POST", "/api/tasks/demo-t7/start", `{"mode":"practice"}`)
	if code != 409 || !strings.Contains(body["error"].(string), "snapshot-base") {
		t.Fatalf("code %d body %v", code, body)
	}
}
