package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// DomesticPricingRepository stores administrator overrides only. The verified
// vendor catalog remains versioned in the service layer so database contents
// can never silently turn a missing official price into a zero-price entry.
type DomesticPricingRepository struct {
	db *sql.DB
}

func NewDomesticPricingRepository(db *sql.DB) service.DomesticPricingRepository {
	return &DomesticPricingRepository{db: db}
}

const domesticPricingSelectColumns = `
	provider,
	model,
	input_price_per_token,
	output_price_per_token,
	cache_read_price_per_token,
	cache_write_price_per_token,
	source,
	version,
	updated_at`

type domesticPricingScanner interface {
	Scan(dest ...any) error
}

func scanDomesticPricingEntry(scanner domesticPricingScanner) (*service.DomesticPricingEntry, error) {
	var (
		entry      service.DomesticPricingEntry
		cacheRead  sql.NullFloat64
		cacheWrite sql.NullFloat64
	)
	if err := scanner.Scan(
		&entry.Provider,
		&entry.Model,
		&entry.InputPricePerToken,
		&entry.OutputPricePerToken,
		&cacheRead,
		&cacheWrite,
		&entry.Source,
		&entry.Version,
		&entry.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if cacheRead.Valid {
		v := cacheRead.Float64
		entry.CacheReadPricePerToken = &v
	}
	if cacheWrite.Valid {
		v := cacheWrite.Float64
		entry.CacheWritePricePerToken = &v
	}
	entry.Status = service.DomesticPricingStatusActive
	entry.Override = true
	return &entry, nil
}

func (r *DomesticPricingRepository) ListOverrides(ctx context.Context) ([]service.DomesticPricingEntry, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("domestic pricing repository database is nil")
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+domesticPricingSelectColumns+`
		FROM domestic_model_pricing_overrides
		ORDER BY provider, model`)
	if err != nil {
		return nil, fmt.Errorf("list domestic pricing overrides: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make([]service.DomesticPricingEntry, 0)
	for rows.Next() {
		entry, scanErr := scanDomesticPricingEntry(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan domestic pricing override: %w", scanErr)
		}
		result = append(result, *entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate domestic pricing overrides: %w", err)
	}
	return result, nil
}

func (r *DomesticPricingRepository) GetOverride(ctx context.Context, provider, model string) (*service.DomesticPricingEntry, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("domestic pricing repository database is nil")
	}
	entry, err := scanDomesticPricingEntry(r.db.QueryRowContext(ctx, `
		SELECT `+domesticPricingSelectColumns+`
		FROM domestic_model_pricing_overrides
		WHERE provider = $1 AND model = $2`, provider, model))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get domestic pricing override: %w", err)
	}
	return entry, nil
}

func (r *DomesticPricingRepository) UpsertOverride(ctx context.Context, input service.DomesticPricingOverrideInput) (*service.DomesticPricingEntry, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("domestic pricing repository database is nil")
	}
	entry, err := scanDomesticPricingEntry(r.db.QueryRowContext(ctx, `
		INSERT INTO domestic_model_pricing_overrides (
			provider,
			model,
			input_price_per_token,
			output_price_per_token,
			cache_read_price_per_token,
			cache_write_price_per_token,
			source,
			version
		) VALUES ($1, $2, $3, $4, $5, $6, $7, 1)
		ON CONFLICT (provider, model) DO UPDATE SET
			input_price_per_token = EXCLUDED.input_price_per_token,
			output_price_per_token = EXCLUDED.output_price_per_token,
			cache_read_price_per_token = EXCLUDED.cache_read_price_per_token,
			cache_write_price_per_token = EXCLUDED.cache_write_price_per_token,
			source = EXCLUDED.source,
			version = domestic_model_pricing_overrides.version + 1,
			updated_at = NOW()
		RETURNING `+domesticPricingSelectColumns,
		input.Provider,
		input.Model,
		input.InputPricePerToken,
		input.OutputPricePerToken,
		input.CacheReadPricePerToken,
		input.CacheWritePricePerToken,
		input.Source,
	))
	if err != nil {
		return nil, fmt.Errorf("upsert domestic pricing override: %w", err)
	}
	return entry, nil
}

func (r *DomesticPricingRepository) DeleteOverride(ctx context.Context, provider, model string) (bool, error) {
	if r == nil || r.db == nil {
		return false, errors.New("domestic pricing repository database is nil")
	}
	result, err := r.db.ExecContext(ctx, `
		DELETE FROM domestic_model_pricing_overrides
		WHERE provider = $1 AND model = $2`, provider, model)
	if err != nil {
		return false, fmt.Errorf("delete domestic pricing override: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read deleted domestic pricing override count: %w", err)
	}
	return count > 0, nil
}
