package match_test

import (
	"errors"
	"testing"

	"github.com/FootGrid/footgrid/internal/match"
)

func TestCreateInputValidate(t *testing.T) {
	valid := match.CreateInput{
		OrganizationID:       "11111111-1111-4111-8111-111111111111",
		VenueName:            "Turf Arena",
		FormatCode:           "6V6",
		PlayersPerSide:       6,
		PeriodCount:          2,
		TotalDurationSeconds: 2400,
		HomeDisplayName:      "Gachibowli FC",
		AwayDisplayName:      "Kondapur SC",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid input, got %v", err)
	}

	tests := []struct {
		name  string
		input match.CreateInput
	}{
		{
			name:  "non UUID organization id",
			input: func() match.CreateInput { input := valid; input.OrganizationID = "org-1"; return input }(),
		},
		{
			name:  "format and player count mismatch",
			input: func() match.CreateInput { input := valid; input.PlayersPerSide = 5; return input }(),
		},
		{
			name:  "unsupported format",
			input: func() match.CreateInput { input := valid; input.FormatCode = "6v6"; return input }(),
		},
		{
			name:  "duration remains minutes instead of seconds",
			input: func() match.CreateInput { input := valid; input.TotalDurationSeconds = 40; return input }(),
		},
		{
			name:  "blank display name",
			input: func() match.CreateInput { input := valid; input.AwayDisplayName = "  "; return input }(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.input.Validate()
			if !errors.Is(err, match.ErrInvalidMatchInput) {
				t.Fatalf("expected ErrInvalidMatchInput, got %v", err)
			}
		})
	}
}

func TestCreateInputValidateAllowsCustomFormat(t *testing.T) {
	input := match.CreateInput{
		OrganizationID:       "11111111-1111-4111-8111-111111111111",
		FormatCode:           "CUSTOM",
		PlayersPerSide:       7,
		PeriodCount:          3,
		TotalDurationSeconds: 3600,
		HomeDisplayName:      "Home",
		AwayDisplayName:      "Away",
	}
	if err := input.Validate(); err != nil {
		t.Fatalf("expected valid custom format, got %v", err)
	}
}
