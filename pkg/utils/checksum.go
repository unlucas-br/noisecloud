package utils

import (
	"crypto/sha256"
	"hash/crc32"
)

// SHA256 calcula o hash SHA-256 dos dados
func SHA256(data []byte) [32]byte {
	return sha256.Sum256(data)
}

// CRC32 calcula o checksum CRC32 dos dados
func CRC32(data []byte) uint32 {
	return crc32.ChecksumIEEE(data)
}

// VerifySHA256 verifica se os dados correspondem ao hash esperado
func VerifySHA256(data []byte, expected [32]byte) bool {
	actual := SHA256(data)
	return actual == expected
}

// VerifyCRC32 verifica se os dados correspondem ao checksum esperado
func VerifyCRC32(data []byte, expected uint32) bool {
	return CRC32(data) == expected
}
