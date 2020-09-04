// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"time"
)

const (
	maxSkippedIntervals = 32
)

type intervalMeter struct {
	running bool
	started time.Time

	halflife time.Duration

	value         float64
	nextHalvening time.Time
	lastUpdated   time.Time
}

// NewIntervalMeter returns a new Meter with the provided halflife
func NewIntervalMeter(halflife time.Duration) Meter {
	return &intervalMeter{halflife: halflife}
}

// TODO change the interface to have all of these functions
// take a current time argument (reduce calls to get the current time)
func (a *intervalMeter) Start(currentTime time.Time) {
	if a.running {
		return
	}
	a.Read(currentTime)
	a.running = true
}

func (a *intervalMeter) Stop(currentTime time.Time) {
	if !a.running {
		return
	}
	a.Read(currentTime)
	a.running = false
}

func (a *intervalMeter) Read(currentTime time.Time) float64 {
	if !currentTime.After(a.lastUpdated) {
		return a.value
	}

	// try to finish the current round
	if currentTime.After(a.nextHalvening) {
		if a.running {
			additionalRunningTime := float64(a.nextHalvening.Sub(a.lastUpdated)) / float64(a.halflife)
			a.value += additionalRunningTime / 2
		}
		a.lastUpdated = a.nextHalvening
		a.nextHalvening = a.nextHalvening.Add(a.halflife)
		a.value /= 2

		// try to skip future rounds
		if totalTime := currentTime.Sub(a.lastUpdated); totalTime > a.halflife {
			numSkippedPeriods := totalTime / a.halflife
			if numSkippedPeriods > maxSkippedIntervals {
				// If this meter hasn't been read in a long time, avoid
				// potential shifting overflow issues and just jump to a
				// reasonable value.
				if a.running {
					a.value = 1
				} else {
					a.value = 0
				}
				a.lastUpdated = currentTime
				a.nextHalvening = a.lastUpdated.Add(a.halflife)
				return a.value
			}

			invFactor := 1 << uint(numSkippedPeriods)
			factor := 1 / float64(invFactor)
			a.value *= factor
			if a.running {
				a.value += 1 - factor
			}
			a.value /= 2
			skippedDuration := a.halflife * numSkippedPeriods
			a.lastUpdated = a.lastUpdated.Add(skippedDuration)
			a.nextHalvening = a.nextHalvening.Add(skippedDuration)
		}
	}

	// increment the value for the current round
	if a.running {
		additionalRunningTime := float64(currentTime.Sub(a.lastUpdated)) / float64(a.halflife)
		a.value += additionalRunningTime / 2
	}
	a.lastUpdated = currentTime
	return a.value
}
