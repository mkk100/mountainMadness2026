ALTER TABLE responses
ADD COLUMN suggestion INT;

UPDATE responses
SET suggestion = CASE
    WHEN rating IN (1, 2) THEN 1
    WHEN rating = 3 THEN 2
    WHEN rating IN (4, 5) THEN 3
    ELSE 2
END
WHERE suggestion IS NULL;

ALTER TABLE responses
ALTER COLUMN suggestion SET NOT NULL;

ALTER TABLE responses
ADD CONSTRAINT responses_suggestion_check CHECK (suggestion BETWEEN 1 AND 3);

CREATE INDEX idx_responses_decision_suggestion
ON responses (decision_id, suggestion);
