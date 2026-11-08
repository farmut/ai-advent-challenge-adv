package guard

import "strings"

// Helpers exported for the Day13 proxy output guard. They reuse the Day12
// regexes and normalisation so both guards agree on what a leak is.

// LeaksSystem reports whether answer repeats any 8-word sequence of system.
// Both arguments are expected to be lower(NFKC(...)) already. Systems
// shorter than 8 words never leak.
func LeaksSystem(answer, system string) bool { return LeaksSystemN(answer, system, leakNGram) }

// LeaksSystemN is LeaksSystem with a configurable n-gram size (n < 2 falls
// back to 8). Besides word n-grams it compares the squashed form of each
// n-gram against the squashed answer, so "с и с т е м н ы й" style
// spacing does not evade the check.
func LeaksSystemN(answer, system string, n int) bool {
	if n < 2 {
		n = leakNGram
	}
	sys := wordRe.FindAllString(system, -1)
	if len(sys) < n {
		return false
	}
	grams := make(map[string]struct{}, len(sys))
	squashed := make([]string, 0, len(sys))
	for i := 0; i+n <= len(sys); i++ {
		grams[strings.Join(sys[i:i+n], " ")] = struct{}{}
		squashed = append(squashed, strings.Join(sys[i:i+n], ""))
	}
	ans := wordRe.FindAllString(answer, -1)
	for i := 0; i+n <= len(ans); i++ {
		if _, ok := grams[strings.Join(ans[i:i+n], " ")]; ok {
			return true
		}
	}
	sqAnswer := squash(answer)
	for _, g := range squashed {
		if strings.Contains(sqAnswer, g) {
			return true
		}
	}
	return false
}

// Squash keeps only letters and digits.
func Squash(s string) string { return squash(s) }

// DangerousScheme reports a javascript:, vbscript: or data:<type>/ URL.
func DangerousScheme(s string) bool { return urlSchemeRe.MatchString(s) }
