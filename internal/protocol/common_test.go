package protocol

import "testing"

func TestFormatWithSeparators(t *testing.T) {
	cases := map[int64]string{
		0:       "0",
		12345:   "12,345",
		999:     "999",
		1000:    "1,000",
		-12345:  "-12,345",
		1234567: "1,234,567",
	}
	for in, want := range cases {
		if got := FormatWithSeparators(in); got != want {
			t.Errorf("FormatWithSeparators(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatSISuffix(t *testing.T) {
	cases := map[int64]string{
		0:                 "0",
		999:               "999",
		1_000:             "1.00K",
		1_200:             "1.20K",
		10_000:            "10.0K",
		100_000:           "100K",
		999_500:           "1.00M",
		1_000_000:         "1.00M",
		1_234_000:         "1.23M",
		12_345_678:        "12.3M",
		999_950_000:       "1.00G",
		1_000_000_000:     "1.00G",
		1_234_000_000:     "1.23G",
		1_234_000_000_000: "1,234G",
	}
	for in, want := range cases {
		if got := FormatSISuffix(in); got != want {
			t.Errorf("FormatSISuffix(%d) = %q, want %q", in, got, want)
		}
	}
}
