package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

var (
	errUnsupportedJSONMediaType = errors.New("expected application/json")
	errInvalidJSONEncoding      = errors.New("JSON must be UTF-8")
)

// decodeJSONRequest ограничивает размер тела и принимает одно значение JSON.
// Неизвестные поля отклоняются, чтобы клиент не мог передать служебные данные.
func decodeJSONRequest(requestContext *gin.Context, maxBodyBytes int64, input any) error {
	mediaType, _, err := mime.ParseMediaType(requestContext.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errUnsupportedJSONMediaType
	}
	requestContext.Request.Body = http.MaxBytesReader(requestContext.Writer, requestContext.Request.Body, maxBodyBytes)
	body, err := io.ReadAll(requestContext.Request.Body)
	if err != nil {
		return err
	}
	if !utf8.Valid(body) {
		return errInvalidJSONEncoding
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		return err
	}
	var trailingValue json.RawMessage
	err = decoder.Decode(&trailingValue)
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values")
	}
	return err
}
