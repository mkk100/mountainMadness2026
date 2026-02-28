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

const (
	slugMaxAttempts  = 8
	maxCommentLength = 180
)

var allowedEmojis = map[string]struct{}{
	"😭":  {},
	"😬":  {},
	"🧨":  {},
	"🤡":  {},
	"🫡":  {},
	"🧠":  {},
	"🔥":  {},
	"🥶":  {},
	"❤️": {},
	"🫠":  {},
}

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
	r.Post("/api/responses/{responseID}/vote", s.handleCreateVote)
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
	emoji := strings.TrimSpace(req.Emoji)
	if _, ok := allowedEmojis[emoji]; !ok {
		writeError(w, nethttp.StatusBadRequest, "emoji is invalid")
		return
	}

	ctx := r.Context()
	decision, err := s.findDecisionBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, nethttp.StatusNotFound, "decision not found")
			return
		}
		writeError(w, nethttp.StatusInternalServerError, "failed to load decision")
		return
	}

	if decision.ClosesAt != nil && time.Now().After(decision.ClosesAt.UTC()) {
		writeError(w, nethttp.StatusConflict, "decision is closed")
		return
	}

	comment, err := normalizeComment(req.Comment)
	if err != nil {
		writeError(w, nethttp.StatusBadRequest, err.Error())
		return
	}

	responseID := uuid.New()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO responses (id, decision_id, viewer_id, rating, emoji, comment)
		VALUES ($1, $2, $3, $4, $5, $6)
	`,
		responseID,
		decision.ID,
		viewerID,
		req.Rating,
		emoji,
		comment,
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
	ViewerID string `json:"viewer_id"`
	Value    int    `json:"value"`
}

type voteSummaryResponse struct {
	ResponseID string `json:"response_id"`
	Score      int    `json:"score"`
	Upvotes    int    `json:"upvotes"`
	Downvotes  int    `json:"downvotes"`
	MyVote     int    `json:"my_vote"`
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

	viewerID, err := uuid.Parse(req.ViewerID)
	if err != nil {
		writeError(w, nethttp.StatusBadRequest, "viewer_id must be a valid UUID")
		return
	}
	if req.Value != -1 && req.Value != 1 {
		writeError(w, nethttp.StatusBadRequest, "value must be -1 or 1")
		return
	}

	summary, status, err := s.toggleVote(r.Context(), responseID, viewerID, req.Value)
	if err != nil {
		writeError(w, status, err.Error())
		return
	}

	writeJSON(w, nethttp.StatusOK, summary)
}

type decisionEnvelope struct {
	Decision  decisionView   `json:"decision"`
	Stats     decisionStats  `json:"stats"`
	Responses []responseCard `json:"responses"`
}

type decisionView struct {
	ID          string     `json:"id"`
	Slug        string     `json:"slug"`
	Title       string     `json:"title"`
	Description *string    `json:"description"`
	ClosesAt    *time.Time `json:"closes_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

type decisionStats struct {
	ResponseCount int          `json:"response_count"`
	RatingCounts  []int        `json:"rating_counts"`
	AvgRating     float64      `json:"avg_rating"`
	NetSentiment  float64      `json:"net_sentiment"`
	EmojiCounts   []emojiCount `json:"emoji_counts"`
	TopEmoji      string       `json:"top_emoji"`
}

type emojiCount struct {
	Emoji string `json:"emoji"`
	Count int    `json:"count"`
}

type responseCard struct {
	ID        string    `json:"id"`
	Rating    int       `json:"rating"`
	Emoji     string    `json:"emoji"`
	Comment   *string   `json:"comment"`
	CreatedAt time.Time `json:"created_at"`
	Score     int       `json:"score"`
	Upvotes   int       `json:"upvotes"`
	Downvotes int       `json:"downvotes"`
	MyVote    int       `json:"my_vote"`
}

type decisionRecord struct {
	ID          uuid.UUID
	Slug        string
	Title       string
	Description *string
	ClosesAt    *time.Time
	CreatedAt   time.Time
}

