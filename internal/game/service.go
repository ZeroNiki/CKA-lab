package game

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"cka-lab/internal/check"
	"cka-lab/internal/cluster"
	"cka-lab/internal/content"
	"cka-lab/internal/progress"
)

type Mode string

const (
	ModeLevels   Mode = "levels"
	ModeMixed    Mode = "mixed"
	ModePractice Mode = "practice"
)

func (m Mode) Valid() bool { return m == ModeLevels || m == ModeMixed || m == ModePractice }

// Checker runs a task's checks against the cluster.
type Checker interface {
	Check(ctx context.Context, t *content.Task) ([]check.Result, error)
}

// Preparer gives a task its fresh namespace and setup.
type Preparer interface {
	Prepare(ctx context.Context, t *content.Task) error
}

var (
	ErrNotFound          = errors.New("not found")
	ErrTheoryRequired    = errors.New("pass the theory of this level first")
	ErrNotStarted        = errors.New("task is not started; press Start first")
	ErrClusterBusy       = errors.New("cluster is being restored from snapshot, wait a moment")
	ErrInsufficientCoins = errors.New("not enough coins")
	ErrBreakActive       = errors.New("a break is already running")
	ErrBadMode           = errors.New("mode must be levels, mixed or practice")
)

type CheckOutcome struct {
	Results        []check.Result `json:"results"`
	Pass           bool           `json:"pass"`
	Coins          int            `json:"coins"`
	LevelCompleted bool           `json:"levelCompleted"`
	Bonus          bool           `json:"bonus"`
	RestoreError   string         `json:"restoreError,omitempty"`
}

type MixedItem struct {
	Kind     string        `json:"kind"` // "question" | "task"
	Question *QuizQuestion `json:"question,omitempty"`
	TaskID   string        `json:"taskId,omitempty"`
}

type Service struct {
	content *content.Content
	cfg     Config
	store   *progress.Store
	checker Checker
	prep    Preparer
	snap    cluster.Snapshotter
	now     func() time.Time

	mu        sync.Mutex
	rng       *rand.Rand
	busy      bool
	quizzes   map[string]*Quiz
	active    map[string]Mode // taskID -> mode of the running attempt
	mixedPerm map[string]QuizQuestion
}

func NewService(c *content.Content, cfg Config, st *progress.Store, ch Checker, pr Preparer,
	sn cluster.Snapshotter, rng *rand.Rand, now func() time.Time) *Service {
	return &Service{
		content: c, cfg: cfg, store: st, checker: ch, prep: pr, snap: sn, rng: rng, now: now,
		quizzes: map[string]*Quiz{}, active: map[string]Mode{}, mixedPerm: map[string]QuizQuestion{},
	}
}

func (s *Service) Content() *content.Content { return s.content }
func (s *Service) Config() Config            { return s.cfg }
func (s *Service) State() progress.State     { return s.store.Get() }

// Busy reports whether a snapshot restore is running.
func (s *Service) Busy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.busy
}

func (s *Service) level(id string) (*content.Level, error) {
	for i := range s.content.Levels {
		if s.content.Levels[i].ID == id {
			return &s.content.Levels[i], nil
		}
	}
	return nil, fmt.Errorf("level %q: %w", id, ErrNotFound)
}

func (s *Service) task(id string) (*content.Task, error) {
	t, ok := s.content.TaskByID[id]
	if !ok {
		return nil, fmt.Errorf("task %q: %w", id, ErrNotFound)
	}
	return t, nil
}

func (s *Service) explain(id string) string {
	if q, ok := s.content.QuestionByID[id]; ok {
		return q.Explanation
	}
	return ""
}

func (s *Service) StartTheory(levelID string) (*Quiz, error) {
	l, err := s.level(levelID)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	qz := NewQuiz(l.ID, l.Questions, s.rng)
	s.quizzes[qz.ID] = qz
	return qz, nil
}

func (s *Service) AnswerTheory(quizID, questionID string, option int) (AnswerResult, error) {
	s.mu.Lock()
	qz, ok := s.quizzes[quizID]
	if !ok {
		s.mu.Unlock()
		return AnswerResult{}, fmt.Errorf("quiz %q: %w", quizID, ErrNotFound)
	}
	res, err := qz.Answer(questionID, option, s.explain)
	if err == nil && res.Done {
		delete(s.quizzes, quizID)
	}
	s.mu.Unlock()
	if err != nil {
		return res, err
	}
	if res.Passed {
		err = s.store.Update(func(st *progress.State) error {
			st.TheoryPassed[qz.LevelID] = true
			return nil
		})
	}
	return res, err
}

func (s *Service) StartTask(ctx context.Context, mode Mode, taskID string) error {
	if !mode.Valid() {
		return ErrBadMode
	}
	t, err := s.task(taskID)
	if err != nil {
		return err
	}
	if mode == ModeLevels && !s.store.Get().TheoryPassed[t.LevelID] {
		return ErrTheoryRequired
	}
	if s.Busy() {
		return ErrClusterBusy
	}
	if err := s.prep.Prepare(ctx, t); err != nil {
		return err
	}
	s.mu.Lock()
	s.active[taskID] = mode
	s.mu.Unlock()
	return nil
}

