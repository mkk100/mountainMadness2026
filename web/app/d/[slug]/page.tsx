"use client";

import { useParams } from "next/navigation";
import { FormEvent, useEffect, useMemo, useState } from "react";
import { getDecision, submitDecisionResponse, voteOnResponse } from "../../../lib/api";
import { getOrCreateViewerId } from "../../../lib/viewer";
import type { DecisionEnvelope } from "../../../lib/types";

const ratingOptions = [
  { value: 1, label: "Terrible" },
  { value: 2, label: "Bad" },
  { value: 3, label: "Neutral" },
  { value: 4, label: "Good" },
  { value: 5, label: "Great" }
] as const;

const emojis = ["😭", "😬", "🧨", "🤡", "🫡", "🧠", "🔥", "🥶", "❤️", "🫠"] as const;

function formatDate(isoDate: string) {
  return new Date(isoDate).toLocaleString();
}

export default function DecisionPage() {
  const params = useParams<{ slug: string }>();
  const slug = params.slug;

  const [viewerId, setViewerId] = useState<string | null>(null);
  const [data, setData] = useState<DecisionEnvelope | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [votingId, setVotingId] = useState<string | null>(null);

  const [rating, setRating] = useState<number | null>(null);
  const [emoji, setEmoji] = useState<string>("");
  const [comment, setComment] = useState("");

  useEffect(() => {
    setViewerId(getOrCreateViewerId());
  }, []);

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
    responses.sort((a, b) => {
      if (b.score !== a.score) {
        return b.score - a.score;
      }
      return new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
    });
    return responses;
  }, [data?.responses]);

  async function onSubmitResponse(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!viewerId) {
      return;
    }

    setSubmitError(null);
    if (!rating) {
      setSubmitError("Choose a rating.");
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
        rating,
        emoji,
        comment: comment.trim() || null
      });

      setComment("");
      await refreshDecision(viewerId);
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : "Failed to submit response");
    } finally {
      setIsSubmitting(false);
    }
  }

  async function onVote(responseId: string, value: 1 | -1) {
    if (!viewerId) {
      return;
    }

    setVotingId(responseId);
    try {
      await voteOnResponse(responseId, { viewer_id: viewerId, value });
      await refreshDecision(viewerId);
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : "Failed to vote");
    } finally {
      setVotingId(null);
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
            {data.decision.description ? <p className="subtitle">{data.decision.description}</p> : null}
            <p className="subtitle">
              Created: {formatDate(data.decision.created_at)}
              {data.decision.closes_at ? ` • Closes: ${formatDate(data.decision.closes_at)}` : ""}
            </p>
          </section>

          <section className="grid">
            <article className="card">
              <h2>Submit Your Response</h2>
              <form onSubmit={onSubmitResponse}>
                <div className="field">
                  <label>Rating</label>
                  <div className="rating-grid">
                    {ratingOptions.map((option) => (
                      <button
                        key={option.value}
                        className={`rating-btn ${rating === option.value ? "active" : ""}`}
                        type="button"
                        onClick={() => setRating(option.value)}
                      >
                        {option.value} · {option.label}
                      </button>
                    ))}
                  </div>
                </div>

                <div className="field">
                  <label>Emotional Damage Emoji</label>
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

                <button className="btn btn-hot" type="submit" disabled={isSubmitting}>
                  {isSubmitting ? "Submitting..." : "Submit Response"}
                </button>
              </form>
              {submitError ? <p className="error">{submitError}</p> : null}
            </article>

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
                            <div className="rating-fill" style={{ width: `${width}%` }} />
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
                  <p>Net sentiment: {data.stats.net_sentiment.toFixed(2)}</p>
                  <p>Top emoji: {data.stats.top_emoji || "—"}</p>
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

            <article className="card">
              <h2>Responses</h2>
              {sortedResponses.length === 0 ? <p className="muted">No responses yet.</p> : null}
              <div className="response-list">
                {sortedResponses.map((response) => (
                  <div key={response.id} className="response-card">
                    <div className="response-head">
                      <strong>
                        {response.emoji} {ratingOptions.find((item) => item.value === response.rating)?.label}
                      </strong>
                      <small className="muted">{formatDate(response.created_at)}</small>
                    </div>

                    {response.comment ? <p>{response.comment}</p> : <p className="muted">No comment.</p>}

                    <div className="vote-row">
                      <button
                        className={`vote-btn ${response.my_vote === 1 ? "active-up" : ""}`}
                        type="button"
                        disabled={votingId === response.id}
                        onClick={() => onVote(response.id, 1)}
                      >
                        ▲ {response.upvotes}
                      </button>
                      <button
                        className={`vote-btn ${response.my_vote === -1 ? "active-down" : ""}`}
                        type="button"
                        disabled={votingId === response.id}
                        onClick={() => onVote(response.id, -1)}
                      >
                        ▼ {response.downvotes}
                      </button>
                      <strong>Score: {response.score}</strong>
                    </div>
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
