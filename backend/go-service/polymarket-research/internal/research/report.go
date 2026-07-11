package research

import "sort"

type Observation struct {
	Symbol, Duration, TokenID, Outcome string
	EntryPrice                         float64
	Won                                bool
	Spread                             float64
	SecondsToExpiry                    int64
}
type Bucket struct {
	Symbol, Duration, Outcome        string
	SecondsToExpiry                  int64
	Samples, Wins                    int
	AverageEntry, AverageSpread, PnL float64
}

func Summarize(values []Observation) []Bucket {
	type key struct {
		symbol, duration, outcome string
		seconds                   int64
	}
	type acc struct {
		Bucket
		entry, spread float64
	}
	items := map[key]*acc{}
	for _, v := range values {
		k := key{v.Symbol, v.Duration, v.Outcome, v.SecondsToExpiry}
		a := items[k]
		if a == nil {
			a = &acc{Bucket: Bucket{Symbol: v.Symbol, Duration: v.Duration, Outcome: v.Outcome, SecondsToExpiry: v.SecondsToExpiry}}
			items[k] = a
		}
		a.Samples++
		a.entry += v.EntryPrice
		a.spread += v.Spread
		if v.Won {
			a.Wins++
			a.PnL += 1 - v.EntryPrice
		} else {
			a.PnL -= v.EntryPrice
		}
	}
	out := make([]Bucket, 0, len(items))
	for _, a := range items {
		if a.Samples > 0 {
			a.AverageEntry = a.entry / float64(a.Samples)
			a.AverageSpread = a.spread / float64(a.Samples)
		}
		out = append(out, a.Bucket)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Symbol == out[j].Symbol {
			if out[i].Duration == out[j].Duration {
				if out[i].Outcome == out[j].Outcome {
					return out[i].SecondsToExpiry < out[j].SecondsToExpiry
				}
				return out[i].Outcome < out[j].Outcome
			}
			return out[i].Duration < out[j].Duration
		}
		return out[i].Symbol < out[j].Symbol
	})
	return out
}
