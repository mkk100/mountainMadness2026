package httpapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	nethttp "net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

const slugMaxAttempts = 8

type Server struct {
	db *sql.DB
}

func New(db *sql.DB) nethttp.Handler {
	s := &Server{db: db}
	r := chi.NewRouter()

	r.Get("/health", s.handleHealth)
	r.Post("/api/decisions", s.handleCreateDecision)
	r.Get("/api/decisions/{slug}", s.handleGetDecision)
	r.Post("/api/decisions/{slug}/responses", s.handleCreateResponse)
	r.Post("/api/responses/{responseID}/votes", s.handleCreateVote)

	return r
}

func (s *Server) handleHealth(w nethttp.ResponseWriter, _ *nethttp.Request) {
	writeJSON(w, nethttp.StatusOK, map[string]bool{"ok": true})
}

type createDecisionRequest struct {
	Title       string     `json:"title"`
	Description *string    `json:"description"`
	ClosesAt    *time.Time `json:"closes_at"`
}

type createDecisionResponse struct {
	ID       string `json:"id"`
	Slug     string `json:"slug"`
	ShareURL string `json:"share_url"`
}

func (s *Server) handleCreateDecision(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req createDecisionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, nethttp.StatusBadRequest, err.Error())
		return
	}

	title := strings.TrimSpace(req.Title)
	if length := utf8.RuneCountInString(title); length < 4 || length > 100 {
		writeError(w, nethttp.StatusBadRequest, "title must be between 4 and 100 characters")
		return
	}

	decisionID := uuid.New()
	baseSlug := slugify(title)
	if baseSlug == "" {
		baseSlug = "decision"
	}

	ctx := r.Context()
	var slug string
	for i := 0; i < slugMaxAttempts; i++ {
		slug = fmt.Sprintf("%s-%s", baseSlug, randSuffix(5))
		_, err := s.db.ExecContext(
			ctx,
			`INSERT INTO decisions (id, slug, title, description, closes_at) VALUES ($1, $2, $3, $4, $5)`,
			decisionID,
			slug,
			title,
			req.Description,
			req.ClosesAt,
		)
		if err == nil {
			writeJSON(w, nethttp.StatusCreated, createDecisionResponse{
				ID:       decisionID.String(),
				Slug:     slug,
				ShareURL: "/d/" + slug,
			})
			return
		}

		if isUniqueViolation(err) {
			continue
		}

		writeError(w, nethttp.StatusInternalServerError, "failed to create decision")
		return
	}

	writeError(w, nethttp.StatusConflict, "failed to generate a unique slug")
}

type decisionResponsePayload struct {
	ViewerID string  `json:"viewer_id"`
	Rating   int     `json:"rating"`
	Emoji    string  `json:"emoji"`
	Comment  *string `json:"comment"`
}

