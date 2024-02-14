package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
)

// Message represents an iRTSP message. iRTSP (possibly standing for Interactive RTSP?) is a
// text-based protocol, essentially similar to RTSP but with some additions for user interaction
// with the server.
//
// A sequence header is present at the start of the message below the protocol version, which works
// the same way as the "CSeq" header on the standard RTSP protocol.
//
// Message lines are split with CRLF, and header fields are values are split with an equal sign (=).
// Messages always end with "Submit + CRLF"
//
// An example message would be:
// iRTSP/1.21 + CRLF
// Seq=0 + CRLF
// SET/START + CRLF
// sc + CRLF
// t=1429051 + CRLF
// Submit + CRLF
type Message struct {
	// Version represents the iRTSP version. An example value would be "iRTSP/1.21"
	Version string

	// Sequence is the message sequence number
	Sequence int

	// Method is the message method
	Method string

	// Code is the response code, if the message is a response
	Code int

	// Headers are the message headers
	Headers map[string]string
}

// ToBytes converts the message to a byte stream
func (m *Message) ToBytes() []byte {
	builder := &strings.Builder{}

	builder.WriteString(m.Version + "\r\n")
	builder.WriteString(fmt.Sprintf("Seq=%d\r\n", m.Sequence))

	// If the code isn't zero, then the message is a response and we can use the response line
	if m.Code > 0 {
		builder.WriteString(fmt.Sprintf("RSP/%s/%d\r\n", m.Method, m.Code))
	} else {
		builder.WriteString(fmt.Sprintf("SET/%s\r\n", m.Method))
	}

	for header, value := range m.Headers {
		// If a header value is empty, we don't write the equal sign
		if value == "" {
			builder.WriteString(header + "\r\n")
		} else {
			builder.WriteString(fmt.Sprintf("%s=%s\r\n", header, value))
		}
	}
	builder.WriteString("Submit\r\n")

	return []byte(builder.String())
}

// NewMessage creates a new Message from a byte array
//
// TODO: Support multiple messages on the same stream
func NewMessage(message []byte) *Message {
	messageString := string(message)
	// Replace CRLF line endings with LF so that we can split the message lines
	messageString = strings.ReplaceAll(messageString, "\r\n", "\n")
	messageLines := strings.Split(messageString, "\n")
	if len(messageLines) <= 0 {
		return nil
	}

	// As there is a CRLF at the end, the last line will be empty
	messageLines = messageLines[:len(messageLines)-1]
	if messageLines[len(messageLines)-1] == "Submit" {
		// Remove "Submit" line
		messageLines = messageLines[:len(messageLines)-1]
	}

	msg := &Message{Headers: make(map[string]string)}
	msg.Version = messageLines[0]

	// Discard the vresion line
	messageLines = messageLines[1:]

	// Extract sequence value from message lines
	seqField, seqValue, found := strings.Cut(messageLines[0], "=")
	if found && seqField == "Seq" {
		seq, err := strconv.Atoi(seqValue)
		if err != nil {
			log.Printf("[ERROR] %v\n", err)
			return msg
		}

		msg.Sequence = seq
		messageLines = messageLines[1:]
	}

	// Extract method from message lines
	msgSource, msgMethod, found := strings.Cut(messageLines[0], "/")
	// If the message is a request, we can set the method right away
	if found && msgSource == "SET" {
		msg.Method = msgMethod
		messageLines = messageLines[1:]
	}

	// If the message is a response, we have to split the method and the response code
	if found && msgSource == "RSP" {
		method, codeString, found := strings.Cut(msgMethod, "/")
		if found {
			msg.Method = method
			code, err := strconv.Atoi(codeString)
			if err != nil {
				log.Printf("[ERROR] %v\n", err)
				return msg
			}
			msg.Code = code
		}
		messageLines = messageLines[1:]
	}

	// Extract headers from message lines
	for _, msgHeaderField := range messageLines {
		msgHeader, msgValue, _ := strings.Cut(msgHeaderField, "=")
		msg.Headers[msgHeader] = msgValue
	}

	return msg
}
