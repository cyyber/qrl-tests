package soak

import (
	"math"
	"slices"
	"time"
)

type point struct {
	at    time.Time
	value float64
}

// slopePerHour fits value = a + b·hours by least squares and returns b. It
// needs at least two distinct timestamps; otherwise it reports ok=false.
func slopePerHour(points []point) (slope float64, ok bool) {
	if len(points) < 2 {
		return 0, false
	}
	origin := points[0].at
	var sumX, sumY, sumXY, sumXX float64
	for _, p := range points {
		x := p.at.Sub(origin).Hours()
		sumX += x
		sumY += p.value
		sumXY += x * p.value
		sumXX += x * x
	}
	n := float64(len(points))
	denominator := n*sumXX - sumX*sumX
	if denominator == 0 {
		return 0, false
	}
	return (n*sumXY - sumX*sumY) / denominator, true
}

// percentile uses nearest-rank on a sorted copy; p is in [0, 100].
func percentile(values []time.Duration, p float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	rank := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	return sorted[min(max(rank, 0), len(sorted)-1)]
}

func bytesToMB(value float64) float64 { return value / (1024 * 1024) }

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}
