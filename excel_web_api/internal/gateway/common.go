// gateway/common.go：统一管线/中间件可复用逻辑归口。
package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"excel_web_api/deal"
)

// decodeOptionalJSONBody decodes a JSON object body when present. An absent or
// empty body leaves target at its zero value without error.
func decodeOptionalJSONBody(
	w http.ResponseWriter,
	request *http.Request,
	target any,
) error {
	request.Body = http.MaxBytesReader(w, request.Body, maxRequestBytes)
	if request.Body == nil {
		return nil
	}
	peek := make([]byte, 1)
	count, readErr := request.Body.Read(peek)
	if count == 0 && (readErr == nil || errors.Is(readErr, io.EOF)) {
		return nil
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return readErr
	}
	request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(peek[:count]), request.Body))
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

// queryString returns the trimmed query value for key, or def when absent.
func queryString(request *http.Request, key string, def string) string {
	value := strings.TrimSpace(request.URL.Query().Get(key))
	if value == "" {
		return def
	}
	return value
}

// queryLimit parses a 1..max int32 limit query value, defaulting to def.
func queryLimit(request *http.Request, def int32, max int32) int32 {
	value := strings.TrimSpace(request.URL.Query().Get("limit"))
	if value == "" {
		return def
	}
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil || parsed < 1 {
		return def
	}
	if parsed > int64(max) {
		return max
	}
	return int32(parsed)
}

// responseEnvelope is the unified client-facing HTTP body（{code, message, data}）。
type responseEnvelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

func writeSuccess(w http.ResponseWriter, data any) {
	writeEnvelope(w, http.StatusOK, responseEnvelope{Code: 0, Message: "ok", Data: data})
}

func writeBusinessError(w http.ResponseWriter, err error) {
	code := grpcBusinessCode(err)
	spec, ok := deal.LookupError(code)
	if !ok {
		code = deal.CodeServiceUnavailable
		spec, _ = deal.LookupError(code)
	}
	writeEnvelope(w, spec.HTTPStatus, responseEnvelope{
		Code:    code,
		Message: spec.Message,
		Data:    nil,
	})
}

func writeCodeError(w http.ResponseWriter, code int) {
	spec, ok := deal.LookupError(code)
	if !ok {
		code = deal.CodeServiceUnavailable
		spec, _ = deal.LookupError(code)
	}
	writeEnvelope(w, spec.HTTPStatus, responseEnvelope{Code: code, Message: spec.Message, Data: nil})
}

// writeFieldError rejects one malformed field with a field-locating 1001
// message（契约 C-02：message 指明字段）。
func writeFieldError(w http.ResponseWriter, field string) {
	writeEnvelope(w, http.StatusBadRequest, responseEnvelope{
		Code:    deal.CodeInvalidArgument,
		Message: "参数缺失或非法: " + field,
		Data:    nil,
	})
}

func writeEnvelope(w http.ResponseWriter, httpStatus int, envelope responseEnvelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(envelope)
}
