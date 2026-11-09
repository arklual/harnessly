package efficiency

import "math"

// Metric is one row of the screen's «where we stand» list (IEffSummaryMetric in web/app/types/efficiency.ts).
type Metric struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	What    string   `json:"what"`
	How     string   `json:"how"`
	Unit    string   `json:"unit"` // usd | count
	Better  string   `json:"better"`
	Value   *float64 `json:"value"`
	Prev    *float64 `json:"prev"`
	Target  *float64 `json:"target"`
	Trend   *float64 `json:"trendPct"`
	Verdict string   `json:"verdict"` // good | bad | flat | nodata
	Link    struct {
		Tab string `json:"tab"`
	} `json:"link"`
}

const week = 7

// metrics are the three figures of the period's last 7 days against the 7 days before: the spend, the merged requests
// and the price of one. A week with a day of unknown spend has no spend: a sum over a hole would read as a drop.
func metrics(days []Day) []Metric {
	last, prev := days, []Day(nil)
	if len(days) > week {
		last = days[len(days)-week:]
		prev = days[:len(days)-week]
		if len(prev) > week {
			prev = prev[len(prev)-week:]
		}
	}
	if len(prev) < week {
		prev = nil
	}
	cost := func(ds []Day) *float64 {
		if len(ds) == 0 {
			return nil
		}
		var sum float64
		for _, d := range ds {
			if d.CostUSD != nil {
				sum += *d.CostUSD
			}
		}
		for _, d := range ds {
			if d.CostUSD != nil {
				return &sum
			}
		}
		return nil
	}
	mrs := func(ds []Day) *float64 {
		if len(ds) == 0 {
			return nil
		}
		n := 0
		for _, d := range ds {
			n += d.MRs
		}
		v := float64(n)
		return &v
	}
	per := func(ds []Day) *float64 {
		c, m := cost(ds), mrs(ds)
		if c == nil || m == nil {
			return nil
		}
		return ratio(*c, int(*m))
	}
	mk := func(id, title, what, how, unit, better string, f func([]Day) *float64) Metric {
		m := Metric{ID: id, Title: title, What: what, How: how, Unit: unit, Better: better}
		m.Value = f(last)
		if prev != nil {
			m.Prev = f(prev)
		}
		m.Link.Tab = "changes"
		if m.Value != nil && m.Prev != nil && *m.Prev != 0 {
			t := math.Round((*m.Value-*m.Prev) / *m.Prev * 1000) / 10
			m.Trend = &t
		}
		switch {
		case m.Value == nil:
			m.Verdict = "nodata"
		case m.Trend == nil || math.Abs(*m.Trend) < 5:
			m.Verdict = "flat"
		case (*m.Trend > 0) == (better == "up"):
			m.Verdict = "good"
		default:
			m.Verdict = "bad"
		}
		return m
	}
	return []Metric{
		mk("usd-per-mr", "Цена смёрженного MR", "Сколько стоит работа агентов на один смёрженный MR за последние 7 дней.",
			"Расход по организации (цены API) за 7 дней, делённый на все MR, которые источник считает смёрженными; ревёрты не в знаменателе.",
			"usd", "down", per),
		mk("cost", "Расход агентов", "Во что обошлись вызовы моделей за последние 7 дней.",
			"Сумма почасовых расходов организации по ценам API.", "usd", "down", cost),
		mk("mrs", "Смёрженные MR", "Сколько запросов на слияние дошло до main за последние 7 дней.",
			"Все смёрженные MR источника, как бы они ни были созданы.", "count", "up", mrs),
	}
}
