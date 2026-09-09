package provider

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

func decodeBase64String(value string) ([]byte, error) {
	value = strings.TrimPrefix(value, "\xef\xbb\xbf")
	value = strings.TrimPrefix(value, "\ufeff")
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.URLEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	return base64.RawURLEncoding.DecodeString(value)
}

func decodeSubscriptionBody(body []byte, decrypt *DecryptConfig) ([]byte, error) {
	if decrypt == nil {
		return body, nil
	}

	body, err := gunzipIfNeeded(body)
	if err != nil {
		return nil, err
	}

	cipherText, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(body)))
	if err != nil {
		return nil, err
	}
	if len(cipherText) == 0 || len(cipherText)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("invalid ciphertext length")
	}

	key := []byte(decrypt.Key)
	iv := []byte(decrypt.IV)
	if len(key) != aes.BlockSize || len(iv) != aes.BlockSize {
		return nil, fmt.Errorf("invalid aes key or iv length")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	plainText := make([]byte, len(cipherText))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plainText, cipherText)

	plainText, err = pkcs7Unpad(plainText)
	if err != nil {
		return nil, err
	}

	return base64.StdEncoding.DecodeString(strings.TrimSpace(string(plainText)))
}

func gunzipIfNeeded(body []byte) ([]byte, error) {
	if len(body) < 2 || body[0] != 0x1f || body[1] != 0x8b {
		return body, nil
	}

	reader, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = reader.Close()
	}()
	return io.ReadAll(reader)
}

func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("invalid pkcs7 data length")
	}

	padding := int(data[len(data)-1])
	if padding == 0 || padding > aes.BlockSize || padding > len(data) {
		return nil, fmt.Errorf("invalid pkcs7 padding")
	}
	for i := len(data) - padding; i < len(data); i++ {
		if int(data[i]) != padding {
			return nil, fmt.Errorf("invalid pkcs7 padding")
		}
	}
	return data[:len(data)-padding], nil
}
