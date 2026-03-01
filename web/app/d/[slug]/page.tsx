"use client";

import { useParams, useSearchParams } from "next/navigation";
import { FormEvent, useEffect, useMemo, useState } from "react";
import {
  getDecision,
  submitDecisionResponse,
  voteOnDecision,
} from "../../../lib/api";
import {
  getOrCreateViewerId,
  hasDecisionResponded,
  markDecisionResponded,
} from "../../../lib/viewer";
import type { DecisionEnvelope } from "../../../lib/types";

const ratingOptions = [
  { value: 1, label: "Terrible" },
  { value: 2, label: "Bad" },
  { value: 3, label: "Neutral" },
  { value: 4, label: "Good" },
  { value: 5, label: "Great" },
] as const;

const decisionOptions = [
  { value: 1, label: "Don't do it" },
  { value: 2, label: "Mixed" },
  { value: 3, label: "Do it" },
] as const;

const emojis = ["🫠", "😭", "😬", "😄", "🫡"] as const;
type DecisionOption = 1 | 2 | 3;

function formatDate(isoDate: string) {
  return new Date(isoDate).toLocaleString();
}

export default function DecisionPage() {
  const params = useParams<{ slug: string }>();
  const searchParams = useSearchParams();
  const slug = params.slug;
  const isCreatorView = searchParams.get("creator") === "1";

  const [viewerId, setViewerId] = useState<string | null>(null);
  const [data, setData] = useState<DecisionEnvelope | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [isVotingPost, setIsVotingPost] = useState(false);
  const [submittedThisSession, setSubmittedThisSession] = useState(false);
  const [persistedResponded, setPersistedResponded] = useState(false);

  const [decisionOption, setDecisionOption] = useState<DecisionOption | null>(
    null,
  );
  const [emoji, setEmoji] = useState<string>("");
  const [comment, setComment] = useState("");

  useEffect(() => {
    setViewerId(getOrCreateViewerId());
  }, []);

  useEffect(() => {
    if (!slug) {
      return;
    }
    setPersistedResponded(hasDecisionResponded(slug));
  }, [slug]);

  async function refreshDecision(currentViewerId?: string) {
    setLoading(true);
    setError(null);
    try {
      const response = await getDecision(slug, currentViewerId);
      setData(response);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load decision");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    if (!slug) {
      return;
    }
    void refreshDecision(viewerId ?? undefined);
  }, [slug, viewerId]);

  const sortedResponses = useMemo(() => {
    const responses = [...(data?.responses ?? [])];
    responses.sort(
      (a, b) =>
        new Date(b.created_at).getTime() - new Date(a.created_at).getTime(),
    );
    return responses;
  }, [data?.responses]);

  const postVote = data?.post_vote ?? {
    score: 0,
    upvotes: 0,
    downvotes: 0,
    my_vote: 0,
  };
  const recommendation = data?.recommendation ?? {
    decision: "no",
    score: 0,
    suggestion_score: 0,
    rating_score: 0,
    comment_sentiment: 0,
    post_vote_score: 0,
  };
  const supportsPostVote = Boolean(
    (data as { post_vote?: unknown } | null)?.post_vote,
  );
  const supportsRecommendation = Boolean(
    (data as { recommendation?: unknown } | null)?.recommendation,
  );
  const viewerHasRespondedFromResponses = Boolean(
    viewerId &&
    (data?.responses as Array<{ viewer_id?: string }> | undefined)?.some(
      (response) => response.viewer_id === viewerId,
    ),
  );
  const viewerHasResponded =
    (data?.viewer_has_responded ?? false) ||
    submittedThisSession ||
    persistedResponded ||
    viewerHasRespondedFromResponses;

  async function onSubmitResponse(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!viewerId) {
      return;
    }

    setSubmitError(null);
    if (!decisionOption) {
      setSubmitError("Choose Do it, Don't do it, or Mixed.");
      return;
    }
    if (!emoji) {
      setSubmitError("Choose an emoji.");
      return;
    }

    setIsSubmitting(true);
    try {
      await submitDecisionResponse(slug, {
        viewer_id: viewerId,
        rating: 0,
        suggestion: decisionOption,
        emoji,
        comment: comment.trim() || null,
      });

      setDecisionOption(null);
      setEmoji("");
      setComment("");
      setSubmittedThisSession(true);
      markDecisionResponded(slug);
      setPersistedResponded(true);
      await refreshDecision(viewerId);
    } catch (err) {
      const message =
        err instanceof Error ? err.message : "Failed to submit response";
      setSubmitError(message);
      if (message.includes("already submitted")) {
        setSubmittedThisSession(true);
        markDecisionResponded(slug);
        setPersistedResponded(true);
        await refreshDecision(viewerId);
      }
    } finally {
      setIsSubmitting(false);
    }
  }

  async function onVotePost(value: 1 | -1) {
    if (!viewerId) {
      return;
    }

    setIsVotingPost(true);
    try {
      await voteOnDecision(slug, { viewer_id: viewerId, value });
      await refreshDecision(viewerId);
    } catch (err) {
      const message = err instanceof Error ? err.message : "Failed to vote";
      if (message.includes("404")) {
        setSubmitError(
          "Post voting endpoint not found. Restart backend with latest code and rerun migrations.",
        );
      } else {
        setSubmitError(message);
      }
    } finally {
      setIsVotingPost(false);
    }
  }

  return (
    <main className="shell">
      {loading ? <p>Loading decision...</p> : null}
      {error ? <p className="error">{error}</p> : null}

      {!loading && data ? (
        <>
          <section className="banner">
            <h1 className="title">{data.decision.title}</h1>
            {data.decision.description ? (
              <p className="subtitle">{data.decision.description}</p>
            ) : null}
            <p className="subtitle">
              Created: {formatDate(data.decision.created_at)}
              {data.decision.closes_at
                ? ` • Closes: ${formatDate(data.decision.closes_at)}`
                : ""}
            </p>
            <div className="vote-row">
              {!isCreatorView ? (
                <>
                  <button
                    className={`vote-btn ${postVote.my_vote === 1 ? "active-up" : ""}`}
                    type="button"
                    disabled={isVotingPost || !supportsPostVote}
                    onClick={() => onVotePost(1)}
                  >
                    ▲ {postVote.upvotes}
                  </button>
                  <button
                    className={`vote-btn ${postVote.my_vote === -1 ? "active-down" : ""}`}
                    type="button"
                    disabled={isVotingPost || !supportsPostVote}
                    onClick={() => onVotePost(-1)}
                  >
                    ▼ {postVote.downvotes}
                  </button>
                </>
              ) : null}
              <strong>Post Score: {postVote.score}</strong>
            </div>
            {!supportsPostVote ? (
              <p className="muted">
                Post voting unavailable on current backend version. Restart
                backend after migration.
              </p>
            ) : null}
          </section>

          <section className="grid">
            <article className="card">
              {isCreatorView ? (
                <>
                  <h2>Creator View</h2>
                  <p className="muted">
                    You are viewing this decision as the creator. Response form
                    is hidden.
                  </p>
                </>
              ) : (
                <>
                  <h2>Submit Your Response</h2>
                  {viewerHasResponded ? (
                    <p className="success">
                      You already submitted a response for this decision.
                    </p>
                  ) : (
                    <form onSubmit={onSubmitResponse}>
                      <div className="field">
                        <label>Your Suggestion</label>
                        <div className="rating-grid">
                          {decisionOptions.map((option) => (
                            <button
                              key={option.value}
                              className={`rating-btn ${decisionOption === option.value ? "active" : ""}`}
                              type="button"
                              onClick={() => setDecisionOption(option.value)}
                            >
                              {option.label}
                            </button>
                          ))}
                        </div>
                      </div>

                      <div className="field">
                        <label>Emoji</label>
                        <div className="emoji-grid">
                          {emojis.map((value) => (
                            <button
                              key={value}
                              className={`emoji-btn ${emoji === value ? "active" : ""}`}
                              type="button"
                              onClick={() => setEmoji(value)}
                            >
                              {value}
                            </button>
                          ))}
                        </div>
                      </div>

                      <div className="field">
                        <label htmlFor="comment">Comment (max 180)</label>
                        <textarea
                          id="comment"
                          className="textarea"
                          maxLength={180}
                          placeholder="Drop your honest take..."
                          value={comment}
                          onChange={(e) => setComment(e.target.value)}
                        />
                        <small className="muted">{comment.length}/180</small>
                      </div>

                      <button
                        className="btn btn-hot"
                        type="submit"
                        disabled={isSubmitting}
                      >
                        {isSubmitting ? "Submitting..." : "Submit Response"}
                      </button>
                    </form>
                  )}
                </>
              )}
              {submitError ? <p className="error">{submitError}</p> : null}
            </article>

            {isCreatorView ? (
              <article className="card">
                <h2>Stats</h2>
                <div className="stats-grid">
                  <div className="metric">
                    <strong>Rating Distribution</strong>
                    <div style={{ marginTop: 10 }}>
                      {ratingOptions.map((option, index) => {
                        const count = data.stats.rating_counts[index] ?? 0;
                        const total = data.stats.response_count || 1;
                        const width = Math.round((count / total) * 100);
                        return (
                          <div key={option.value} className="rating-bar">
                            <span>{option.label}</span>
                            <div className="rating-track">
                              <div
                                className="rating-fill"
                                style={{ width: `${width}%` }}
                              />
                            </div>
                            <span>{count}</span>
                          </div>
                        );
                      })}
                    </div>
                  </div>

                  <div className="metric">
                    <strong>Summary</strong>
                    <p>Responses: {data.stats.response_count}</p>
                    <p>Avg rating: {data.stats.avg_rating.toFixed(2)}</p>
                    <p>Top emoji: {data.stats.top_emoji || "—"}</p>
                    <p>Do it: {data.stats.categories.do_it}</p>
                    <p>Don't do it: {data.stats.categories.dont_do_it}</p>
                    <p>Mixed: {data.stats.categories.mixed}</p>
                  </div>

                  <div className="metric">
                    <strong>Emoji Counts</strong>
                    {data.stats.emoji_counts.length === 0 ? (
                      <p className="muted">No emoji data yet.</p>
                    ) : (
                      data.stats.emoji_counts.map((item) => (
                        <p key={item.emoji}>
                          {item.emoji} × {item.count}
                        </p>
                      ))
                    )}
                  </div>
                </div>
              </article>
            ) : null}

            {isCreatorView ? (
              <article className="card">
                <h2>Model Recommendation</h2>
                {supportsRecommendation ? (
                  <>
                    <p>
                      According to our analysis, our model recommends{" "}
                      <strong>
                        {recommendation.decision === "yes"
                          ? "pursuing"
                          : "not pursuing"}
                      </strong>{" "}
                      this decision.
                    </p>
                  </>
                ) : (
                  <p className="muted">
                    Model recommendation unavailable on current backend version.
                  </p>
                )}
              </article>
            ) : null}

            <article className="card">
              <h2>Responses</h2>
              {sortedResponses.length === 0 ? (
                <p className="muted">No responses yet.</p>
              ) : null}
              <div className="response-list">
                {sortedResponses.map((response) => (
                  <div key={response.id} className="response-card">
                    <div className="response-head">
                      <strong>
                        {response.emoji}{" "}
                        {
                          ratingOptions.find(
                            (item) => item.value === response.rating,
                          )?.label
                        }
                      </strong>
                      <small className="muted">
                        {formatDate(response.created_at)}
                      </small>
                    </div>

                    {response.comment ? (
                      <p>{response.comment}</p>
                    ) : (
                      <p className="muted">No comment.</p>
                    )}
                  </div>
                ))}
              </div>
            </article>
          </section>
        </>
      ) : null}
    </main>
  );
}
