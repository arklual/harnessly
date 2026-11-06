-- The pricer asks for calls without a price (UnpricedCalls) on every pass; without an index that read the whole of
-- usage_calls each time, though such calls are almost never there. A partial index holds only them. Additive only: the
-- previous binary does not know the index and is not slowed by it.
CREATE INDEX usage_calls_unpriced ON usage_calls (org_key, provider, msg_key) WHERE list_cost_micros IS NULL;
