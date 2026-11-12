package catalog

import (
	"slices"
	"testing"
)

func TestNormalizeTopicSelection(t *testing.T) {
	tooMany := make([]int64, maxTopicIDsInRequest+1)
	for index := range tooMany {
		tooMany[index] = int64(index + 1)
	}
	tests := []struct {
		name    string
		input   []int64
		want    []int64
		wantErr bool
	}{
		{"order preserved", []int64{3, 1, 2}, []int64{3, 1, 2}, false},
		{"duplicates dropped, first wins", []int64{1, 1, 3, 1}, []int64{1, 3}, false},
		{"single", []int64{7}, []int64{7}, false},
		{"max allowed", []int64{1, 2, 3, 4, 5}, []int64{1, 2, 3, 4, 5}, false},
		{"duplicates do not count", []int64{1, 2, 3, 4, 5, 5, 4}, []int64{1, 2, 3, 4, 5}, false},
		{"too many topics", []int64{1, 2, 3, 4, 5, 6}, nil, true},
		{"empty", []int64{}, nil, true},
		{"nil", nil, nil, true},
		{"zero id", []int64{0}, nil, true},
		{"negative id", []int64{1, -2}, nil, true},
		{"oversized request", tooMany, nil, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeTopicSelection(test.input)
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, test.wantErr)
			}
			if !test.wantErr && !slices.Equal(got, test.want) {
				t.Fatalf("got %v, want %v", got, test.want)
			}
		})
	}
}
