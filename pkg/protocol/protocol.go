package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
)

const (
	MsgTypeData      = 0x01
	MsgTypeKeepAlive = 0x02
	MsgTypeHandshake = 0x03
)

type Header struct {
	Type   uint8
	Length uint16
}

type Packet struct {
	Header  Header
	Payload []byte
}

const HeaderSize = 3 // 1 byte Type + 2 bytes Length

// Encapsulate wraps a raw payload into a Packet byte slice.
func Encapsulate(msgType uint8, payload []byte) ([]byte, error) {
	if len(payload) > 65535 {
		return nil, errors.New("payload too large")
	}

	buf := new(bytes.Buffer)

	if err := binary.Write(buf, binary.BigEndian, msgType); err != nil {
		return nil, err
	}

	if err := binary.Write(buf, binary.BigEndian, uint16(len(payload))); err != nil {
		return nil, err
	}

	if _, err := buf.Write(payload); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func ReadPacket(r io.Reader) (*Packet, error) {
	// TODO: Handle timeouts and partial reads maybe 

	headerBuf := make([]byte, HeaderSize)
	if _, err := io.ReadFull(r, headerBuf); err != nil {
		return nil, err
	}

	var header Header
	header.Type = headerBuf[0]
	header.Length = binary.BigEndian.Uint16(headerBuf[1:3])

	payload := make([]byte, header.Length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}

	return &Packet{
		Header:  header,
		Payload: payload,
	}, nil
}

