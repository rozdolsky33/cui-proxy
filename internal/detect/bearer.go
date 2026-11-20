package detect

// maxWordLetters is the longest run of letters still read as a word. Longer
// runs are treated as opaque, so an unseparated all-letter token is masked.
const maxWordLetters = 20

// bearerRefine accepts a bearer-token match unless the text after the scheme
// word reads as ordinary words, as in "Bearer authentication is required".
// Inside an Authorization header the match is always accepted: whatever
// follows the scheme there is a credential, even one that looks like a word.
func bearerRefine(text string, s, e int) (int, int, bool) {
	if afterAuthorization(text, s) || !wordsOnly(text, s) {
		return s, e, true
	}
	return s, e, false
}

func isLetter(b byte) bool { return b|0x20 >= 'a' && b|0x20 <= 'z' }

// bearerSeparator reports whether b can join the parts of a token or of a
// written phrase: the bearer charset's punctuation and the underscore.
func bearerSeparator(b byte) bool {
	switch b {
	case '.', '_', '~', '+', '/', '=', '-':
		return true
	}
	return false
}

// wordsOnly reports whether the whole token starting at s is made of words:
// no digit, and every run of letters is at most maxWordLetters long and is
// all lower case, all upper case, or capitalized. A random token of letters
// mixes case inside a run or is longer, so it is not words.
func wordsOnly(text string, s int) bool {
	for i := s; i < len(text); {
		b := text[i]
		switch {
		case bearerSeparator(b):
			i++
		case isLetter(b):
			j, upper, lower := i, 0, 0
			for ; j < len(text) && isLetter(text[j]); j++ {
				if text[j] < 'a' {
					upper++
				} else {
					lower++
				}
			}
			n := j - i
			capitalized := upper == 1 && text[i] < 'a'
			if n > maxWordLetters || upper != 0 && lower != 0 && !capitalized {
				return false
			}
			i = j
		case isDigit(b):
			return false
		default:
			return true // end of the token
		}
	}
	return true
}

// afterAuthorization reports whether the scheme word before the token at s
// is the value of an Authorization header or field, in forms such as
// `Authorization: Bearer`, `"authorization": "Bearer` and
// `AUTHORIZATION=Bearer`.
func afterAuthorization(text string, s int) bool {
	i := s
	for i > 0 && (text[i-1] == ' ' || text[i-1] == '\t') {
		i--
	}
	if i -= len("bearer"); i < 0 || !foldEqual(text[i:i+len("bearer")], "bearer") {
		return false
	}
	skip := func(set string) {
		for i > 0 {
			ok := false
			for k := 0; k < len(set); k++ {
				ok = ok || text[i-1] == set[k]
			}
			if !ok {
				return
			}
			i--
		}
	}
	skip(" \t\"'\\")
	if i == 0 || text[i-1] != ':' && text[i-1] != '=' {
		return false
	}
	i--
	skip(" \t\"'\\")
	const name = "authorization"
	return i >= len(name) && foldEqual(text[i-len(name):i], name)
}

// foldEqual reports whether s equals the lower-case ASCII word w, ignoring
// the case of s.
func foldEqual(s, w string) bool {
	if len(s) != len(w) {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i]|0x20 != w[i] {
			return false
		}
	}
	return true
}
