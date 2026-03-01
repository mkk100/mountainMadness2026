CREATE TABLE decision_votes (
    id UUID PRIMARY KEY,
    decision_id UUID NOT NULL REFERENCES decisions(id) ON DELETE CASCADE,
    voter_viewer_id UUID NOT NULL,
    value INT NOT NULL CHECK (value IN (-1, 1)),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (decision_id, voter_viewer_id)
);

CREATE INDEX idx_decision_votes_decision_id ON decision_votes (decision_id);