func (s *Service) CheckTask(ctx context.Context, taskID string) (CheckOutcome, error) {
	t, err := s.task(taskID)
	if err != nil {
		return CheckOutcome{}, err
	}
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return CheckOutcome{}, ErrClusterBusy
	}
	mode, ok := s.active[taskID]
	// Take the attempt while checking so a double click cannot award twice.
	delete(s.active, taskID)
	s.mu.Unlock()
	if !ok {
		return CheckOutcome{}, ErrNotStarted
	}

	results, err := s.checker.Check(ctx, t)
	pass := err == nil && allPass(results)
	if !pass {
		s.mu.Lock()
		s.active[taskID] = mode
		s.mu.Unlock()
		if err != nil {
			return CheckOutcome{}, err
		}
		return CheckOutcome{Results: results}, nil
	}

	out := CheckOutcome{Results: results, Pass: true}
	if err := s.store.Update(func(st *progress.State) error {
		out.Coins = TaskReward(s.cfg, t.Difficulty, st.SolvedCount[t.ID] == 0, st.Revealed[t.ID])
		st.Coins += out.Coins
		st.SolvedCount[t.ID]++
		delete(st.Revealed, t.ID) // a revealed solution costs only the next solve
		if mode == ModeLevels {
			st.LevelSolved[t.ID] = true
			out.LevelCompleted = s.levelComplete(st, t.LevelID)
			if out.LevelCompleted && !st.LevelBonusPaid[t.LevelID] {
				st.Coins += s.cfg.Coins.LevelBonus
				st.LevelBonusPaid[t.LevelID] = true
				out.Bonus = true
			}
		}
		return nil
	}); err != nil {
		return CheckOutcome{}, err
	}
	if t.RequiresSnapshot {
		if err := s.restore(ctx); err != nil {
			out.RestoreError = err.Error()
		}
	}
	return out, nil
}

func (s *Service) levelComplete(st *progress.State, levelID string) bool {
	l, err := s.level(levelID)
	if err != nil {
		return false
	}
	for _, t := range l.Tasks {
		if !st.LevelSolved[t.ID] {
			return false
		}
	}
	return true
}

func allPass(rs []check.Result) bool {
	for _, r := range rs {
		if !r.Pass {
			return false
		}
	}
	return len(rs) > 0
}

func (s *Service) restore(ctx context.Context) error {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return ErrClusterBusy
	}
	s.busy = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.busy = false
		s.mu.Unlock()
	}()
	return s.snap.Restore(ctx)
}

// Reveal returns the reference solution. Before the first solve it marks the task revealed (no coins next time).
func (s *Service) Reveal(taskID string) (docs, solution string, err error) {
	t, err := s.task(taskID)
	if err != nil {
		return "", "", err
	}
	err = s.store.Update(func(st *progress.State) error {
		if st.SolvedCount[t.ID] == 0 {
			st.Revealed[t.ID] = true
		}
		return nil
	})
	return t.Docs, t.Solution, err
}

func (s *Service) GiveUp(ctx context.Context, taskID string) error {
	t, err := s.task(taskID)
	if err != nil {
		return err
	}
	if s.Busy() {
		return ErrClusterBusy
	}
	s.mu.Lock()
	delete(s.active, taskID)
	s.mu.Unlock()
	if t.RequiresSnapshot {
		return s.restore(ctx)
	}
	return nil
}

func (s *Service) NextMixed(practiceOnly, includeSnapshot bool) (MixedItem, error) {
	var tasks []*content.Task
	for _, l := range s.content.Levels {
		for i := range l.Tasks {
			if includeSnapshot || !l.Tasks[i].RequiresSnapshot {
				tasks = append(tasks, s.content.TaskByID[l.Tasks[i].ID])
			}
		}
	}
	var qs []content.Question
	if !practiceOnly {
		for _, l := range s.content.Levels {
			qs = append(qs, l.Questions...)
		}
	}
	total := len(tasks) + len(qs)
	if total == 0 {
		return MixedItem{}, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.rng.IntN(total)
	if n < len(tasks) {
		return MixedItem{Kind: "task", TaskID: tasks[n].ID}, nil
	}
	qq := shuffleQuestion(qs[n-len(tasks)], s.rng)
	s.mixedPerm[qq.ID] = qq
	return MixedItem{Kind: "question", Question: &qq}, nil
}

// AnswerMixed grades a question offered by NextMixed. It never changes progress.
func (s *Service) AnswerMixed(questionID string, option int) (AnswerResult, error) {
	s.mu.Lock()
	qq, ok := s.mixedPerm[questionID]
	delete(s.mixedPerm, questionID)
	s.mu.Unlock()
	if !ok {
		return AnswerResult{}, ErrUnknownQuestion
	}
	if option < 0 || option >= len(qq.Options) {
		return AnswerResult{}, ErrOptionOutOfRange
	}
	return AnswerResult{
		Correct:       option == qq.answer,
		CorrectOption: qq.answer,
		Explanation:   s.explain(questionID),
	}, nil
}

func (s *Service) Buy(itemID string) (time.Time, error) {
	var item *ShopItem
	for i := range s.cfg.Shop {
		if s.cfg.Shop[i].ID == itemID {
			item = &s.cfg.Shop[i]
		}
	}
	if item == nil {
		return time.Time{}, fmt.Errorf("shop item %q: %w", itemID, ErrNotFound)
	}
	now := s.now()
	var until time.Time
	err := s.store.Update(func(st *progress.State) error {
		if st.BreakUntil != nil && st.BreakUntil.After(now) {
			return ErrBreakActive
		}
		if st.Coins < item.Price {
			return ErrInsufficientCoins
		}
		st.Coins -= item.Price
		until = now.Add(time.Duration(item.Minutes) * time.Minute)
		st.BreakUntil = &until
		return nil
	})
	return until, err
}
