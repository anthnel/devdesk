package docker

import "testing"

// parseSize sees both of Docker's unit families, and which one depends on the
// column: `docker image ls {{.Size}}`, `docker system df -v` and the
// NetIO/BlockIO cells of `docker stats` go through units.HumanSize and come out
// decimal, while MemUsage is formatted elsewhere and comes out binary.
//
// The binary half is newer than the rest of this function, and it was added for
// a reason worth keeping in view: "15.18GiB" used to fall past every decimal
// case to the bare "B", fail to parse "15.18Gi" as a number, and return 0 — a
// value that then served as the denominator of the dashboard's Docker memory
// share.
func TestParseSize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int64
	}{
		{"empty string", "", 0},
		{"bytes", "512B", 512},
		{"zero bytes", "0B", 0},
		{"kilobytes", "10.2kB", 10200},
		{"megabytes", "256MB", 256_000_000},
		{"gigabytes", "1.5GB", 1_500_000_000},
		{"fractional kilobytes", "1.2kB", 1200},
		{"surrounding whitespace is trimmed", "  64MB  ", 64_000_000},
		{"gigabytes take precedence over the bare B suffix", "2GB", 2_000_000_000},
		{"terabytes", "5TB", 5_000_000_000_000},

		// The binary family, and the order that makes it reachable: every one of
		// these ends in "B" too, so a decimal-first table matches "GB" against
		// "GiB"'s tail and never gets here.
		{"binary kilobytes", "512KiB", 524_288},
		{"binary megabytes", "5.324MiB", 5_582_618},
		{"binary gigabytes", "1.5GiB", 1_610_612_736},
		{"binary terabytes", "2TiB", 2_199_023_255_552},
		{"a binary unit is not read as its decimal neighbour", "15.18GiB", 16_299_400_888},
		{"non-numeric value yields zero", "abcMB", 0},
		{"missing unit yields zero", "1024", 0},
		{"embedded space breaks parsing", "1.5 GB", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseSize(tt.in); got != tt.want {
				t.Errorf("parseSize(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseNetIO(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		wantRx int64
		wantTx int64
	}{
		{"typical docker stats NetIO column", "1.2kB / 3.4kB", 1200, 3400},
		{"no whitespace around the separator", "500B/1kB", 500, 1000},
		{"zero traffic", "0B / 0B", 0, 0},
		{"mixed units", "1.5GB / 256MB", 1_500_000_000, 256_000_000},
		{"missing separator yields zeroes", "1.2kB", 0, 0},
		{"empty string yields zeroes", "", 0, 0},
		{"unparsable operands yield zeroes", "n/a", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rx, tx := parseNetIO(tt.in)
			if rx != tt.wantRx || tx != tt.wantTx {
				t.Errorf("parseNetIO(%q) = (%d, %d), want (%d, %d)", tt.in, rx, tx, tt.wantRx, tt.wantTx)
			}
		})
	}
}
