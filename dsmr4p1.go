package dsmr4p1

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/howeyc/crc16"
)

// Compute and return the CRC16 checksum over the byte slice in data.
// According to the DSMR 4.0.4 spec, this CRC16 uses the polynomial
// x^16 + x^15 + x^2 + 1, which is the same polynomial as in CRC16-IBM.
// However, we cannot simply use the checksum of the crc16 package as this version
// of the spec (as opposed to the 4.0 version) also states: "CRC16 uses no XOR in,
// no XOR out and is computed with least significant bit first." This code is the same
// as the "update" function in the crc16 function minus the initial and final XOR operations.
func p1crc16(data []byte) (crc uint16) {
	crc = 0
	for _, v := range data {
		crc = crc16.IBMTable[byte(crc)^v] ^ (crc >> 8)
	}
	return
}

// Constants for now as I'm assuming all dutch smartmeters will be in the
// same Dutch timezone.
const (
	summerTimezone = "CEST"
	winterTimezone = "CET"
)

// Parse the timestamp format used in the dutch smartmeters. Do note this function
// assumes the CET/CEST timezone.
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
		return time.Time{}, errors.New("Error parsing timestamp: missing DST indicator")
	}

	// To make sure parsing is always consistent and indepentent of the
	// the local timezone of the host this code is running on, let's for now
	// assume Dutch time.
	loc, err := time.LoadLocation("Europe/Amsterdam")

	timestamp = timestamp[:len(timestamp)-1] + " " + timezone
	ts, err := time.ParseInLocation("060102150405 MST", timestamp, loc)
	if err != nil {
		return ts, err
	}
	return ts, nil
}

// Starts polling and attempts to parse a telegram.
func startPolling(input io.Reader, ch chan Telegram) {
	br := bufio.NewReader(input)
	for {
		// Read until we find a '/', which should be the beginning of the telegram.
		_, err := br.ReadBytes(byte('/'))
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
		data, err := br.ReadBytes(byte('!'))
		if err != nil {
			log.Println(err)
			continue
		}
		// The four hexadecimal characters are the CRC-16 of the preceding data, delimitted by
		// a carriage return.
		crcBytes, err := br.ReadBytes(byte('\n'))
		if err != nil {
			log.Println(err)
			continue
		}

		if len(crcBytes) != 6 {
			log.Println("Unexpected number of CRC bytes.")
			continue // Maybe we can recover?
		}
		dataCRC := string(crcBytes[:4])
		computedCRC := fmt.Sprintf("%04X", p1crc16(data))

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

// Start polling the P1 port. This function will start a goroutine and received telegrams are
// put into returned channel. Only telegrams whose CRC value are correct are put into
// the channel.
func Poll(input io.Reader) chan Telegram {
	ch := make(chan Telegram)
	go startPolling(input, ch)
	return ch
}
