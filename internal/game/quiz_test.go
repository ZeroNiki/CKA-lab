package game

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"testing"

	"cka-lab/internal/content"
)

func questions(n int) []content.Question {
	qs := make([]content.Question, n)
	for i := range qs {
		qs[i] = content.Question{
			ID:          fmt.Sprintf("q%d", i),
			Question:    fmt.Sprintf("Question %d?", i),
			Options:     []string{"wrong-a", "right", "wrong-b", "wrong-c"},
			Answer:      1,
			Explanation: fmt.Sprintf("explain %d", i),
		}
	}
	return qs
}

func explain(id string) string { return "explain for " + id }

func rightOption(q QuizQuestion) int {
	for i, o := range q.Options {
		if o == "right" {
			return i
		}
	}
	return -1
}

func wrongOption(q QuizQuestion) int {
	for i, o := range q.Options {
		if o != "right" {
			return i
		}
	}
	return -1
}

func TestNewQuiz_ShufflesButKeepsAnswer(t *testing.T) {
	qz := NewQuiz("lvl", questions(10), rand.New(rand.NewPCG(1, 2)))
	if len(qz.Questions) != 10 || qz.LevelID != "lvl" || qz.ID == "" {
		t.Fatalf("quiz = %+v", qz)
	}
	sameOrder, sameOptions := true, true
	for i, q := range qz.Questions {
		if q.ID != fmt.Sprintf("q%d", i) {
			sameOrder = false
		}
		if q.Options[1] != "right" {
			sameOptions = false
		}
	}
	if sameOrder || sameOptions {
		t.Fatalf("quiz is not shuffled: order=%v options=%v", sameOrder, sameOptions)
	}
	q := qz.Questions[0]
	res, err := qz.Answer(q.ID, rightOption(q), explain)
	if err != nil || !res.Correct || res.CorrectOption != rightOption(q) {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	if res.Explanation != "explain for "+q.ID {
		t.Fatalf("explanation = %q", res.Explanation)
	}
}

func TestQuiz_OneMistakePasses(t *testing.T) {
	qz := NewQuiz("lvl", questions(10), rand.New(rand.NewPCG(3, 4)))
	var res AnswerResult
	for i, q := range qz.Questions {
		opt := rightOption(q)
		if i == 4 {
			opt = wrongOption(q)
		}
		var err error
		res, err = qz.Answer(q.ID, opt, explain)
		if err != nil {
			t.Fatal(err)
		}
		if i < 9 && res.Done {
			t.Fatalf("done too early at %d", i)
		}
	}
	if !res.Done || !res.Passed || res.Wrong != 1 {
		t.Fatalf("final = %+v", res)
	}
}

func TestQuiz_SecondMistakeFailsImmediately(t *testing.T) {
	qz := NewQuiz("lvl", questions(10), rand.New(rand.NewPCG(5, 6)))
	q0, q1, q2 := qz.Questions[0], qz.Questions[1], qz.Questions[2]
	if res, _ := qz.Answer(q0.ID, wrongOption(q0), explain); res.Done {
		t.Fatal("one mistake must not end the quiz")
	}
	res, err := qz.Answer(q1.ID, wrongOption(q1), explain)
	if err != nil || !res.Done || res.Passed || res.Wrong != 2 {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	if _, err := qz.Answer(q2.ID, rightOption(q2), explain); !errors.Is(err, ErrQuizDone) {
		t.Fatalf("err = %v, want ErrQuizDone", err)
	}
}

func TestQuiz_UnknownAndDuplicate(t *testing.T) {
	qz := NewQuiz("lvl", questions(10), rand.New(rand.NewPCG(7, 8)))
	if _, err := qz.Answer("nope", 0, explain); !errors.Is(err, ErrUnknownQuestion) {
		t.Fatalf("err = %v", err)
	}
	q := qz.Questions[0]
	qz.Answer(q.ID, rightOption(q), explain)
	if _, err := qz.Answer(q.ID, rightOption(q), explain); !errors.Is(err, ErrAlreadyAnswered) {
		t.Fatalf("err = %v", err)
	}
	if _, err := qz.Answer(qz.Questions[1].ID, 99, explain); err == nil {
		t.Fatal("option out of range must be an error")
	}
}
