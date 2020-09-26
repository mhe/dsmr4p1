// Package dsmr4p1 is a library for reading (and parsing) data from the P1 port of dutch smart meters.
package dsmr4p1

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/howeyc/crc16"
)

// Constants for now as I'm assuming all dutch smartmeters will be in the
// same Dutch timezone.
const (
	summerTimezone = "CEST"
	winterTimezone = "CET"
)

var (
	// ErrorParseTimestamp indicates that there was an error parsing a timestamp.
	ErrorParseTimestamp = errors.New("error parsing timestamp: missing DST indicator")
	// ErrorParseValueWithUnit indicates that there was an error parsing a value string
	// (i.e., a string containing both a value and a unit)
	ErrorParseValueWithUnit = errors.New("error parsing string that should contain both a value and a unit")

	// According to the DSMR 4.0.4 spec, the CRC16 here uses the polynomial
	// x^16 + x^15 + x^2 + 1, which is the same polynomial as in CRC16-IBM.
	// However, we cannot simply use the IBM checksum of the crc16 package as this version
	// of the spec (as opposed to the 4.0 version) also states: "CRC16 uses no XOR in,
	// no XOR out and is computed with least significant bit first."
	// Hence, we define our own table here.
	ibmTableNoXOR = crc16.MakeTableNoXOR(crc16.IBM)
)

// ParseTimestamp parses the timestamp format used in the dutch smartmeters. Do
// note this function assumes the CET/CEST timezone.
func ParseTimestamp(timestamp string) (time.Time, error) {
	// The format for the timestamp is:
	// YYMMDDhhmmssX
	// The value used for X determines whether DST is active.
	// S (summer?) means yes, W (winter?) means no.

	var timezone string
	switch timestamp[len(timestamp)-1] {
	case 'S':
		timezone = summerTimezone
	case 'W':
		timezone = winterTimezone
	default:
		return time.Time{}, ErrorParseTimestamp
	}

	// To make sure parsing is always consistent and indepentent of the the local
	// timezone of the host this code is running on, let's for now assume Dutch
	// time.
	loc, err := time.LoadLocation("Europe/Amsterdam")

	timestamp = timestamp[:len(timestamp)-1] + " " + timezone
	ts, err := time.ParseInLocation("060102150405 MST", timestamp, loc)
	if err != nil {
		return ts, err
	}
	return ts, nil
}

// ParseValueWithUnit parses the provided string into a float and a unit. If the
// unit starts with "k" the value is multiplied by 1000 and the "k" is removed
// from the unit.
func ParseValueWithUnit(input string) (value float64, unit string, err error) {
	parts := strings.Split(input, "*")
	if len(parts) != 2 {
		err = ErrorParseValueWithUnit
		return
	}
	value, err = strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return
	}
	unit = parts[1]
	if strings.HasPrefix(unit, "k") {
		value *= 1000
		unit = unit[1:]
	}
	return
}

// Starts polling and attempts to parse a telegram.
func startPolling(input io.Reader, ch chan Telegram) {
	br := bufio.NewReader(input)
	for {
		// Read until we find a '/', which should be the beginning of the telegram.
		_, err := br.ReadBytes('/')
		if err == io.EOF {
			break
		} else if err != nil {
			log.Println(err)
			continue
		}

		// Unread the byte as the '/' is also part of the CRC computation.
		err = br.UnreadByte()
		if err != nil {
			log.Println(err)
			continue
		}

		// The '!' character signals the end of the telegram.
		data, err := br.ReadBytes('!')
		if err != nil {
			log.Println(err)
			continue
		}
		// The four hexadecimal characters are the CRC-16 of the preceding data, delimitted by
		// a carriage return.
		crcBytes, err := br.ReadBytes('\n')
		if err != nil {
			log.Println(err)
			continue
		}

		if len(crcBytes) != 6 {
			log.Println("Unexpected number of CRC bytes.")
			continue // Maybe we can recover?
		}
		dataCRC := string(crcBytes[:4])
		computedCRC := fmt.Sprintf("%04X", crc16.Checksum(data, ibmTableNoXOR))

		if dataCRC == computedCRC {
			t := Telegram(data)
			ch <- t
		} else {
			log.Printf("CRC values do not match: %s vs %s\n", dataCRC, computedCRC)
		}
	}
	// Close the channel (should only happen with EOF, allows for clean exit).
	close(ch)
}

// Poll starts polling the P1 port represented by input (an io.Reader). It will
// start a goroutine and received telegrams are put into returned channel. Only
// telegrams whose CRC value are correct are put into the channel.
func Poll(input io.Reader) chan Telegram {
	ch := make(chan Telegram)
	go startPolling(input, ch)
	return ch
}

// Some code to simulate a smartmeter
type delayedReader struct {
	rd     *bufio.Reader
	delim  byte
	ticker *time.Ticker
}

func (dr *delayedReader) Read(p []byte) (n int, err error) {
	tmp, _ := dr.rd.Peek(len(p))
	i1 := bytes.IndexByte(tmp, dr.delim)
	// No start of telegram here, just continue reading
	if i1 == -1 {
		n, err = dr.rd.Read(p)
		return
	}
	// So there is a '/' coming up. If the '/' is not the first charactar, simply
	// let read until it is.
	if i1 != 0 {
		n, err = dr.rd.Read(p[:i1])
		return
	}

	// i1 == 0, so tmp[0] == '/': a new telegram is coming up. Let's wait until
	// the ticker fires.
	<-dr.ticker.C

	// Ok, but how much should we return? Is there maybe another '/'?
	i2 := bytes.IndexByte(tmp[i1+1:], dr.delim)

	// If there isn't, just read the rest.
	if i2 == -1 {
		n, err = dr.rd.Read(p)
		return
	}

	// Finally, if there is another '/' coming up, read until that character.
	n, err = dr.rd.Read(p[:i2])
	return
}

// RateLimit takes a io.Reader (typically the output of a os.Open) and delay the
// output of each Telegram (delimited by a '/') at a certain rate (delay). The
// main purpose is for testing/simulation. Simply save the output of an actual
// smartmeter to a file. Then in your test program open the file and use the
// resulting io.Reader with this function. The resulting io.Reader will mimick a
// real smart-meter that outputs a telegram every n seconds (typically 10).
func RateLimit(input io.Reader, delay time.Duration) io.Reader {
	return &delayedReader{rd: bufio.NewReader(input), delim: '/', ticker: time.NewTicker(delay)}
}
