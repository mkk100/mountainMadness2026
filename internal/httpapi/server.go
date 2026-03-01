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
	"net"
	nethttp "net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	slugMaxAttempts            = 8
	slugMaxLength              = 128
	titleMinLength             = 4
	titleMaxLength             = 100
	descriptionMaxLength       = 500
	maxCommentLength           = 180
	maxCreateDecisionBodyBytes = 4 * 1024
	maxResponseBodyBytes       = 4 * 1024
	maxVoteBodyBytes           = 2 * 1024
	suggestionWeight           = 0.35
	ratingWeight               = 0.30
	commentSentimentWeight     = 0.20
	postVoteWeight             = 0.15
	recommendationYesThreshold = 0.0
	ipRateLimitPerMinute       = 120
	viewerRateLimitPerMinute   = 60
	rateLimitWindow            = time.Minute
)

var emojiRatings = map[string]int{
	"🫠": 1,
	"😭": 2,
	"😬": 3,
	"😄": 4,
	"🫡": 5,
}

var positiveSentimentWords = map[string]struct{}{
	"amazing":     {},
	"better":      {},
	"benefit":     {},
	"best":        {},
	"excellent":   {},
	"good":        {},
	"great":       {},
	"growth":      {},
	"happy":       {},
	"love":        {},
	"opportunity": {},
	"positive":    {},
	"safe":        {},
	"smart":       {},
	"strong":      {},
	"support":     {},
	"upside":      {},
	"worth":       {},
	"yes":         {},
	"win":         {},
}

var negativeSentimentWords = map[string]struct{}{
	"bad":       {},
	"concern":   {},
	"costly":    {},
	"difficult": {},
	"downside":  {},
	"expensive": {},
	"hard":      {},
	"hate":      {},
	"loss":      {},
	"negative":  {},
	"no":        {},
	"problem":   {},
	"risk":      {},
	"risky":     {},
	"stress":    {},
	"unsafe":    {},
	"worse":     {},
	"worst":     {},
}

type Server struct {
	db                *sql.DB
	ipLimiter         *fixedWindowLimiter
	viewerLimiter     *fixedWindowLimiter
	allowedOrigins    map[string]struct{}
	allowAnyOrigin    bool
	trustProxyHeaders bool
	writeAPIKeys      map[string]struct{}
}

type rateWindowCounter struct {
	count   int
	resetAt time.Time
}

type fixedWindowLimiter struct {
	mu          sync.Mutex
	window      time.Duration
	limit       int
	buckets     map[string]rateWindowCounter
	lastCleanup time.Time
}

