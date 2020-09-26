package dsmr4p1

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
)

// Telegram holds the a P1 telegram. It is essentially a slice of bytes.
type Telegram []byte

// Identifier returns the identifier in the telegram.
func (t Telegram) Identifier() string {
	// According to the documentation, the telegram starts with:
	// "/XXXZ Ident CR LF CR LF", followed by the data.
	i := bytes.Index(t, []byte("\r\n\r\n"))
	return string(t[5:i])
}

// Parse attempts to parse the telegram. It returns a map of strings to string
// slices. The keys in the map are the ID-codes, the strings in the slice are
// are the value between brackets for that ID-code.
func (t Telegram) Parse() (map[string][]string, error) {
	// Parse the telegram in a relatively naive way. Of course this
	// is not properly langsec approved :)

	lines := strings.Split(string(t), "\r\n")

	if len(lines) < 2 {
		return nil, errors.New("parse error: unexpected number of lines in telegram")
	}

	// Some additional checks
	if lines[0][0] != '/' {
		return nil, errors.New("expected '/' missing in first line of telegram")
	}
	if len(lines[1]) != 0 {
		return nil, errors.New("missing separating new line (CR+LF) between identifier and data in telegram")
	}

	result := make(map[string][]string)
	// Iterate over the lines and try to parse the data. The first two lines can
	// be skipped because they should contain the identifier (see Identifier())
	// and a new-line.  The last line is skipped because it should only contain an
	// exclamation mark.
	for i, l := range lines[2 : len(lines)-1] {
		idCodeEnd := strings.Index(l, "(")
		if idCodeEnd == -1 {
			return nil, errors.New("Expected '(', not found on line" + strconv.Itoa(i))
		}

		idCode := l[:idCodeEnd]

		// The rest of the line is a number of values in round brackets "()".
		// Let's use a simple split on ")(" to get those.
		parts := strings.Split(l[idCodeEnd+1:len(l)-1], ")(")
		result[idCode] = parts
	}

	return result, nil
}
