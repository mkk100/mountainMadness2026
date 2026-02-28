CREATE TABLE decisions (
    id UUID PRIMARY KEY,
    slug TEXT UNIQUE NOT NULL,
    title TEXT NOT NULL,
    description TEXT NULL,
    closes_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE responses (
    id UUID PRIMARY KEY,
    decision_id UUID NOT NULL REFERENCES decisions(id) ON DELETE CASCADE,
    viewer_id UUID NOT NULL,
    rating INT NOT NULL CHECK (rating BETWEEN 1 AND 5),
    emoji TEXT NOT NULL,
    comment TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (decision_id, viewer_id)
);

CREATE INDEX idx_responses_decision_created_at ON responses (decision_id, created_at DESC);

CREATE TABLE votes (
    id UUID PRIMARY KEY,
    response_id UUID NOT NULL REFERENCES responses(id) ON DELETE CASCADE,
    voter_viewer_id UUID NOT NULL,
    value INT NOT NULL CHECK (value IN (-1, 1)),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (response_id, voter_viewer_id)
);

CREATE INDEX idx_votes_response_id ON votes (response_id);
