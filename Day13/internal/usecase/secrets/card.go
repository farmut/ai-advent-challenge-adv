package secrets

import "strconv"

// luhn reports whether the digit string passes the Luhn checksum.
func luhn(digits string) bool {
	sum, double := 0, false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if d < 0 || d > 9 {
			return false
		}
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return len(digits) > 0 && sum%10 == 0
}

// binOK reports whether the number starts with an issuer prefix of a major
// payment system: Visa, Mastercard, MIR, Amex, UnionPay, Discover, JCB,
// Maestro.
func binOK(digits string) bool {
	p := func(n int) int {
		if len(digits) < n {
			return -1
		}
		v, err := strconv.Atoi(digits[:n])
		if err != nil {
			return -1
		}
		return v
	}
	in := func(v, lo, hi int) bool { return v >= lo && v <= hi }
	switch {
	case digits[0] == '4': // Visa
		return true
	case in(p(2), 51, 55), in(p(4), 2221, 2720): // Mastercard
		return true
	case in(p(4), 2200, 2204): // MIR
		return true
	case p(2) == 34, p(2) == 37: // Amex
		return true
	case p(2) == 62: // UnionPay
		return true
	case p(4) == 6011, in(p(3), 644, 649), p(2) == 65: // Discover
		return true
	case in(p(4), 3528, 3589): // JCB
		return true
	case p(2) == 50, in(p(2), 56, 58), p(4) == 6304, p(4) == 6759, in(p(4), 6761, 6763): // Maestro
		return true
	}
	return false
}

// validCard checks a card candidate: 13-19 digits, Luhn and a known BIN.
func validCard(s string) bool {
	digits := onlyDigits(s)
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}
	return luhn(digits) && binOK(digits)
}

func onlyDigits(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			b = append(b, s[i])
		}
	}
	return string(b)
}

// validE164 accepts 10-15 digits.
func validE164(s string) bool {
	n := len(onlyDigits(s))
	return n >= 10 && n <= 15
}
