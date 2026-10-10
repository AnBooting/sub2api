-- Provider-scoped USD token pricing overrides for OpenAI-compatible domestic
-- model groups. Built-in official prices live in the application catalog;
-- this table stores only explicit administrator overrides.
CREATE TABLE IF NOT EXISTS domestic_model_pricing_overrides (
    provider                    VARCHAR(32) NOT NULL,
    model                       VARCHAR(255) NOT NULL,
    input_price_per_token       NUMERIC(24,18) NOT NULL,
    output_price_per_token      NUMERIC(24,18) NOT NULL,
    cache_read_price_per_token  NUMERIC(24,18),
    cache_write_price_per_token NUMERIC(24,18),
    source                      TEXT NOT NULL,
    version                     BIGINT NOT NULL DEFAULT 1,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (provider, model),
    CONSTRAINT domestic_model_pricing_provider_check CHECK (
        provider IN ('deepseek', 'qwen', 'doubao', 'kimi', 'zhipu',
                     'minimax', 'baichuan', 'yi', 'stepfun', 'custom')
    ),
    CONSTRAINT domestic_model_pricing_model_check CHECK (length(btrim(model)) > 0),
    CONSTRAINT domestic_model_pricing_source_url_check CHECK (source ~ '^https?://'),
    CONSTRAINT domestic_model_pricing_input_positive_check CHECK (input_price_per_token > 0),
    CONSTRAINT domestic_model_pricing_output_positive_check CHECK (output_price_per_token > 0),
    CONSTRAINT domestic_model_pricing_cache_read_nonnegative_check CHECK (
        cache_read_price_per_token IS NULL OR cache_read_price_per_token > 0
    ),
    CONSTRAINT domestic_model_pricing_cache_write_nonnegative_check CHECK (
        cache_write_price_per_token IS NULL OR cache_write_price_per_token > 0
    ),
    CONSTRAINT domestic_model_pricing_version_positive_check CHECK (version > 0)
);

-- CREATE TABLE IF NOT EXISTS does not repair a partially-created table. Fail
-- with an actionable list instead of letting a later index/query fail opaquely
-- or, worse, running without the fail-closed price constraints.
DO $$
DECLARE
    missing_columns TEXT;
    missing_constraints TEXT;
BEGIN
    SELECT string_agg(expected.column_name, ', ' ORDER BY expected.column_name)
      INTO missing_columns
      FROM unnest(ARRAY[
          'provider', 'model', 'input_price_per_token', 'output_price_per_token',
          'cache_read_price_per_token', 'cache_write_price_per_token', 'source',
          'version', 'created_at', 'updated_at'
      ]) AS expected(column_name)
     WHERE NOT EXISTS (
         SELECT 1
           FROM information_schema.columns actual
          WHERE actual.table_schema = current_schema()
            AND actual.table_name = 'domestic_model_pricing_overrides'
            AND actual.column_name = expected.column_name
     );

    IF missing_columns IS NOT NULL THEN
        RAISE EXCEPTION
            'domestic_model_pricing_overrides is incompatible; missing columns: %',
            missing_columns;
    END IF;

    SELECT string_agg(expected.constraint_name, ', ' ORDER BY expected.constraint_name)
      INTO missing_constraints
      FROM unnest(ARRAY[
          'domestic_model_pricing_overrides_pkey',
          'domestic_model_pricing_provider_check',
          'domestic_model_pricing_model_check',
          'domestic_model_pricing_source_url_check',
          'domestic_model_pricing_input_positive_check',
          'domestic_model_pricing_output_positive_check',
          'domestic_model_pricing_cache_read_nonnegative_check',
          'domestic_model_pricing_cache_write_nonnegative_check',
          'domestic_model_pricing_version_positive_check'
      ]) AS expected(constraint_name)
     WHERE NOT EXISTS (
         SELECT 1
           FROM pg_constraint actual
          WHERE actual.conrelid = 'domestic_model_pricing_overrides'::regclass
            AND actual.conname = expected.constraint_name
     );

    IF missing_constraints IS NOT NULL THEN
        RAISE EXCEPTION
            'domestic_model_pricing_overrides is incompatible; missing constraints: %',
            missing_constraints;
    END IF;
END
$$;

CREATE INDEX IF NOT EXISTS idx_domestic_model_pricing_updated_at
    ON domestic_model_pricing_overrides (updated_at DESC);

COMMENT ON TABLE domestic_model_pricing_overrides IS
    'Administrator overrides for provider-scoped domestic model prices; all values are direct international API USD/token prices';
COMMENT ON COLUMN domestic_model_pricing_overrides.input_price_per_token IS
    'International API input price in USD per token; must be positive';
COMMENT ON COLUMN domestic_model_pricing_overrides.output_price_per_token IS
    'International API output price in USD per token; must be positive';
COMMENT ON COLUMN domestic_model_pricing_overrides.cache_read_price_per_token IS
    'International API cache-hit/read price in USD per token; NULL means provider does not publish one';
COMMENT ON COLUMN domestic_model_pricing_overrides.cache_write_price_per_token IS
    'International API cache-write price in USD per token; NULL means provider does not publish one';
