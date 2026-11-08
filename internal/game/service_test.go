package game

import (
	"context"
	"errors"
	"math/rand/v2"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"cka-lab/internal/check"
	"cka-lab/internal/content"
	"cka-lab/internal/progress"
)

type fakeChecker struct {
	pass bool
	err  error
}

func (f *fakeChecker) Check(context.Context, *content.Task) ([]check.Result, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []check.Result{{Description: "d", Pass: f.pass}}, nil
}

type fakePreparer struct{ prepared []string }

func (f *fakePreparer) Prepare(_ context.Context, t *content.Task) error {
	f.prepared = append(f.prepared, t.ID)
	return nil
}

type fakeSnap struct {
	mu       sync.Mutex
	restores int
	block    chan struct{} // if set, Restore waits on it
	started  chan struct{}
}

func (f *fakeSnap) Restore(context.Context) error {
	if f.started != nil {
		close(f.started)
	}
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	f.restores++
	f.mu.Unlock()
	return nil
}

var testNow = time.Date(2026, 11, 8, 18, 0, 0, 0, time.UTC)

type harness struct {
	svc   *Service
	store *progress.Store
	ch    *fakeChecker
	pr    *fakePreparer
	sn    *fakeSnap
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	c, err := content.Load("../content/testdata/content")
	if err != nil {
		t.Fatal(err)
	}
	st, err := progress.Open(filepath.Join(t.TempDir(), "progress.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{store: st, ch: &fakeChecker{pass: true}, pr: &fakePreparer{}, sn: &fakeSnap{}}
	h.svc = NewService(c, testConfig(), st, h.ch, h.pr, h.sn, rand.New(rand.NewPCG(1, 1)), func() time.Time { return testNow })
	return h
}

func (h *harness) passTheory(t *testing.T) {
	t.Helper()
	h.store.Update(func(s *progress.State) error { s.TheoryPassed["demo"] = true; return nil })
}

func (h *harness) solve(t *testing.T, mode Mode, id string) CheckOutcome {
	t.Helper()
	if err := h.svc.StartTask(context.Background(), mode, id); err != nil {
		t.Fatalf("start %s: %v", id, err)
	}
	out, err := h.svc.CheckTask(context.Background(), id)
	if err != nil {
		t.Fatalf("check %s: %v", id, err)
	}
	return out
}

func TestStartTheory_PassMarksLevel(t *testing.T) {
	h := newHarness(t)
	qz, err := h.svc.StartTheory("demo")
	if err != nil {
		t.Fatal(err)
	}
	var res AnswerResult
	for _, q := range qz.Questions {
		res, err = h.svc.AnswerTheory(qz.ID, q.ID, rightOption(q))
		if err != nil {
			t.Fatal(err)
		}
	}
	if !res.Passed || !h.store.Get().TheoryPassed["demo"] {
		t.Fatalf("res = %+v, theoryPassed = %v", res, h.store.Get().TheoryPassed)
	}
	if !strings.HasPrefix(res.Explanation, "Because ") {
		t.Fatalf("explanation should come from content, got %q", res.Explanation)
	}
}

func TestStartTask_LevelsRequiresTheory(t *testing.T) {
	h := newHarness(t)
	if err := h.svc.StartTask(context.Background(), ModeLevels, "demo-t1"); !errors.Is(err, ErrTheoryRequired) {
		t.Fatalf("err = %v", err)
	}
	if len(h.pr.prepared) != 0 {
		t.Fatal("task must not be prepared without theory")
	}
}

func TestStartTask_PracticeNoTheory(t *testing.T) {
	h := newHarness(t)
	if err := h.svc.StartTask(context.Background(), ModePractice, "demo-t1"); err != nil {
		t.Fatal(err)
	}
	if len(h.pr.prepared) != 1 {
		t.Fatal("task should be prepared")
	}
}

func TestStartTask_Unknown(t *testing.T) {
	h := newHarness(t)
	if err := h.svc.StartTask(context.Background(), ModePractice, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestCheckTask_FirstSolveHardAwards30(t *testing.T) {
	h := newHarness(t)
	h.passTheory(t)
	out := h.solve(t, ModeLevels, "demo-t3")
	if !out.Pass || out.Coins != 30 || h.store.Get().Coins != 30 || !h.store.Get().LevelSolved["demo-t3"] {
		t.Fatalf("out = %+v state = %+v", out, h.store.Get())
	}
}

func TestCheckTask_RepeatAwards30Percent(t *testing.T) {
	h := newHarness(t)
	h.passTheory(t)
	h.solve(t, ModeLevels, "demo-t3")
	out := h.solve(t, ModeLevels, "demo-t3")
	if out.Coins != 9 || h.store.Get().Coins != 39 {
		t.Fatalf("out = %+v coins = %d", out, h.store.Get().Coins)
	}
}

func TestCheckTask_SecondCheckAfterPassAwardsNothing(t *testing.T) {
	h := newHarness(t)
	h.passTheory(t)
	h.solve(t, ModeLevels, "demo-t3")
	if _, err := h.svc.CheckTask(context.Background(), "demo-t3"); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("err = %v, want ErrNotStarted", err)
	}
	if h.store.Get().Coins != 30 {
		t.Fatalf("coins = %d", h.store.Get().Coins)
	}
}

func TestCheckTask_FailKeepsAttempt(t *testing.T) {
	h := newHarness(t)
	h.passTheory(t)
	h.svc.StartTask(context.Background(), ModeLevels, "demo-t3")
	h.ch.pass = false
	out, err := h.svc.CheckTask(context.Background(), "demo-t3")
	if err != nil || out.Pass || out.Coins != 0 {
		t.Fatalf("out = %+v err = %v", out, err)
	}
	h.ch.pass = true
	if out, err := h.svc.CheckTask(context.Background(), "demo-t3"); err != nil || !out.Pass {
		t.Fatalf("retry after fail: out = %+v err = %v", out, err)
	}
}

func TestCheckTask_PracticeDoesNotMarkLevel(t *testing.T) {
	h := newHarness(t)
	out := h.solve(t, ModePractice, "demo-t3")
	st := h.store.Get()
	if out.Coins != 30 || st.LevelSolved["demo-t3"] || st.SolvedCount["demo-t3"] != 1 {
		t.Fatalf("out = %+v state = %+v", out, st)
	}
}

func TestCheckTask_SolveInPracticeThenLevelGivesRepeat(t *testing.T) {
	h := newHarness(t)
	h.solve(t, ModeMixed, "demo-t3")
	h.passTheory(t)
	out := h.solve(t, ModeLevels, "demo-t3")
	if out.Coins != 9 || !h.store.Get().LevelSolved["demo-t3"] {
		t.Fatalf("out = %+v", out)
	}
}

func TestCheckTask_SeventhSolveAddsBonusOnce(t *testing.T) {
	h := newHarness(t)
	h.passTheory(t)
	var out CheckOutcome
	for _, id := range []string{"demo-t1", "demo-t2", "demo-t3", "demo-t4", "demo-t5", "demo-t6", "demo-t7"} {
		out = h.solve(t, ModeLevels, id)
	}
	// 2×20 + 5×30 + 50 bonus
	if !out.LevelCompleted || !out.Bonus || h.store.Get().Coins != 240 {
		t.Fatalf("out = %+v coins = %d", out, h.store.Get().Coins)
	}
	again := h.solve(t, ModeLevels, "demo-t1")
	if again.Bonus || h.store.Get().Coins != 246 {
		t.Fatalf("bonus paid twice: %+v coins = %d", again, h.store.Get().Coins)
	}
}

func TestCheckTask_RevealedGivesZeroButCounts(t *testing.T) {
	h := newHarness(t)
	h.passTheory(t)
	docs, sol, err := h.svc.Reveal("demo-t3")
	if err != nil || docs == "" || sol == "" {
		t.Fatalf("reveal: %q %q %v", docs, sol, err)
	}
	out := h.solve(t, ModeLevels, "demo-t3")
	if out.Coins != 0 || !h.store.Get().LevelSolved["demo-t3"] {
		t.Fatalf("out = %+v", out)
	}
}

func TestCheckTask_UnavailableAwardsNothing(t *testing.T) {
	h := newHarness(t)
	h.passTheory(t)
	h.svc.StartTask(context.Background(), ModeLevels, "demo-t3")
	h.ch.err = check.ErrUnavailable
	if _, err := h.svc.CheckTask(context.Background(), "demo-t3"); !errors.Is(err, check.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
	st := h.store.Get()
	if st.Coins != 0 || st.SolvedCount["demo-t3"] != 0 {
		t.Fatalf("state = %+v", st)
	}
	h.ch.err = nil
	if _, err := h.svc.CheckTask(context.Background(), "demo-t3"); err != nil {
		t.Fatalf("attempt should survive an unavailable cluster: %v", err)
	}
}

func TestCheckTask_SnapshotTaskRestores(t *testing.T) {
	h := newHarness(t)
	h.passTheory(t)
	h.solve(t, ModeLevels, "demo-t7")
	if h.sn.restores != 1 {
		t.Fatalf("restores = %d", h.sn.restores)
	}
}

func TestGiveUp_SnapshotTaskRestores(t *testing.T) {
	h := newHarness(t)
	h.passTheory(t)
	h.svc.StartTask(context.Background(), ModeLevels, "demo-t7")
	if err := h.svc.GiveUp(context.Background(), "demo-t7"); err != nil {
		t.Fatal(err)
	}
	if h.sn.restores != 1 {
		t.Fatalf("restores = %d", h.sn.restores)
	}
	if _, err := h.svc.CheckTask(context.Background(), "demo-t7"); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("err = %v", err)
	}
}

func TestBusy_RejectsStartAndCheck(t *testing.T) {
	h := newHarness(t)
	h.passTheory(t)
	h.sn.block = make(chan struct{})
	h.sn.started = make(chan struct{})
	h.svc.StartTask(context.Background(), ModeLevels, "demo-t7")
	h.svc.StartTask(context.Background(), ModeLevels, "demo-t1")
	done := make(chan struct{})
	go func() {
		h.svc.CheckTask(context.Background(), "demo-t7")
		close(done)
	}()
	<-h.sn.started
	if err := h.svc.StartTask(context.Background(), ModeLevels, "demo-t2"); !errors.Is(err, ErrClusterBusy) {
		t.Errorf("start: err = %v", err)
	}
	if _, err := h.svc.CheckTask(context.Background(), "demo-t1"); !errors.Is(err, ErrClusterBusy) {
		t.Errorf("check: err = %v", err)
	}
	if !h.svc.Busy() {
		t.Error("Busy() should be true during restore")
	}
	close(h.sn.block)
	<-done
	if h.svc.Busy() {
		t.Error("Busy() should be false after restore")
	}
}

func TestNextMixed_ExcludesSnapshotByDefault(t *testing.T) {
	h := newHarness(t)
	for range 200 {
		item, err := h.svc.NextMixed(true, false)
		if err != nil {
			t.Fatal(err)
		}
		if item.TaskID == "demo-t7" {
			t.Fatal("snapshot task must be excluded")
		}
	}
	seen := false
	for range 500 {
		item, _ := h.svc.NextMixed(true, true)
		seen = seen || item.TaskID == "demo-t7"
	}
	if !seen {
		t.Fatal("snapshot task never offered when included")
	}
}

func TestNextMixed_PracticeOnlyNoQuestions(t *testing.T) {
	h := newHarness(t)
	sawQuestion := false
	for range 200 {
		item, _ := h.svc.NextMixed(true, true)
		if item.Kind != "task" {
			t.Fatalf("practice-only gave %q", item.Kind)
		}
		mixed, _ := h.svc.NextMixed(false, false)
		sawQuestion = sawQuestion || mixed.Kind == "question"
	}
	if !sawQuestion {
		t.Fatal("mixed mode never offered a question")
	}
}

func TestAnswerMixed(t *testing.T) {
	h := newHarness(t)
	var item MixedItem
	for item.Kind != "question" {
		item, _ = h.svc.NextMixed(false, false)
	}
	res, err := h.svc.AnswerMixed(item.Question.ID, rightOption(*item.Question))
	if err != nil || !res.Correct {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	if h.store.Get().Coins != 0 || len(h.store.Get().TheoryPassed) != 0 {
		t.Fatal("mixed answers must not change progress")
	}
	if _, err := h.svc.AnswerMixed("never-offered", 0); !errors.Is(err, ErrUnknownQuestion) {
		t.Fatalf("err = %v", err)
	}
}

func TestBuy_DeductsAndSetsBreak(t *testing.T) {
	h := newHarness(t)
	h.store.Update(func(s *progress.State) error { s.Coins = 40; return nil })
	until, err := h.svc.Buy("break-5")
	if err != nil {
		t.Fatal(err)
	}
	if !until.Equal(testNow.Add(5*time.Minute)) || h.store.Get().Coins != 10 {
		t.Fatalf("until = %v coins = %d", until, h.store.Get().Coins)
	}
}

func TestBuy_Insufficient(t *testing.T) {
	h := newHarness(t)
	h.store.Update(func(s *progress.State) error { s.Coins = 29; return nil })
	if _, err := h.svc.Buy("break-5"); !errors.Is(err, ErrInsufficientCoins) {
		t.Fatalf("err = %v", err)
	}
	if h.store.Get().Coins != 29 {
		t.Fatal("coins changed")
	}
}

func TestBuy_BreakActive(t *testing.T) {
	h := newHarness(t)
	h.store.Update(func(s *progress.State) error {
		s.Coins = 100
		until := testNow.Add(time.Minute)
		s.BreakUntil = &until
		return nil
	})
	if _, err := h.svc.Buy("break-5"); !errors.Is(err, ErrBreakActive) {
		t.Fatalf("err = %v", err)
	}
}

func TestBuy_UnknownItem(t *testing.T) {
	h := newHarness(t)
	if _, err := h.svc.Buy("pizza"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}
