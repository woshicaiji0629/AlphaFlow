package indicatorcalc

func aiPerformanceClusters(results []aiSupertrendFactorResult) ([]aiPerformanceCluster, bool) {
	if len(results) < 3 {
		return nil, false
	}
	perfs := make([]float64, 0, len(results))
	for _, result := range results {
		perfs = append(perfs, result.perf)
	}
	centroids := []float64{
		percentileSortedCopy(perfs, 0.25),
		percentileSortedCopy(perfs, 0.50),
		percentileSortedCopy(perfs, 0.75),
	}
	assignments := make([]int, len(results))
	for iteration := 0; iteration < 100; iteration++ {
		sums := []float64{0, 0, 0}
		counts := []int{0, 0, 0}
		for index, result := range results {
			cluster := nearestCentroidIndex(result.perf, centroids)
			assignments[index] = cluster
			sums[cluster] += result.perf
			counts[cluster]++
		}
		changed := false
		for index := range centroids {
			if counts[index] == 0 {
				continue
			}
			next := sums[index] / float64(counts[index])
			if absFloat(next-centroids[index]) > 0.00000001 {
				changed = true
			}
			centroids[index] = next
		}
		if !changed {
			break
		}
	}
	clusters := []aiPerformanceCluster{
		{name: "cluster_0", centroid: centroids[0]},
		{name: "cluster_1", centroid: centroids[1]},
		{name: "cluster_2", centroid: centroids[2]},
	}
	for index, result := range results {
		cluster := assignments[index]
		clusters[cluster].factors = append(clusters[cluster].factors, result.factor)
		clusters[cluster].perfs = append(clusters[cluster].perfs, result.perf)
	}
	sortPerformanceClusters(clusters)
	clusters[0].name = "worst"
	clusters[1].name = "average"
	clusters[2].name = "best"
	return clusters, true
}

func nearestCentroidIndex(value float64, centroids []float64) int {
	index := 0
	distance := absFloat(value - centroids[0])
	for nextIndex := 1; nextIndex < len(centroids); nextIndex++ {
		nextDistance := absFloat(value - centroids[nextIndex])
		if nextDistance < distance {
			index = nextIndex
			distance = nextDistance
		}
	}
	return index
}

func percentileSortedCopy(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	for index := 1; index < len(sorted); index++ {
		value := sorted[index]
		position := index - 1
		for position >= 0 && sorted[position] > value {
			sorted[position+1] = sorted[position]
			position--
		}
		sorted[position+1] = value
	}
	if percentile <= 0 {
		return sorted[0]
	}
	if percentile >= 1 {
		return sorted[len(sorted)-1]
	}
	position := percentile * float64(len(sorted)-1)
	lower := int(position)
	upper := lower + 1
	if upper >= len(sorted) {
		return sorted[lower]
	}
	weight := position - float64(lower)
	return sorted[lower] + (sorted[upper]-sorted[lower])*weight
}

func sortPerformanceClusters(clusters []aiPerformanceCluster) {
	for index := 1; index < len(clusters); index++ {
		value := clusters[index]
		position := index - 1
		for position >= 0 && clusters[position].centroid > value.centroid {
			clusters[position+1] = clusters[position]
			position--
		}
		clusters[position+1] = value
	}
}
