package util

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"strconv"

	"go.uber.org/zap"
)

var (
	Logger = zap.New(nil)
)

func UintToString(i uint) string {
	return strconv.FormatUint(uint64(i), 10)
}

func StringToRand(s string) string {
	h := md5.New()
	io.WriteString(h, s)
	return hex.EncodeToString(h.Sum(nil))
}
