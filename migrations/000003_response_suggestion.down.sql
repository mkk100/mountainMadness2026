DROP INDEX IF EXISTS idx_responses_decision_suggestion;

ALTER TABLE responses
DROP CONSTRAINT IF EXISTS responses_suggestion_check;

ALTER TABLE responses
DROP COLUMN IF EXISTS suggestion;
