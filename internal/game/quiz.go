package game

import (
	"errors"
	"fmt"
	"math/rand/v2"

	"cka-lab/internal/content"
)

// MaxTheoryMistakes is how many wrong answers still pass the theory gate.
const MaxTheoryMistakes = 1

var (
	ErrQuizDone         = errors.New("quiz is finished; start a new one")
	ErrUnknownQuestion  = errors.New("question is not part of this quiz")
	ErrAlreadyAnswered  = errors.New("question already answered")
	ErrOptionOutOfRange = errors.New("option out of range")
)

// Quiz is one attempt at a level's theory block, with questions and options shuffled.
type Quiz struct {
	ID        string          `json:"quizId"`
	LevelID   string          `json:"levelId"`
	Questions []QuizQuestion  `json:"questions"`
	Wrong     int             `json:"-"`
	Answered  map[string]bool `json:"-"`
	Done      bool            `json:"-"`
	Passed    bool            `json:"-"`
}

// QuizQuestion is what the browser sees; the correct option stays on the server.
type QuizQuestion struct {
	ID       string   `json:"id"`
	Question string   `json:"question"`
	Options  []string `json:"options"`
	answer   int
}

type AnswerResult struct {
	Correct       bool   `json:"correct"`
	CorrectOption int    `json:"correctOption"`
	Explanation   string `json:"explanation"`
	Wrong         int    `json:"wrong"`
	Done          bool   `json:"done"`
	Passed        bool   `json:"passed"`
}

func NewQuiz(levelID string, qs []content.Question, rng *rand.Rand) *Quiz {
	out := make([]QuizQuestion, len(qs))
	for i, idx := range rng.Perm(len(qs)) {
		out[i] = shuffleQuestion(qs[idx], rng)
	}
	return &Quiz{
		ID:        fmt.Sprintf("%s-%016x", levelID, rng.Uint64()),
		LevelID:   levelID,
		Questions: out,
		Answered:  map[string]bool{},
	}
}

func shuffleQuestion(q content.Question, rng *rand.Rand) QuizQuestion {
	qq := QuizQuestion{ID: q.ID, Question: q.Question, Options: make([]string, len(q.Options))}
	for newPos, oldPos := range rng.Perm(len(q.Options)) {
		qq.Options[newPos] = q.Options[oldPos]
		if oldPos == q.Answer {
			qq.answer = newPos
		}
	}
	return qq
}

// Answer grades one question. The second mistake ends the quiz at once: it can no longer pass.
func (q *Quiz) Answer(questionID string, option int, explain func(id string) string) (AnswerResult, error) {
	if q.Done {
		return AnswerResult{}, ErrQuizDone
	}
	var qq *QuizQuestion
	for i := range q.Questions {
		if q.Questions[i].ID == questionID {
			qq = &q.Questions[i]
		}
	}
	if qq == nil {
		return AnswerResult{}, ErrUnknownQuestion
	}
	if q.Answered[questionID] {
		return AnswerResult{}, ErrAlreadyAnswered
	}
	if option < 0 || option >= len(qq.Options) {
		return AnswerResult{}, ErrOptionOutOfRange
	}
	q.Answered[questionID] = true
	correct := option == qq.answer
	if !correct {
		q.Wrong++
	}
	switch {
	case q.Wrong > MaxTheoryMistakes:
		q.Done = true
	case len(q.Answered) == len(q.Questions):
		q.Done, q.Passed = true, true
	}
	return AnswerResult{
		Correct:       correct,
		CorrectOption: qq.answer,
		Explanation:   explain(questionID),
		Wrong:         q.Wrong,
		Done:          q.Done,
		Passed:        q.Passed,
	}, nil
}