func (s *Server) handleCreateResponse(w nethttp.ResponseWriter, r *nethttp.Request) {
	slug := chi.URLParam(r, "slug")
	if strings.TrimSpace(slug) == "" {
		writeError(w, nethttp.StatusBadRequest, "slug is required")
		return
	}

	var req decisionResponsePayload
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, nethttp.StatusBadRequest, err.Error())
		return
	}

	viewerID, err := uuid.Parse(req.ViewerID)
	if err != nil {
		writeError(w, nethttp.StatusBadRequest, "viewer_id must be a valid UUID")
		return
	}
	if req.Rating < 1 || req.Rating > 5 {
		writeError(w, nethttp.StatusBadRequest, "rating must be between 1 and 5")
		return
	}
	if strings.TrimSpace(req.Emoji) == "" {
		writeError(w, nethttp.StatusBadRequest, "emoji is required")
		return
	}

	ctx := r.Context()
	decisionID, err := s.findDecisionIDBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, nethttp.StatusNotFound, "decision not found")
			return
		}
		writeError(w, nethttp.StatusInternalServerError, "failed to load decision")
		return
	}

	responseID := uuid.New()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO responses (id, decision_id, viewer_id, rating, emoji, comment)
		VALUES ($1, $2, $3, $4, $5, $6)
	`,
		responseID,
		decisionID,
		viewerID,
		req.Rating,
		strings.TrimSpace(req.Emoji),
		req.Comment,
	)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, nethttp.StatusConflict, "viewer already submitted a response for this decision")
			return
		}
		writeError(w, nethttp.StatusInternalServerError, "failed to create response")
		return
	}

	writeJSON(w, nethttp.StatusCreated, map[string]string{"id": responseID.String()})
}

type voteRequest struct {
	VoterViewerID string `json:"voter_viewer_id"`
	Value         int    `json:"value"`
}

func (s *Server) handleCreateVote(w nethttp.ResponseWriter, r *nethttp.Request) {
	responseID, err := uuid.Parse(chi.URLParam(r, "responseID"))
	if err != nil {
		writeError(w, nethttp.StatusBadRequest, "responseID must be a valid UUID")
		return
	}

	var req voteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, nethttp.StatusBadRequest, err.Error())
		return
	}

	voterID, err := uuid.Parse(req.VoterViewerID)
	if err != nil {
		writeError(w, nethttp.StatusBadRequest, "voter_viewer_id must be a valid UUID")
		return
	}
	if req.Value != -1 && req.Value != 1 {
		writeError(w, nethttp.StatusBadRequest, "value must be -1 or 1")
		return
	}

	voteID := uuid.New()
	_, err = s.db.ExecContext(r.Context(), `
		INSERT INTO votes (id, response_id, voter_viewer_id, value)
		VALUES ($1, $2, $3, $4)
	`, voteID, responseID, voterID, req.Value)
	if err != nil {
		if isForeignKeyViolation(err) {
			writeError(w, nethttp.StatusNotFound, "response not found")
			return
		}
		if isUniqueViolation(err) {
			writeError(w, nethttp.StatusConflict, "viewer already voted on this response")
			return
		}
		writeError(w, nethttp.StatusInternalServerError, "failed to create vote")
		return
	}

	writeJSON(w, nethttp.StatusCreated, map[string]string{"id": voteID.String()})
}

type decisionDetails struct {
	ID          string             `json:"id"`
	Slug        string             `json:"slug"`
	Title       string             `json:"title"`
	Description *string            `json:"description"`
	ClosesAt    *time.Time         `json:"closes_at"`
	CreatedAt   time.Time          `json:"created_at"`
	Responses   []decisionResponse `json:"responses"`
}

type decisionResponse struct {
	ID        string    `json:"id"`
	ViewerID  string    `json:"viewer_id"`
	Rating    int       `json:"rating"`
	Emoji     string    `json:"emoji"`
	Comment   *string   `json:"comment"`
	Score     int       `json:"score"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Server) handleGetDecision(w nethttp.ResponseWriter, r *nethttp.Request) {
	slug := chi.URLParam(r, "slug")
	if strings.TrimSpace(slug) == "" {
		writeError(w, nethttp.StatusBadRequest, "slug is required")
		return
	}

	ctx := r.Context()

	var d decisionDetails
	err := s.db.QueryRowContext(ctx, `
		SELECT id, slug, title, description, closes_at, created_at
		FROM decisions
		WHERE slug = $1
	`, slug).Scan(&d.ID, &d.Slug, &d.Title, &d.Description, &d.ClosesAt, &d.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, nethttp.StatusNotFound, "decision not found")
			return
		}
		writeError(w, nethttp.StatusInternalServerError, "failed to load decision")
		return
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			r.id,
			r.viewer_id,
			r.rating,
			r.emoji,
			r.comment,
			COALESCE(SUM(v.value), 0) AS score,
			r.created_at
		FROM responses r
		LEFT JOIN votes v ON v.response_id = r.id
		WHERE r.decision_id = $1
		GROUP BY r.id
		ORDER BY r.created_at DESC
	`, d.ID)
	if err != nil {
		writeError(w, nethttp.StatusInternalServerError, "failed to load responses")
		return
	}
	defer rows.Close()

	for rows.Next() {
		var item decisionResponse
		var viewerID uuid.UUID
		if err := rows.Scan(&item.ID, &viewerID, &item.Rating, &item.Emoji, &item.Comment, &item.Score, &item.CreatedAt); err != nil {
			writeError(w, nethttp.StatusInternalServerError, "failed to parse responses")
			return
		}
		item.ViewerID = viewerID.String()
		d.Responses = append(d.Responses, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, nethttp.StatusInternalServerError, "failed to read responses")
		return
	}

	writeJSON(w, nethttp.StatusOK, d)
}

func (s *Server) findDecisionIDBySlug(ctx context.Context, slug string) (uuid.UUID, error) {
	var decisionID uuid.UUID
	err := s.db.QueryRowContext(ctx, "SELECT id FROM decisions WHERE slug = $1", slug).Scan(&decisionID)
	return decisionID, err
}

func decodeJSON(r *nethttp.Request, out any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("invalid JSON: request body must contain a single JSON object")
	}
	return nil
}

func writeJSON(w nethttp.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w nethttp.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

func slugify(input string) string {
	var b strings.Builder
	b.Grow(len(input))

	lastHyphen := false
	for _, r := range strings.ToLower(strings.TrimSpace(input)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastHyphen = false
			continue
		}
		if unicode.IsSpace(r) || r == '-' || r == '_' {
			if !lastHyphen && b.Len() > 0 {
				b.WriteRune('-')
				lastHyphen = true
			}
		}
	}

	return strings.Trim(b.String(), "-")
}

func randSuffix(length int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	if length <= 0 {
		return ""
	}

	var b strings.Builder
	b.Grow(length)
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			b.WriteByte(letters[0])
			continue
		}
		b.WriteByte(letters[n.Int64()])
	}
	return b.String()
}
