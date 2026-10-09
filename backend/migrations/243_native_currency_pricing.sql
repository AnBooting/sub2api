ALTER TABLE channel_model_pricing ADD COLUMN IF NOT EXISTS currency varchar(3) NOT NULL DEFAULT 'USD';
ALTER TABLE channel_model_pricing ADD CONSTRAINT channel_model_pricing_currency_check CHECK (currency IN ('USD', 'CNY'));
-- Historical rows have no snapshot; they retain their original amounts and USD semantics.
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS pricing_snapshot jsonb;
