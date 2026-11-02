package store

import (
	"context"
	"time"
)

// Rate sources.
const (
	RateCBR    = "cbr"
	RateManual = "manual"
)

// CurrencyRate is how many units of Code a dollar was on Day (YYYY-MM-DD).
type CurrencyRate struct {
	Code      string
	Day       string
	PerUSD    float64
	Source    string
	FetchedAt time.Time
}

// RateOf is the rate of code on day, or ErrNotFound.
func (s Store) RateOf(ctx context.Context, code, day string) (CurrencyRate, error) {
	var r CurrencyRate
	var at int64
	err := s.q.QueryRow(ctx, `SELECT code, day, per_usd, source, fetched_at FROM currency_rates WHERE code = ? AND day = ?`, code, day).
		Scan(&r.Code, &r.Day, &r.PerUSD, &r.Source, &at)
	if err != nil {
		return CurrencyRate{}, err
	}
	r.FetchedAt = fromMs(at)
	return r, nil
}

// PutRate keeps a rate. A manual rate replaces anything; a fetched one never replaces a manual one.
func (s Store) PutRate(ctx context.Context, r CurrencyRate) error {
	q := `INSERT INTO currency_rates (code, day, per_usd, source, fetched_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (code, day) DO UPDATE SET per_usd = excluded.per_usd, source = excluded.source, fetched_at = excluded.fetched_at`
	if r.Source != RateManual {
		q += ` WHERE currency_rates.source <> 'manual'`
	}
	_, err := s.q.Exec(ctx, q, r.Code, r.Day, r.PerUSD, r.Source, r.FetchedAt.UnixMilli())
	return err
}
