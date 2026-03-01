import type {
  CreateDecisionRequest,
  CreateDecisionResponse,
  DecisionEnvelope,
  SubmitResponseRequest,
  VoteRequest,
  VoteSummary
} from "./types";

function resolveApiBaseUrl() {
  const configured = process.env.NEXT_PUBLIC_API_BASE_URL?.replace(/\/+$/, "");
  if (configured) {
    return configured;
  }

  if (typeof window !== "undefined") {
    return `${window.location.protocol}//${window.location.hostname}:8080`;
  }

  return "http://localhost:8080";
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${resolveApiBaseUrl()}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers ?? {})
    }
  });

  if (!response.ok) {
    let message = `Request failed with status ${response.status}`;
    try {
      const errBody = (await response.json()) as { error?: string };
      if (errBody.error) {
        message = errBody.error;
      }
    } catch {
      // Use default message when body is not JSON.
    }
    throw new Error(message);
  }

  return (await response.json()) as T;
}

export function createDecision(payload: CreateDecisionRequest) {
  return request<CreateDecisionResponse>("/api/decisions", {
    method: "POST",
    body: JSON.stringify(payload)
  });
}

export function getDecision(slug: string, viewerId?: string) {
  const query = viewerId ? `?viewer_id=${encodeURIComponent(viewerId)}` : "";
  return request<DecisionEnvelope>(`/api/decisions/${encodeURIComponent(slug)}${query}`, {
    cache: "no-store"
  });
}

export function submitDecisionResponse(slug: string, payload: SubmitResponseRequest) {
  return request<{ id: string }>(`/api/decisions/${encodeURIComponent(slug)}/responses`, {
    method: "POST",
    body: JSON.stringify(payload)
  });
}

export function voteOnDecision(slug: string, payload: VoteRequest) {
  return request<VoteSummary>(`/api/decisions/${encodeURIComponent(slug)}/vote`, {
    method: "POST",
    body: JSON.stringify(payload)
  });
}
