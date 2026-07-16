package indicatorwindow

import (
	"sort"
	"strconv"
)

func (ctx *analysisContext) setNumericValue(name string, value float64, ok bool) {
	if !ok {
		return
	}
	if ctx.numericValues != nil {
		ctx.numericValues[name] = value
	}
	if ctx.encodeValues {
		ctx.values[name] = format(value)
	}
}

func (ctx *analysisContext) setNumericInt(name string, value int) {
	if ctx.numericValues != nil {
		ctx.numericValues[name] = float64(value)
	}
	if ctx.encodeValues {
		ctx.values[name] = strconv.Itoa(value)
	}
}

func format(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func sortedKeys(seen map[string]struct{}) []string {
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