func (s *Server) handleGetDecision(w nethttp.ResponseWriter, r *nethttp.Request) {
	slug := chi.URLParam(r, "slug")
	if strings.TrimSpace(slug) == "" {
		writeError(w, nethttp.StatusBadRequest, "slug is required")
		return
	}

	ctx := r.Context()
	viewerID, err := parseViewerIDQuery(r)
	if err != nil {
		writeError(w, nethttp.StatusBadRequest, err.Error())
		return
	}

	decision, err := s.findDecisionBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, nethttp.StatusNotFound, "decision not found")
			return
		}
		writeError(w, nethttp.StatusInternalServerError, "failed to load decision")
		return
	}

	stats, err := s.loadDecisionStats(ctx, decision.ID)
	if err != nil {
		writeError(w, nethttp.StatusInternalServerError, "failed to load decision stats")
		return
	}

	responses, err := s.loadResponseCards(ctx, decision.ID, viewerID)
	if err != nil {
		writeError(w, nethttp.StatusInternalServerError, "failed to load responses")
		return
	}

	out := decisionEnvelope{
		Decision: decisionView{
			ID:          decision.ID.String(),
			Slug:        decision.Slug,
			Title:       decision.Title,
			Description: decision.Description,
			ClosesAt:    decision.ClosesAt,
			CreatedAt:   decision.CreatedAt,
		},
		Stats:     stats,
		Responses: responses,
	}

	writeJSON(w, nethttp.StatusOK, out)
}

func (s *Server) findDecisionBySlug(ctx context.Context, slug string) (decisionRecord, error) {
	var d decisionRecord
	err := s.db.QueryRowContext(ctx, `
		SELECT id, slug, title, description, closes_at, created_at
		FROM decisions
		WHERE slug = $1
	`, slug).Scan(&d.ID, &d.Slug, &d.Title, &d.Description, &d.ClosesAt, &d.CreatedAt)
	return d, err
}

func parseViewerIDQuery(r *nethttp.Request) (*uuid.UUID, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("viewer_id"))
	if raw == "" {
		return nil, nil
	}

	viewerID, err := uuid.Parse(raw)
	if err != nil {
		return nil, errors.New("viewer_id query param must be a valid UUID")
	}
	return &viewerID, nil
}