func New(db *sql.DB) nethttp.Handler {
	allowedOrigins, allowAnyOrigin := loadAllowedOriginsFromEnv()
	s := &Server{
		db:                db,
		ipLimiter:         newFixedWindowLimiter(ipRateLimitPerMinute, rateLimitWindow),
		viewerLimiter:     newFixedWindowLimiter(viewerRateLimitPerMinute, rateLimitWindow),
		allowedOrigins:    allowedOrigins,
		allowAnyOrigin:    allowAnyOrigin,
		trustProxyHeaders: parseBoolEnv("TRUST_PROXY_HEADERS", false),
		writeAPIKeys:      loadAPIKeysFromEnv("WRITE_API_KEYS"),
	}
	r := chi.NewRouter()
	r.Use(s.securityHeadersMiddleware)
	r.Use(s.corsMiddleware)
	r.Use(s.rateLimitMiddleware)

	r.Get("/health", s.handleHealth)
	r.Get("/api/decisions/{slug}", s.handleGetDecision)
	r.Group(func(r chi.Router) {
		// Optional API key auth for write routes supports key rotation:
		// provide one or more comma-separated keys via WRITE_API_KEYS.
		r.Use(s.requireWriteAPIKeyMiddleware)
		r.Post("/api/decisions", s.handleCreateDecision)
		r.Post("/api/decisions/{slug}/responses", s.handleCreateResponse)
		r.Post("/api/decisions/{slug}/vote", s.handleDecisionVote)
		r.Post("/api/decisions/{slug}/votes", s.handleDecisionVote)
	})

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
	if err := decodeJSON(w, r, maxCreateDecisionBodyBytes, &req); err != nil {
		writeError(w, nethttp.StatusBadRequest, err.Error())
		return
	}

	title, err := normalizeRequiredText(req.Title, titleMinLength, titleMaxLength, "title", false)
	if err != nil {
		writeError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	description, err := normalizeOptionalText(req.Description, descriptionMaxLength, "description", true)
	if err != nil {
		writeError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	closesAt, err := normalizeClosesAt(req.ClosesAt)
	if err != nil {
		writeError(w, nethttp.StatusBadRequest, err.Error())
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
			description,
			closesAt,
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
	ViewerID   string  `json:"viewer_id"`
	Rating     int     `json:"rating"`
	Suggestion int     `json:"suggestion"`
	Emoji      string  `json:"emoji"`
	Comment    *string `json:"comment"`
}

func (s *Server) handleCreateResponse(w nethttp.ResponseWriter, r *nethttp.Request) {
	slug, err := normalizeSlugParam(chi.URLParam(r, "slug"))
	if err != nil {
		writeError(w, nethttp.StatusBadRequest, err.Error())
		return
	}

	var req decisionResponsePayload
	if err := decodeJSON(w, r, maxResponseBodyBytes, &req); err != nil {
		writeError(w, nethttp.StatusBadRequest, err.Error())
		return
	}

	viewerID, err := uuid.Parse(strings.TrimSpace(req.ViewerID))
	if err != nil {
		writeError(w, nethttp.StatusBadRequest, "viewer_id must be a valid UUID")
		return
	}
	if !s.allowViewerRequest(w, viewerID.String()) {
		return
	}
	if req.Suggestion < 1 || req.Suggestion > 3 {
		writeError(w, nethttp.StatusBadRequest, "suggestion must be 1 (don't do it), 2 (mixed), or 3 (do it)")
		return
	}
	emoji := strings.TrimSpace(req.Emoji)
	rating, ok := emojiRatings[emoji]
	if !ok {
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
		INSERT INTO responses (id, decision_id, viewer_id, rating, suggestion, emoji, comment)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`,
		responseID,
		decision.ID,
		viewerID,
		rating,
		req.Suggestion,
		emoji,
		comment,
	)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, nethttp.StatusConflict, "viewer already submitted a response for this decision")
			return
		}
		if isUndefinedColumn(err) {
			writeError(w, nethttp.StatusInternalServerError, "database schema is out of date. Run migrations and restart the server")
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

type decisionVoteSummary struct {
	Score     int `json:"score"`
	Upvotes   int `json:"upvotes"`
	Downvotes int `json:"downvotes"`
	MyVote    int `json:"my_vote"`
}

type decisionVoteSummaryResponse struct {
	DecisionID string `json:"decision_id"`
	Score      int    `json:"score"`
	Upvotes    int    `json:"upvotes"`
	Downvotes  int    `json:"downvotes"`
	MyVote     int    `json:"my_vote"`
}

func (s *Server) handleDecisionVote(w nethttp.ResponseWriter, r *nethttp.Request) {
	slug, err := normalizeSlugParam(chi.URLParam(r, "slug"))
	if err != nil {
		writeError(w, nethttp.StatusBadRequest, err.Error())
		return
	}

	var req voteRequest
	if err := decodeJSON(w, r, maxVoteBodyBytes, &req); err != nil {
		writeError(w, nethttp.StatusBadRequest, err.Error())
		return
	}

	viewerID, err := uuid.Parse(strings.TrimSpace(req.ViewerID))
	if err != nil {
		writeError(w, nethttp.StatusBadRequest, "viewer_id must be a valid UUID")
		return
	}
	if !s.allowViewerRequest(w, viewerID.String()) {
		return
	}
	if req.Value != -1 && req.Value != 1 {
		writeError(w, nethttp.StatusBadRequest, "value must be -1 or 1")
		return
	}

	decision, err := s.findDecisionBySlug(r.Context(), slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, nethttp.StatusNotFound, "decision not found")
			return
		}
		writeError(w, nethttp.StatusInternalServerError, "failed to load decision")
		return
	}

	summary, status, err := s.toggleDecisionVote(r.Context(), decision.ID, viewerID, req.Value)
	if err != nil {
		writeError(w, status, err.Error())
		return
	}

	writeJSON(w, nethttp.StatusOK, decisionVoteSummaryResponse{
		DecisionID: decision.ID.String(),
		Score:      summary.Score,
		Upvotes:    summary.Upvotes,
		Downvotes:  summary.Downvotes,
		MyVote:     summary.MyVote,
	})
}

type decisionEnvelope struct {
	Decision           decisionView        `json:"decision"`
	Stats              decisionStats       `json:"stats"`
	Recommendation     recommendationView  `json:"recommendation"`
	PostVote           decisionVoteSummary `json:"post_vote"`
	ViewerHasResponded bool                `json:"viewer_has_responded"`
	Responses          []responseCard      `json:"responses"`
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
	Categories    voteBuckets  `json:"categories"`
	EmojiCounts   []emojiCount `json:"emoji_counts"`
	TopEmoji      string       `json:"top_emoji"`
}

type recommendationView struct {
	Decision         string  `json:"decision"`
	Score            float64 `json:"score"`
	SuggestionScore  float64 `json:"suggestion_score"`
	RatingScore      float64 `json:"rating_score"`
	CommentSentiment float64 `json:"comment_sentiment"`
	PostVoteScore    float64 `json:"post_vote_score"`
}

type voteBuckets struct {
	DoIt     int `json:"do_it"`
	DontDoIt int `json:"dont_do_it"`
	Mixed    int `json:"mixed"`
}

type emojiCount struct {
	Emoji string `json:"emoji"`
	Count int    `json:"count"`
}

type responseCard struct {
	ID         string    `json:"id"`
	Rating     int       `json:"rating"`
	Suggestion int       `json:"suggestion"`
	Emoji      string    `json:"emoji"`
	Comment    *string   `json:"comment"`
	CreatedAt  time.Time `json:"created_at"`
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
	slug, err := normalizeSlugParam(chi.URLParam(r, "slug"))
	if err != nil {
		writeError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	if err := validateDecisionQueryParams(r); err != nil {
		writeError(w, nethttp.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	viewerID, err := parseViewerIDQuery(r)
	if err != nil {
		writeError(w, nethttp.StatusBadRequest, err.Error())
		return
	}
	if viewerID != nil && !s.allowViewerRequest(w, viewerID.String()) {
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
		if isUndefinedColumn(err) {
			writeError(w, nethttp.StatusInternalServerError, "database schema is out of date. Run migrations and restart the server")
			return
		}
		writeError(w, nethttp.StatusInternalServerError, "failed to load decision stats")
		return
	}

	recommendation, err := s.loadRecommendation(ctx, decision.ID)
	if err != nil {
		if isUndefinedColumn(err) {
			writeError(w, nethttp.StatusInternalServerError, "database schema is out of date. Run migrations and restart the server")
			return
		}
		writeError(w, nethttp.StatusInternalServerError, "failed to compute decision recommendation")
		return
	}

	postVote, err := s.queryDecisionVoteSummary(ctx, s.db, decision.ID, viewerID)
	if err != nil {
		writeError(w, nethttp.StatusInternalServerError, "failed to load post votes")
		return
	}

	viewerHasResponded, err := s.viewerHasResponded(ctx, decision.ID, viewerID)
	if err != nil {
		writeError(w, nethttp.StatusInternalServerError, "failed to load viewer response state")
		return
	}

	responses, err := s.loadResponseCards(ctx, decision.ID)
	if err != nil {
		if isUndefinedColumn(err) {
			writeError(w, nethttp.StatusInternalServerError, "database schema is out of date. Run migrations and restart the server")
			return
		}
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
		Stats:              stats,
		Recommendation:     recommendation,
		PostVote:           postVote,
		ViewerHasResponded: viewerHasResponded,
		Responses:          responses,
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
	values, exists := r.URL.Query()["viewer_id"]
	if !exists || len(values) == 0 {
		return nil, nil
	}
	if len(values) > 1 {
		return nil, errors.New("viewer_id query param must appear only once")
	}

	raw := strings.TrimSpace(values[0])
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
		s1            int
		s2            int
		s3            int
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
			COUNT(*) FILTER (WHERE suggestion = 1)::int AS s1,
			COUNT(*) FILTER (WHERE suggestion = 2)::int AS s2,
			COUNT(*) FILTER (WHERE suggestion = 3)::int AS s3,
			COALESCE(AVG(rating), 0)::float8 AS avg_rating
		FROM responses
		WHERE decision_id = $1
	`, decisionID).Scan(&responseCount, &r1, &r2, &r3, &r4, &r5, &s1, &s2, &s3, &avgRating)
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
		Categories: voteBuckets{
			DoIt:     s3,
			DontDoIt: s1,
			Mixed:    s2,
		},
		EmojiCounts: emojiCounts,
		TopEmoji:    topEmoji,
	}
	return stats, nil
}

func (s *Server) loadRecommendation(ctx context.Context, decisionID uuid.UUID) (recommendationView, error) {
	var (
		voteSum   int
		voteCount int
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(value), 0)::int AS vote_sum,
			COUNT(*)::int AS vote_count
		FROM decision_votes
		WHERE decision_id = $1
	`, decisionID).Scan(&voteSum, &voteCount)
	if err != nil {
		return recommendationView{}, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT suggestion, rating, comment
		FROM responses
		WHERE decision_id = $1
	`, decisionID)
	if err != nil {
		return recommendationView{}, err
	}
	defer rows.Close()

	var (
		responseCount         int
		commentCount          int
		suggestionScoreTotal  float64
		ratingScoreTotal      float64
		commentSentimentTotal float64
	)

	for rows.Next() {
		var (
			suggestion int
			rating     int
			comment    *string
		)
		if err := rows.Scan(&suggestion, &rating, &comment); err != nil {
			return recommendationView{}, err
		}

		responseCount++
		suggestionScoreTotal += suggestionToScore(suggestion)
		ratingScoreTotal += clamp((float64(rating)-3.0)/2.0, -1.0, 1.0)

		if comment != nil {
			commentSentimentTotal += analyzeCommentSentiment(*comment)
			commentCount++
		}
	}
	if err := rows.Err(); err != nil {
		return recommendationView{}, err
	}

	suggestionScore := 0.0
	ratingScore := 0.0
	commentSentiment := 0.0
	postVoteScore := 0.0

	if responseCount > 0 {
		suggestionScore = suggestionScoreTotal / float64(responseCount)
		ratingScore = ratingScoreTotal / float64(responseCount)
	}
	if commentCount > 0 {
		commentSentiment = commentSentimentTotal / float64(commentCount)
	}
	if voteCount > 0 {
		postVoteScore = clamp(float64(voteSum)/float64(voteCount), -1.0, 1.0)
	}

	score := clamp(
		(suggestionWeight*suggestionScore)+
			(ratingWeight*ratingScore)+
			(commentSentimentWeight*commentSentiment)+
			(postVoteWeight*postVoteScore),
		-1.0,
		1.0,
	)

	decision := "no"
	if score >= recommendationYesThreshold {
		decision = "yes"
	}

	return recommendationView{
		Decision:         decision,
		Score:            score,
		SuggestionScore:  suggestionScore,
		RatingScore:      ratingScore,
		CommentSentiment: commentSentiment,
		PostVoteScore:    postVoteScore,
	}, nil
}

func suggestionToScore(suggestion int) float64 {
	switch suggestion {
	case 1:
		return -1.0
	case 2:
		return 0.0
	case 3:
		return 1.0
	default:
		return 0.0
	}
}

func analyzeCommentSentiment(comment string) float64 {
	words := strings.FieldsFunc(strings.ToLower(comment), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '\''
	})
	if len(words) == 0 {
		return 0.0
	}

	positiveCount := 0
	negativeCount := 0
	for _, word := range words {
		normalized := strings.ReplaceAll(word, "'", "")
		if _, ok := positiveSentimentWords[normalized]; ok {
			positiveCount++
		}
		if _, ok := negativeSentimentWords[normalized]; ok {
			negativeCount++
		}
	}

	totalHits := positiveCount + negativeCount
	if totalHits == 0 {
		return 0.0
	}

	return clamp(float64(positiveCount-negativeCount)/float64(totalHits), -1.0, 1.0)
}

