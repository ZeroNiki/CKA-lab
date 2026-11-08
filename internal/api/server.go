// Package api is the JSON HTTP interface the browser UI talks to.
package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"cka-lab/internal/check"
	"cka-lab/internal/game"
)

type server struct{ svc *game.Service }

func New(svc *game.Service, static fs.FS) http.Handler {
	s := &server{svc: svc}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/levels", s.levels)
	mux.HandleFunc("GET /api/state", s.state)
	mux.HandleFunc("GET /api/shop", s.shop)
	mux.HandleFunc("POST /api/shop/{id}/buy", s.buy)
	mux.HandleFunc("POST /api/levels/{id}/theory", s.startTheory)
	mux.HandleFunc("POST /api/quiz/{quizId}/answer", s.answer)
	mux.HandleFunc("GET /api/tasks/{id}", s.task)
	mux.HandleFunc("POST /api/tasks/{id}/start", s.start)
	mux.HandleFunc("POST /api/tasks/{id}/check", s.check)
	mux.HandleFunc("POST /api/tasks/{id}/reveal", s.reveal)
	mux.HandleFunc("POST /api/tasks/{id}/giveup", s.giveUp)
	mux.HandleFunc("GET /api/mixed/next", s.mixedNext)
	mux.HandleFunc("POST /api/mixed/answer", s.mixedAnswer)
	mux.Handle("/", http.FileServerFS(static))
	return mux
}

type taskSummary struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	Difficulty       string `json:"difficulty"`
	RequiresSnapshot bool   `json:"requiresSnapshot"`
	Solved           bool   `json:"solved"`
}

type levelView struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	Domain       string        `json:"domain"`
	TheoryPassed bool          `json:"theoryPassed"`
	Solved       int           `json:"solved"`
	Total        int           `json:"total"`
	Tasks        []taskSummary `json:"tasks"`
}

func (s *server) levels(w http.ResponseWriter, _ *http.Request) {
	st := s.svc.State()
	out := []levelView{}
	for _, l := range s.svc.Content().Levels {
		v := levelView{ID: l.ID, Title: l.Title, Domain: l.Domain, TheoryPassed: st.TheoryPassed[l.ID], Total: len(l.Tasks)}
		for _, t := range l.Tasks {
			solved := st.LevelSolved[t.ID]
			if solved {
				v.Solved++
			}
			v.Tasks = append(v.Tasks, taskSummary{t.ID, t.Title, t.Difficulty, t.RequiresSnapshot, solved})
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) state(w http.ResponseWriter, _ *http.Request) {
	st := s.svc.State()
	writeJSON(w, http.StatusOK, map[string]any{"coins": st.Coins, "breakUntil": st.BreakUntil, "busy": s.svc.Busy()})
}

func (s *server) shop(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.Config().Shop)
}

func (s *server) buy(w http.ResponseWriter, r *http.Request) {
	until, err := s.svc.Buy(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]time.Time{"breakUntil": until})
}

func (s *server) startTheory(w http.ResponseWriter, r *http.Request) {
	qz, err := s.svc.StartTheory(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, qz)
}

type answerReq struct {
	QuestionID string `json:"questionId"`
	Option     int    `json:"option"`
}

func (s *server) answer(w http.ResponseWriter, r *http.Request) {
	var req answerReq
	if !readJSON(w, r, &req) {
		return
	}
	res, err := s.svc.AnswerTheory(r.PathValue("quizId"), req.QuestionID, req.Option)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *server) task(w http.ResponseWriter, r *http.Request) {
	t, ok := s.svc.Content().TaskByID[r.PathValue("id")]
	if !ok {
		writeError(w, game.ErrNotFound)
		return
	}
	out := map[string]any{
		"id": t.ID, "title": t.Title, "difficulty": t.Difficulty, "ns": t.NS,
		"statement": t.Statement, "requiresSnapshot": t.RequiresSnapshot, "levelId": t.LevelID,
	}
	mode, _ := s.svc.ActiveMode(t.ID)
	out["active"] = string(mode) // "" when no attempt is started
	st := s.svc.State()
	// Docs and the solution are part of the reward: hidden until solved or explicitly revealed.
	if st.SolvedCount[t.ID] > 0 || st.Revealed[t.ID] {
		out["docs"], out["solution"] = t.Docs, t.Solution
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) start(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode game.Mode `json:"mode"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.svc.StartTask(r.Context(), req.Mode, r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) check(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.CheckTask(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) reveal(w http.ResponseWriter, r *http.Request) {
	docs, sol, err := s.svc.Reveal(r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"docs": docs, "solution": sol})
}

func (s *server) giveUp(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.GiveUp(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) mixedNext(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	item, err := s.svc.NextMixed(q.Get("practice") == "1", q.Get("snapshot") == "1")
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *server) mixedAnswer(w http.ResponseWriter, r *http.Request) {
	var req answerReq
	if !readJSON(w, r, &req) {
		return
	}
	res, err := s.svc.AnswerMixed(req.QuestionID, req.Option)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body: " + err.Error()})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write response: %v", err)
	}
}

func writeError(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	msg := err.Error()
	switch {
	case errors.Is(err, game.ErrNotFound):
		code = http.StatusNotFound
	case errors.Is(err, game.ErrTheoryRequired), errors.Is(err, game.ErrNotStarted), errors.Is(err, game.ErrBadMode),
		errors.Is(err, game.ErrQuizDone), errors.Is(err, game.ErrUnknownQuestion),
		errors.Is(err, game.ErrAlreadyAnswered), errors.Is(err, game.ErrOptionOutOfRange):
		code = http.StatusBadRequest
	case errors.Is(err, game.ErrClusterBusy), errors.Is(err, game.ErrBreakActive), errors.Is(err, game.ErrNoSnapshot):
		code = http.StatusConflict
	case errors.Is(err, game.ErrInsufficientCoins):
		code = http.StatusPaymentRequired
	case errors.Is(err, check.ErrUnavailable):
		code = http.StatusServiceUnavailable
		msg = "cluster unavailable: is the lab running? (./lab up) — " +
			strings.TrimPrefix(err.Error(), check.ErrUnavailable.Error()+": ")
	default:
		log.Printf("internal error: %v", err)
	}
	writeJSON(w, code, map[string]string{"error": msg})
}
