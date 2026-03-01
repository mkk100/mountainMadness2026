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
  post_vote: {
    score: number;
    upvotes: number;
    downvotes: number;
    my_vote: number;
  };
  recommendation: {
    decision: "yes" | "no";
    score: number;
    suggestion_score: number;
    rating_score: number;
    comment_sentiment: number;
    post_vote_score: number;
  };
  viewer_has_responded: boolean;
  stats: {
    response_count: number;
    rating_counts: number[];
    avg_rating: number;
    net_sentiment: number;
    categories: {
      do_it: number;
      dont_do_it: number;
      mixed: number;
    };
    emoji_counts: Array<{
      emoji: string;
      count: number;
    }>;
    top_emoji: string;
  };
  responses: Array<{
    id: string;
    rating: number;
    suggestion: 1 | 2 | 3;
    emoji: string;
    comment: string | null;
    created_at: string;
  }>;
};

export type SubmitResponseRequest = {
  viewer_id: string;
  rating: number;
  suggestion: 1 | 2 | 3;
  emoji: string;
  comment: string | null;
};

export type VoteRequest = {
  viewer_id: string;
  value: 1 | -1;
};

export type VoteSummary = {
  decision_id: string;
  score: number;
  upvotes: number;
  downvotes: number;
  my_vote: number;
};
