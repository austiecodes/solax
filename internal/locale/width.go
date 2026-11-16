package locale

// Width is the number of terminal columns that s takes: two for a wide
// character, as Chinese ones, one for any other.
func Width(s string) int {
	n := 0
	for _, r := range s {
		n++
		if wide(r) {
			n++
		}
	}
	return n
}

// Pad fills s with spaces up to w columns.
func Pad(s string, w int) string {
	for n := Width(s); n < w; n++ {
		s += " "
	}
	return s
}

// wide tells whether r is wide or full width (East Asian Width W or F),
// for the blocks a terminal draws in two columns.
func wide(r rune) bool {
	switch {
	case r < 0x1100:
		return false
	case r <= 0x115f, // Hangul Jamo
		r >= 0x2e80 && r <= 0x303e, // CJK radicals, punctuation
		r >= 0x3041 && r <= 0x33ff, // kana, CJK symbols
		r >= 0x3400 && r <= 0x4dbf, // CJK extension A
		r >= 0x4e00 && r <= 0x9fff, // CJK unified ideographs
		r >= 0xa000 && r <= 0xa4cf, // Yi
		r >= 0xac00 && r <= 0xd7a3, // Hangul syllables
		r >= 0xf900 && r <= 0xfaff, // CJK compatibility ideographs
		r >= 0xfe30 && r <= 0xfe4f, // CJK compatibility forms
		r >= 0xff00 && r <= 0xff60, // full width forms
		r >= 0xffe0 && r <= 0xffe6,
		r >= 0x1f300 && r <= 0x1f64f, // emoji
		r >= 0x1f900 && r <= 0x1f9ff,
		r >= 0x20000 && r <= 0x3fffd: // CJK extensions B and later
		return true
	}
	return false
}