func (s *Server) loadResponseCards(ctx context.Context, decisionID uuid.UUID) ([]responseCard, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			r.id,
			r.rating,
			r.suggestion,
			r.emoji,
			r.comment,
			r.created_at
		FROM responses r
		WHERE r.decision_id = $1
		ORDER BY r.created_at DESC
	`, decisionID)
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
			&item.Suggestion,
			&item.Emoji,
			&item.Comment,
			&item.CreatedAt,
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

func (s *Server) viewerHasResponded(ctx context.Context, decisionID uuid.UUID, viewerID *uuid.UUID) (bool, error) {
	if viewerID == nil {
		return false, nil
	}

	var exists bool
	err := s.db.QueryRowContext(
		ctx,
		"SELECT EXISTS(SELECT 1 FROM responses WHERE decision_id = $1 AND viewer_id = $2)",
		decisionID,
		*viewerID,
	).Scan(&exists)
	return exists, err
}

func (s *Server) toggleDecisionVote(ctx context.Context, decisionID uuid.UUID, viewerID uuid.UUID, value int) (decisionVoteSummary, int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return decisionVoteSummary{}, nethttp.StatusInternalServerError, errors.New("failed to start vote transaction")
	}
	defer tx.Rollback()

	var (
		voteID        uuid.UUID
		existingValue int
	)
	err = tx.QueryRowContext(ctx, `
		SELECT id, value
		FROM decision_votes
		WHERE decision_id = $1 AND voter_viewer_id = $2
		FOR UPDATE
	`, decisionID, viewerID).Scan(&voteID, &existingValue)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return decisionVoteSummary{}, nethttp.StatusInternalServerError, errors.New("failed to load vote")
	}

	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = tx.ExecContext(ctx, `
			INSERT INTO decision_votes (id, decision_id, voter_viewer_id, value)
			VALUES ($1, $2, $3, $4)
		`, uuid.New(), decisionID, viewerID, value)
		if err != nil {
			return decisionVoteSummary{}, nethttp.StatusInternalServerError, errors.New("failed to insert vote")
		}
	case existingValue == value:
		_, err = tx.ExecContext(ctx, `
			DELETE FROM decision_votes
			WHERE id = $1
		`, voteID)
		if err != nil {
			return decisionVoteSummary{}, nethttp.StatusInternalServerError, errors.New("failed to remove vote")
		}
	default:
		_, err = tx.ExecContext(ctx, `
			UPDATE decision_votes
			SET value = $1, created_at = now()
			WHERE id = $2
		`, value, voteID)
		if err != nil {
			return decisionVoteSummary{}, nethttp.StatusInternalServerError, errors.New("failed to update vote")
		}
	}

	summary, err := s.queryDecisionVoteSummary(ctx, tx, decisionID, &viewerID)
	if err != nil {
		return decisionVoteSummary{}, nethttp.StatusInternalServerError, errors.New("failed to summarize vote")
	}
	if err := tx.Commit(); err != nil {
		return decisionVoteSummary{}, nethttp.StatusInternalServerError, errors.New("failed to commit vote")
	}

	return summary, nethttp.StatusOK, nil
}

func (s *Server) queryDecisionVoteSummary(ctx context.Context, querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, decisionID uuid.UUID, viewerID *uuid.UUID) (decisionVoteSummary, error) {
	var out decisionVoteSummary
	var viewerParam any
	if viewerID != nil {
		viewerParam = *viewerID
	} else {
		viewerParam = nil
	}

	err := querier.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(value), 0)::int AS score,
			COALESCE(COUNT(*) FILTER (WHERE value = 1), 0)::int AS upvotes,
			COALESCE(COUNT(*) FILTER (WHERE value = -1), 0)::int AS downvotes,
			COALESCE(MAX(CASE WHEN $2::uuid IS NOT NULL AND voter_viewer_id = $2::uuid THEN value END), 0)::int AS my_vote
		FROM decision_votes
		WHERE decision_id = $1
	`, decisionID, viewerParam).Scan(&out.Score, &out.Upvotes, &out.Downvotes, &out.MyVote)
	return out, err
}

