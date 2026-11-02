package rates_test

import (
	"context"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/rates"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

func TestParseUSD(t *testing.T) {
	body := []byte("<ValCurs><Valute><CharCode>EUR</CharCode><Nominal>1</Nominal><Value>95,1</Value></Valute>" +
		"<Valute><CharCode>USD</CharCode><Nominal>10</Nominal><Value>815,25</Value></Valute></ValCurs>")
	if v, err := rates.ParseUSD(body); err != nil || v != 81.525 {
		t.Errorf("ParseUSD = %v, %v", v, err)
	}
	for _, bad := range []string{"", "<CharCode>USD</CharCode>", "<CharCode>USD</CharCode><Value>abc</Value>", "<CharCode>USD</CharCode><Value>0</Value>"} {
		if _, err := rates.ParseUSD([]byte(bad)); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestMoney(t *testing.T) {
	c := rates.Currency{Code: "RUB", PerUSD: 81.5}
	if m := c.Micros(1_234_567); m.USD != 1.2346 || m.Shown != 100.62 {
		t.Errorf("Micros = %+v", m)
	}
	if m := rates.None("2026-10-01").USD(3); m.Shown != 3 {
		t.Errorf("None = %+v", m)
	}
}

func TestBookKeepsManual(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, "sqlite://"+t.TempDir()+"/hub.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	st := db.S()
	if err := st.PutRate(ctx, store.CurrencyRate{Code: "RUB", Day: "2026-10-01", PerUSD: 90, Source: store.RateManual}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutRate(ctx, store.CurrencyRate{Code: "RUB", Day: "2026-10-01", PerUSD: 81, Source: store.RateCBR}); err != nil {
		t.Fatal(err)
	}
	b := &rates.Book{DB: db, URL: rates.Off}
	month := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if c := b.For(ctx, "rub", month); c.PerUSD != 90 || c.Source != "manual" {
		t.Errorf("For = %+v", c)
	}
	if c := b.For(ctx, "RUB", month.AddDate(0, 1, 0)); c.Source != "none" || c.Code != "USD" {
		t.Errorf("unknown month = %+v", c)
	}
	if c := (*rates.Book)(nil).For(ctx, "RUB", month); c.Source != "none" {
		t.Errorf("nil book = %+v", c)
	}
}
