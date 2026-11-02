// Package rates is the exchange rate money is shown in: the Central Bank of Russia's rate of the organization's currency
// on the 1st of the month (docs/hub/analytics.md, «Money and periods»), kept in currency_rates once fetched; an
// administrator's correction ('manual') wins over it.
package rates

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// DefaultURL is the Central Bank's daily rates (XML, windows-1251).
const DefaultURL = "https://www.cbr.ru/scripts/XML_daily.asp"

// Off as the URL switches fetching off: amounts are shown in dollars (source 'none').
const Off = "off"

// retryAfter is how long a failed fetch is not tried again.
const retryAfter = 10 * time.Minute

// Currency is the rate of an answer: Currency of web/app/hub-api/common.ts.
type Currency struct {
	Code     string  `json:"code"`
	PerUSD   float64 `json:"perUsd"`
	RateDate string  `json:"rateDate"`
	Source   string  `json:"source"`
}

// Money is an amount in USD and in the shown currency.
type Money struct {
	USD   float64 `json:"usd"`
	Shown float64 `json:"shown"`
}

func round(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(v*p) / p
}

// Micros is an amount of micro-dollars in this currency.
func (c Currency) Micros(micros int64) Money { return c.USD(float64(micros) / 1e6) }

// USD is an amount of dollars in this currency.
func (c Currency) USD(usd float64) Money {
	return Money{USD: round(usd, 4), Shown: round(usd*c.PerUSD, 2)}
}

// None is the rate while none is known: dollars.
func None(month string) Currency {
	return Currency{Code: "USD", PerUSD: 1, RateDate: month, Source: "none"}
}

// Book reads and fetches rates.
type Book struct {
	DB     *store.DB
	URL    string // DefaultURL when "", Off to never fetch
	Client *http.Client
	Log    *slog.Logger
	Now    func() time.Time

	mu     sync.Mutex
	failed map[string]time.Time // code|day → when the fetch failed
}

func (b *Book) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

// For is the rate of code for the month that starts at month (a midnight of the organization's zone): its 1st day's.
// USD, an unknown code, a month in the future and a rate that cannot be fetched are shown in dollars.
func (b *Book) For(ctx context.Context, code string, month time.Time) Currency {
	d := month.Format("2006-01-02")
	code = strings.ToUpper(code)
	if b == nil || code != "RUB" {
		return None(d)
	}
	r, err := b.DB.S().RateOf(ctx, code, d)
	if err == nil {
		return Currency{Code: code, PerUSD: r.PerUSD, RateDate: d, Source: r.Source}
	}
	if !store.IsNotFound(err) {
		b.log().Warn("rate not read", "code", code, "day", d, "error", err)
		return None(d)
	}
	if b.URL == Off || month.After(b.now()) {
		return None(d)
	}
	key := code + "|" + d
	b.mu.Lock()
	if at, ok := b.failed[key]; ok && b.now().Sub(at) < retryAfter {
		b.mu.Unlock()
		return None(d)
	}
	b.mu.Unlock()
	per, err := b.fetch(ctx, code, month)
	if err != nil {
		b.log().Warn("CBR rate not fetched", "code", code, "day", d, "error", err)
		b.mu.Lock()
		if b.failed == nil {
			b.failed = map[string]time.Time{}
		}
		b.failed[key] = b.now()
		b.mu.Unlock()
		return None(d)
	}
	if err := b.DB.S().PutRate(ctx, store.CurrencyRate{Code: code, Day: d, PerUSD: per, Source: store.RateCBR, FetchedAt: b.now()}); err != nil {
		b.log().Warn("rate not kept", "code", code, "day", d, "error", err)
	}
	if r, err := b.DB.S().RateOf(ctx, code, d); err == nil {
		return Currency{Code: code, PerUSD: r.PerUSD, RateDate: d, Source: r.Source}
	}
	return Currency{Code: code, PerUSD: per, RateDate: d, Source: store.RateCBR}
}

func (b *Book) log() *slog.Logger {
	if b.Log != nil {
		return b.Log
	}
	return slog.Default()
}

// fetch asks the Central Bank for the dollar's rate in roubles on that day.
func (b *Book) fetch(ctx context.Context, code string, day time.Time) (float64, error) {
	u := b.URL
	if u == "" {
		u = DefaultURL
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u+"?date_req="+day.Format("02/01/2006"), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "harnessly-hub")
	c := b.Client
	if c == nil {
		c = http.DefaultClient
	}
	res, err := c.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("CBR answered %s", res.Status)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return 0, err
	}
	return ParseUSD(body)
}

// ParseUSD reads the dollar's rate (roubles per dollar) from the Central Bank's XML_daily answer. The answer is in
// windows-1251; only ASCII tags and numbers are read, so it is searched as bytes.
func ParseUSD(body []byte) (float64, error) {
	i := bytes.Index(body, []byte("<CharCode>USD</CharCode>"))
	if i < 0 {
		return 0, fmt.Errorf("no USD in the CBR answer")
	}
	rest := body[i:]
	if end := bytes.Index(rest, []byte("</Valute>")); end >= 0 {
		rest = rest[:end]
	}
	tag := func(name string) (string, bool) {
		a := bytes.Index(rest, []byte("<"+name+">"))
		if a < 0 {
			return "", false
		}
		v := rest[a+len(name)+2:]
		z := bytes.Index(v, []byte("</"+name+">"))
		if z < 0 {
			return "", false
		}
		return strings.TrimSpace(strings.ReplaceAll(string(v[:z]), ",", ".")), true
	}
	val, ok := tag("Value")
	if !ok {
		return 0, fmt.Errorf("no Value of USD in the CBR answer")
	}
	v, err := strconv.ParseFloat(val, 64)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("bad USD value %q", val)
	}
	nominal := 1.0
	if n, ok := tag("Nominal"); ok {
		if f, err := strconv.ParseFloat(n, 64); err == nil && f > 0 {
			nominal = f
		}
	}
	return v / nominal, nil
}
