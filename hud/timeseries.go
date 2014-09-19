package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

type Metric struct {
	TS    int64
	Value float64
}

type TimeSeries struct {
	values map[int64]float64
}

type TSCollection struct {
	series map[string]*TimeSeries
}

type Int64Slice []int64

func (s Int64Slice) Len() int {
	return len(s)
}

func (s Int64Slice) Less(i, j int) bool {
	return s[i] < s[j]
}

func (s Int64Slice) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func NewTimeSeries() *TimeSeries {
	return &TimeSeries{
		values: make(map[int64]float64),
	}
}

func NewTSCollection() *TSCollection {
	return &TSCollection{
		series: make(map[string]*TimeSeries),
	}
}

func (t *TSCollection) GetNames(pattern string) []string {

	names := []string{}
	for k, _ := range t.series {
		if pattern != "" {
			if strings.Contains(k, pattern) {
				names = append(names, k)
			}
		} else {
			names = append(names, k)
		}
	}
	return names
}

func (t *TSCollection) Get(name string) *TimeSeries {
	ts := t.series[name]
	if ts == nil {
		ts = NewTimeSeries()
		t.series[name] = ts
	}
	return ts
}

func (t *TSCollection) GetMulti(from, to int64, names ...string) map[int64]map[string]float64 {
	metrics := make(map[int64]map[string]float64)
	for _, name := range names {
		ts := t.Get(name)
		for _, metric := range ts.Filter(from, to) {
			value := metrics[metric.TS]
			if value == nil {
				value = make(map[string]float64)
				metrics[metric.TS] = value
			}
			value[name] = metric.Value
		}
	}
	return metrics
}

func (t *TimeSeries) Fill(from, to, step int64, value float64) {
	i := from
	for {
		if i > to {
			break
		}
		if _, ok := t.values[i]; !ok {
			t.values[i] = value
		}
		i += step
	}
}

func (t *TimeSeries) Add(ts int64, value float64) {
	t.values[ts] = value
}

func (t *TimeSeries) AddAll(ts *TimeSeries) {
	for _, metric := range ts.Metrics() {
		t.values[metric.TS] = metric.Value
	}
}

func (t *TimeSeries) Remove(ts int64) {
	delete(t.values, ts)
}

func (t *TimeSeries) metrics(from, to int64) []Metric {
	ts := Int64Slice{}
	for k, _ := range t.values {
		if k >= from && k < to {
			ts = append(ts, k)
		}
	}
	sort.Sort(ts)

	metrics := []Metric{}
	for _, ts := range ts {
		metrics = append(metrics, Metric{
			TS:    ts,
			Value: t.values[ts],
		})
	}
	return metrics
}

func (t *TimeSeries) Oldest() Metric {
	metrics := t.Metrics()
	if len(metrics) > 0 {
		return metrics[0]
	}
	return Metric{}
}

func (t *TimeSeries) Current() Metric {
	metrics := t.Metrics()
	if len(metrics) > 0 {
		return metrics[len(metrics)-1]
	}
	return Metric{}
}

func (t *TimeSeries) Metrics() []Metric {
	return t.metrics(0, math.MaxInt64)
}

func (t *TimeSeries) Filter(from, to int64) []Metric {
	return t.metrics(from, to)
}

func (t *TimeSeries) String() string {
	return fmt.Sprintf("TimeSeries(%d)", len(t.values))
}