func (s *Server) loadDecisionStats(ctx context.Context, decisionID uuid.UUID) (decisionStats, error) {
	var (
		responseCount int
		r1            int
		r2            int
		r3            int
		r4            int
		r5            int
		avgRating     float64
	)

	err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*)::int AS response_count,
			COUNT(*) FILTER (WHERE rating = 1)::int AS r1,
			COUNT(*) FILTER (WHERE rating = 2)::int AS r2,
			COUNT(*) FILTER (WHERE rating = 3)::int AS r3,
			COUNT(*) FILTER (WHERE rating = 4)::int AS r4,
			COUNT(*) FILTER (WHERE rating = 5)::int AS r5,
			COALESCE(AVG(rating), 0)::float8 AS avg_rating
		FROM responses
		WHERE decision_id = $1
	`, decisionID).Scan(&responseCount, &r1, &r2, &r3, &r4, &r5, &avgRating)
	if err != nil {
		return decisionStats{}, err
	}

	emojiCounts := make([]emojiCount, 0, 8)
	rows, err := s.db.QueryContext(ctx, `
		SELECT emoji, COUNT(*)::int AS count
		FROM responses
		WHERE decision_id = $1
		GROUP BY emoji
		ORDER BY count DESC, emoji ASC
	`, decisionID)
	if err != nil {
		return decisionStats{}, err
	}
	defer rows.Close()

	for rows.Next() {
		var item emojiCount
		if err := rows.Scan(&item.Emoji, &item.Count); err != nil {
			return decisionStats{}, err
		}
		emojiCounts = append(emojiCounts, item)
	}
	if err := rows.Err(); err != nil {
		return decisionStats{}, err
	}

	topEmoji := ""
	if len(emojiCounts) > 0 {
		topEmoji = emojiCounts[0].Emoji
	}

	netSentiment := clamp((avgRating-3.0)/2.0, -1.0, 1.0)
	stats := decisionStats{
		ResponseCount: responseCount,
		RatingCounts:  []int{r1, r2, r3, r4, r5},
		AvgRating:     avgRating,
		NetSentiment:  netSentiment,
		EmojiCounts:   emojiCounts,
		TopEmoji:      topEmoji,
	}
	return stats, nil
}

func (s *Server) loadResponseCards(ctx context.Context, decisionID uuid.UUID, viewerID *uuid.UUID) ([]responseCard, error) {
	var viewerParam any
	if viewerID != nil {
		viewerParam = *viewerID
	} else {
		viewerParam = nil
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			r.id,
			r.rating,
			r.emoji,
			r.comment,
			r.created_at,
			COALESCE(SUM(v.value), 0)::int AS score,
			COALESCE(COUNT(*) FILTER (WHERE v.value = 1), 0)::int AS upvotes,
			COALESCE(COUNT(*) FILTER (WHERE v.value = -1), 0)::int AS downvotes,
			COALESCE(MAX(CASE WHEN $2::uuid IS NOT NULL AND v.voter_viewer_id = $2::uuid THEN v.value END), 0)::int AS my_vote
		FROM responses r
		LEFT JOIN votes v ON v.response_id = r.id
		WHERE r.decision_id = $1
		GROUP BY r.id
		ORDER BY r.created_at DESC
	`, decisionID, viewerParam)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	responses := make([]responseCard, 0, 16)
	for rows.Next() {
		var item responseCard
		var id uuid.UUID
		if err := rows.Scan(
			&id,
			&item.Rating,
			&item.Emoji,
			&item.Comment,
			&item.CreatedAt,
			&item.Score,
			&item.Upvotes,
			&item.Downvotes,
			&item.MyVote,
		); err != nil {
			return nil, err
		}
		item.ID = id.String()
		responses = append(responses, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return responses, nil
}

func (s *Server) toggleVote(ctx context.Context, responseID uuid.UUID, viewerID uuid.UUID, value int) (voteSummaryResponse, int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return voteSummaryResponse{}, nethttp.StatusInternalServerError, errors.New("failed to start vote transaction")
	}
	defer tx.Rollback()

	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM responses WHERE id = $1)", responseID).Scan(&exists); err != nil {
		return voteSummaryResponse{}, nethttp.StatusInternalServerError, errors.New("failed to check response")
	}
	if !exists {
		return voteSummaryResponse{}, nethttp.StatusNotFound, errors.New("response not found")
	}

	var (
		voteID        uuid.UUID
		existingValue int
	)
	err = tx.QueryRowContext(ctx, `
		SELECT id, value
		FROM votes
		WHERE response_id = $1 AND voter_viewer_id = $2
		FOR UPDATE
	`, responseID, viewerID).Scan(&voteID, &existingValue)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return voteSummaryResponse{}, nethttp.StatusInternalServerError, errors.New("failed to load vote")
	}

	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = tx.ExecContext(ctx, `
			INSERT INTO votes (id, response_id, voter_viewer_id, value)
			VALUES ($1, $2, $3, $4)
		`, uuid.New(), responseID, viewerID, value)
		if err != nil {
			return voteSummaryResponse{}, nethttp.StatusInternalServerError, errors.New("failed to insert vote")
		}
	case existingValue == value:
		_, err = tx.ExecContext(ctx, `
			DELETE FROM votes
			WHERE id = $1
		`, voteID)
		if err != nil {
			return voteSummaryResponse{}, nethttp.StatusInternalServerError, errors.New("failed to remove vote")
		}
	default:
		_, err = tx.ExecContext(ctx, `
			UPDATE votes
			SET value = $1, created_at = now()
			WHERE id = $2
		`, value, voteID)
		if err != nil {
			return voteSummaryResponse{}, nethttp.StatusInternalServerError, errors.New("failed to update vote")
		}
	}

	summary, err := queryVoteSummary(ctx, tx, responseID, viewerID)
	if err != nil {
		return voteSummaryResponse{}, nethttp.StatusInternalServerError, errors.New("failed to summarize vote")
	}
	if err := tx.Commit(); err != nil {
		return voteSummaryResponse{}, nethttp.StatusInternalServerError, errors.New("failed to commit vote")
	}

	summary.ResponseID = responseID.String()
	return summary, nethttp.StatusOK, nil
}

func queryVoteSummary(ctx context.Context, querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, responseID uuid.UUID, viewerID uuid.UUID) (voteSummaryResponse, error) {
	var out voteSummaryResponse
	err := querier.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(value), 0)::int AS score,
			COALESCE(COUNT(*) FILTER (WHERE value = 1), 0)::int AS upvotes,
			COALESCE(COUNT(*) FILTER (WHERE value = -1), 0)::int AS downvotes,
			COALESCE(MAX(CASE WHEN voter_viewer_id = $2 THEN value END), 0)::int AS my_vote
		FROM votes
		WHERE response_id = $1
	`, responseID, viewerID).Scan(&out.Score, &out.Upvotes, &out.Downvotes, &out.MyVote)
	return out, err
}

func normalizeComment(comment *string) (*string, error) {
	if comment == nil {
		return nil, nil
	}

	trimmed := strings.TrimSpace(*comment)
	if trimmed == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(trimmed) > maxCommentLength {
		return nil, fmt.Errorf("comment must be %d characters or fewer", maxCommentLength)
	}

	return &trimmed, nil
}

func clamp(value, minValue, maxValue float64) float64 {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
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