func normalizeComment(comment *string) (*string, error) {
	if comment == nil {
		return nil, nil
	}

	trimmed := strings.TrimSpace(normalizeLineBreaks(*comment))
	if trimmed == "" {
		return nil, nil
	}
	if containsDisallowedControlChars(trimmed, true) {
		return nil, errors.New("comment contains unsupported control characters")
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

func (s *Server) securityHeadersMiddleware(next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) corsMiddleware(next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" && s.isOriginAllowed(origin) {
			if s.allowAnyOrigin {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key")
			w.Header().Set("Access-Control-Max-Age", "300")
		}

		if r.Method == nethttp.MethodOptions {
			if origin != "" && !s.isOriginAllowed(origin) {
				writeError(w, nethttp.StatusForbidden, "origin not allowed")
				return
			}
			w.WriteHeader(nethttp.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) rateLimitMiddleware(next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method == nethttp.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		clientIP := s.clientIPFromRequest(r)
		allowed, retryAfter := s.ipLimiter.Allow("ip:"+clientIP, time.Now())
		if !allowed {
			writeRateLimitExceeded(w, retryAfter)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireWriteAPIKeyMiddleware(next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if len(s.writeAPIKeys) == 0 {
			next.ServeHTTP(w, r)
			return
		}

		apiKey := strings.TrimSpace(r.Header.Get("X-API-Key"))
		if apiKey == "" {
			writeError(w, nethttp.StatusUnauthorized, "missing API key")
			return
		}
		if _, ok := s.writeAPIKeys[apiKey]; !ok {
			writeError(w, nethttp.StatusUnauthorized, "invalid API key")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) allowViewerRequest(w nethttp.ResponseWriter, viewerID string) bool {
	allowed, retryAfter := s.viewerLimiter.Allow("viewer:"+viewerID, time.Now())
	if !allowed {
		writeRateLimitExceeded(w, retryAfter)
		return false
	}
	return true
}

func writeRateLimitExceeded(w nethttp.ResponseWriter, retryAfter time.Duration) {
	seconds := int(retryAfter.Seconds())
	if retryAfter > 0 && seconds == 0 {
		seconds = 1
	}
	if seconds < 0 {
		seconds = 0
	}

	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeJSON(w, nethttp.StatusTooManyRequests, map[string]any{
		"error":               "rate limit exceeded",
		"retry_after_seconds": seconds,
	})
}

func newFixedWindowLimiter(limit int, window time.Duration) *fixedWindowLimiter {
	return &fixedWindowLimiter{
		window:      window,
		limit:       limit,
		buckets:     make(map[string]rateWindowCounter, 2048),
		lastCleanup: time.Now(),
	}
}

func (l *fixedWindowLimiter) Allow(key string, now time.Time) (bool, time.Duration) {
	if l.limit <= 0 {
		return true, 0
	}
	if key == "" {
		key = "unknown"
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastCleanup) >= l.window {
		for k, bucket := range l.buckets {
			if !now.Before(bucket.resetAt) {
				delete(l.buckets, k)
			}
		}
		l.lastCleanup = now
	}

	bucket, exists := l.buckets[key]
	if !exists || !now.Before(bucket.resetAt) {
		bucket = rateWindowCounter{
			count:   0,
			resetAt: now.Add(l.window),
		}
	}

	if bucket.count >= l.limit {
		return false, bucket.resetAt.Sub(now)
	}

	bucket.count++
	l.buckets[key] = bucket
	return true, 0
}

func (s *Server) isOriginAllowed(origin string) bool {
	if s.allowAnyOrigin {
		return true
	}
	_, ok := s.allowedOrigins[origin]
	return ok
}

func (s *Server) clientIPFromRequest(r *nethttp.Request) string {
	if s.trustProxyHeaders {
		if forwardedFor := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwardedFor != "" {
			parts := strings.Split(forwardedFor, ",")
			if len(parts) > 0 {
				if ip := parseIPCandidate(parts[0]); ip != "" {
					return ip
				}
			}
		}
		if realIP := strings.TrimSpace(r.Header.Get("X-Real-Ip")); realIP != "" {
			if ip := parseIPCandidate(realIP); ip != "" {
				return ip
			}
		}
	}

	if host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr)); err == nil {
		if ip := parseIPCandidate(host); ip != "" {
			return ip
		}
	}
	if ip := parseIPCandidate(r.RemoteAddr); ip != "" {
		return ip
	}
	return "unknown"
}

func parseIPCandidate(raw string) string {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil {
		return ""
	}
	return ip.String()
}

func loadAllowedOriginsFromEnv() (map[string]struct{}, bool) {
	raw := strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGINS"))
	if raw == "" {
		raw = "http://localhost:3000,http://127.0.0.1:3000"
	}

	if raw == "*" {
		return nil, true
	}

	origins := make(map[string]struct{}, 8)
	for _, part := range strings.Split(raw, ",") {
		origin := strings.TrimSpace(part)
		if origin == "" {
			continue
		}
		origins[origin] = struct{}{}
	}
	return origins, false
}

func loadAPIKeysFromEnv(key string) map[string]struct{} {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}

	keys := make(map[string]struct{}, 8)
	for _, part := range strings.Split(raw, ",") {
		parsed := strings.TrimSpace(part)
		if parsed == "" {
			continue
		}
		keys[parsed] = struct{}{}
	}
	return keys
}

func parseBoolEnv(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func normalizeRequiredText(raw string, minLen, maxLen int, field string, allowNewLines bool) (string, error) {
	normalized := strings.TrimSpace(normalizeLineBreaks(raw))
	if normalized == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if containsDisallowedControlChars(normalized, allowNewLines) {
		return "", fmt.Errorf("%s contains unsupported control characters", field)
	}
	length := utf8.RuneCountInString(normalized)
	if length < minLen || length > maxLen {
		return "", fmt.Errorf("%s must be between %d and %d characters", field, minLen, maxLen)
	}
	return normalized, nil
}

func normalizeOptionalText(raw *string, maxLen int, field string, allowNewLines bool) (*string, error) {
	if raw == nil {
		return nil, nil
	}

	normalized := strings.TrimSpace(normalizeLineBreaks(*raw))
	if normalized == "" {
		return nil, nil
	}
	if containsDisallowedControlChars(normalized, allowNewLines) {
		return nil, fmt.Errorf("%s contains unsupported control characters", field)
	}
	if utf8.RuneCountInString(normalized) > maxLen {
		return nil, fmt.Errorf("%s must be %d characters or fewer", field, maxLen)
	}
	return &normalized, nil
}

func normalizeLineBreaks(input string) string {
	return strings.ReplaceAll(strings.ReplaceAll(input, "\r\n", "\n"), "\r", "\n")
}

func containsDisallowedControlChars(input string, allowNewLines bool) bool {
	for _, r := range input {
		if !unicode.IsControl(r) {
			continue
		}
		if allowNewLines && (r == '\n' || r == '\t') {
			continue
		}
		return true
	}
	return false
}

func normalizeClosesAt(raw *time.Time) (*time.Time, error) {
	if raw == nil {
		return nil, nil
	}

	closesAt := raw.UTC()
	if closesAt.Before(time.Now().UTC().Add(-1 * time.Minute)) {
		return nil, errors.New("closes_at must be in the future")
	}
	return &closesAt, nil
}

func normalizeSlugParam(raw string) (string, error) {
	slug := strings.TrimSpace(raw)
	if slug == "" {
		return "", errors.New("slug is required")
	}
	if len(slug) > slugMaxLength {
		return "", fmt.Errorf("slug must be %d characters or fewer", slugMaxLength)
	}
	if !isValidSlug(slug) {
		return "", errors.New("slug is invalid")
	}
	return slug, nil
}

func isValidSlug(slug string) bool {
	if strings.HasPrefix(slug, "-") || strings.HasSuffix(slug, "-") {
		return false
	}
	for _, r := range slug {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return false
		}
	}
	return true
}

func validateDecisionQueryParams(r *nethttp.Request) error {
	for key := range r.URL.Query() {
		if key != "viewer_id" {
			return fmt.Errorf("unexpected query parameter: %s", key)
		}
	}
	return nil
}

func decodeJSON(w nethttp.ResponseWriter, r *nethttp.Request, maxBytes int64, out any) error {
	defer r.Body.Close()

	limitedBody := nethttp.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(limitedBody)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		var maxBytesErr *nethttp.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return fmt.Errorf("request body must be %d bytes or fewer", maxBytes)
		}
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

func isUndefinedColumn(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42703"
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
