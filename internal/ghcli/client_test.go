package ghcli

import (
	"encoding/json"
	"testing"
)

func TestSummarizeRollupStatus(t *testing.T) {
	type tc struct {
		name string
		raw  []string
		want CheckStatus
	}
	tests := []tc{
		{
			name: "none",
			raw:  nil,
			want: CheckStatusNone,
		},
		{
			name: "pass",
			raw: []string{
				`{"conclusion":"SUCCESS"}`,
			},
			want: CheckStatusPass,
		},
		{
			name: "pending",
			raw: []string{
				`{"status":"IN_PROGRESS"}`,
			},
			want: CheckStatusPending,
		},
		{
			name: "fail wins",
			raw: []string{
				`{"state":"SUCCESS"}`,
				`{"conclusion":"FAILURE"}`,
				`{"status":"PENDING"}`,
			},
			want: CheckStatusFail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := make([]json.RawMessage, 0, len(tt.raw))
			for _, r := range tt.raw {
				items = append(items, json.RawMessage(r))
			}
			if got := summarizeRollupStatus(items); got != tt.want {
				t.Fatalf("status=%s want %s", got, tt.want)
			}
		})
	}
}
