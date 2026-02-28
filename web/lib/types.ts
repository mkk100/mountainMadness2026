export type CreateDecisionRequest = {
  title: string;
  description: string | null;
  closes_at: string | null;
};

export type CreateDecisionResponse = {
  id: string;
  slug: string;
  share_url: string;
};

export type DecisionEnvelope = {
  decision: {
    id: string;
    slug: string;
    title: string;
    description: string | null;
    closes_at: string | null;
    created_at: string;
  };
  stats: {
    response_count: number;
    rating_counts: number[];
    avg_rating: number;
    net_sentiment: number;
    emoji_counts: Array<{
      emoji: string;
      count: number;
    }>;
    top_emoji: string;
  };
  responses: Array<{
    id: string;
    rating: number;
    emoji: string;
    comment: string | null;
    created_at: string;
    score: number;
    upvotes: number;
    downvotes: number;
    my_vote: number;
  }>;
};

export type SubmitResponseRequest = {
  viewer_id: string;
  rating: number;
  emoji: string;
  comment: string | null;
};

export type VoteRequest = {
  viewer_id: string;
  value: 1 | -1;
};

export type VoteSummary = {
  response_id: string;
  score: number;
  upvotes: number;
  downvotes: number;
  my_vote: number;
};
