package main

import "testing"

func TestTimeSeriesAdd(t *testing.T) {
	ts := NewTimeSeries()

	ts.Add(1, 1)
	ts.Add(2, 2)

	metrics := ts.Metrics()
	if len(metrics) != 2 {
		t.Fatalf("len(metrics) = %d. Want %d", len(metrics), 2)
	}
	if metrics[0].TS != 1 && metrics[0].Value != 1 {
		t.Fatalf("metrics[0] = (%d, %f). Want (%d, %f)", metrics[0].TS, metrics[0].Value,
			1, 1)
	}
	if metrics[1].TS != 2 && metrics[1].Value != 2 {
		t.Fatalf("metrics[0] = (%d, %f). Want (%d, %f)", metrics[1].TS, metrics[1].Value,
			1, 1)
	}
}

func TestTimeSeriesAddDuplicate(t *testing.T) {
	ts := NewTimeSeries()

	ts.Add(1, 1)
	ts.Add(1, 2)

	metrics := ts.Metrics()
	if len(metrics) != 1 {
		t.Fatalf("len(metrics) = %d. Want %d", len(metrics), 2)
	}
	if metrics[0].TS != 1 && metrics[0].Value != 2 {
		t.Fatalf("metrics[0] = (%d, %f). Want (%d, %f)", metrics[0].TS, metrics[0].Value,
			1, 2)
	}
}

func TestTimeSeriesFilter(t *testing.T) {
	ts := NewTimeSeries()

	ts.Add(1, 1)
	ts.Add(2, 2)
	ts.Add(3, 3)
	ts.Add(4, 4)
	ts.Add(5, 5)

	metrics := ts.Filter(2, 4)
	if len(metrics) != 2 {
		t.Fatalf("len(metrics) = %d. Want %d", len(metrics), 2)
	}
	if metrics[0].TS != 2 && metrics[0].Value != 2 {
		t.Fatalf("metrics[0] = (%d, %f). Want (%d, %f)", metrics[0].TS, metrics[0].Value,
			2, 2)
	}
	if metrics[1].TS != 3 && metrics[0].Value != 3 {
		t.Fatalf("metrics[0] = (%d, %f). Want (%d, %f)", metrics[0].TS, metrics[0].Value,
			3, 3)
	}
}

func TestTimeSeriesRemove(t *testing.T) {
	ts := NewTimeSeries()

	ts.Add(1, 1)
	ts.Add(2, 2)
	ts.Add(3, 3)
	ts.Add(4, 4)
	ts.Add(5, 5)
	ts.Remove(3)

	metrics := ts.Filter(2, 4)
	if len(metrics) != 1 {
		t.Fatalf("len(metrics) = %d. Want %d", len(metrics), 2)
	}
	if metrics[0].TS != 2 && metrics[0].Value != 2 {
		t.Fatalf("metrics[0] = (%d, %f). Want (%d, %f)", metrics[0].TS, metrics[0].Value,
			2, 2)
	}
}
