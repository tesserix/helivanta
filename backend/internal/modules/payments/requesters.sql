SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Hospital billing asks for UPI at its desk (#198, #211): a second requester.
ALTER TABLE payment_intents DROP CONSTRAINT payment_intents_requester_check;
ALTER TABLE payment_intents ADD CONSTRAINT payment_intents_requester_check CHECK (requester IN ('pharmacy','billing'));
