package indicatorwindow

import (
	"sort"
	"strconv"
)

func setValue(values map[string]string, name string, value float64, ok bool) {
	if !ok {
		return
	}
	values[name] = format(value)
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
