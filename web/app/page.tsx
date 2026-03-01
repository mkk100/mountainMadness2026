"use client";

import { FormEvent, useMemo, useState } from "react";
import { createDecision } from "../lib/api";

export default function CreateDecisionPage() {
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [sharePath, setSharePath] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  const shareUrl = useMemo(() => {
    if (!sharePath) {
      return "";
    }
    if (typeof window === "undefined") {
      return sharePath;
    }
    return `${window.location.origin}${sharePath}`;
  }, [sharePath]);

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setIsSubmitting(true);
    setError(null);
    setCopied(false);

    try {
      const created = await createDecision({
        title: title.trim(),
        description: description.trim() || null,
        closes_at: null
      });
      setSharePath(created.share_url);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create decision");
    } finally {
      setIsSubmitting(false);
    }
  }

  async function copyLink() {
    if (!shareUrl) {
      return;
    }

    try {
      await navigator.clipboard.writeText(shareUrl);
      setCopied(true);
    } catch {
      setError("Unable to copy automatically. Copy the link manually.");
    }
  }

  return (
    <main className="shell">
      <section className="banner">
        <h1 className="title">RateMyLifeDecision</h1>
        <p className="subtitle">
          Create one life decision, share one link, let friends rate emotional damage, comment, and vote on the post.
        </p>
      </section>

      <section className="grid">
        <article className="card">
          <h2>Create Decision Link</h2>
          <form onSubmit={onSubmit}>
            <div className="field">
              <label htmlFor="title">Title</label>
              <input
                id="title"
                className="input"
                placeholder="Should I move to Toronto?"
                minLength={4}
                maxLength={100}
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                required
              />
            </div>

            <div className="field">
              <label htmlFor="description">Description (optional)</label>
              <textarea
                id="description"
                className="textarea"
                placeholder="New job, expensive rent, zero family nearby."
                value={description}
                onChange={(e) => setDescription(e.target.value)}
              />
            </div>

            <button className="btn btn-hot" type="submit" disabled={isSubmitting}>
              {isSubmitting ? "Creating..." : "Create Decision"}
            </button>
          </form>

          {error ? <p className="error">{error}</p> : null}
        </article>

        {sharePath ? (
          <article className="card">
            <h2>Share Link</h2>
            <div className="field">
              <label htmlFor="share-url">Decision URL</label>
              <input id="share-url" className="input" value={shareUrl} readOnly />
            </div>
            <div className="row">
              <button className="btn btn-primary" type="button" onClick={copyLink}>
                Copy Link
              </button>
              <a className="btn" href={sharePath}>
                Open Decision
              </a>
            </div>
            {copied ? <p className="success">Copied.</p> : null}
          </article>
        ) : null}
      </section>
    </main>
  );
}
