package uniqueid

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"math/rand"
	"time"
)

func UniqueId() string {
	b := make([]byte, 16)

	ts := time.Now().UnixMicro()
	binary.BigEndian.PutUint64(b[:8], uint64(ts))

	_, err := rand.Read(b[8:])
	if err != nil {
		panic(err)
	}

	encBuffer := make([]byte, 0, 32)
	encBufferWriter := bytes.NewBuffer(encBuffer)
	encoder := base64.NewEncoder(base64.URLEncoding, encBufferWriter)
	defer encoder.Close()

	_, err = encoder.Write(b)
	if err != nil {
		panic(err)
	}

	return encBufferWriter.String()
}
