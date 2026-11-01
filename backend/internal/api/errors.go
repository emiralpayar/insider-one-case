package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/emiralpayar/campaign-composer/internal/campaign"
	"github.com/emiralpayar/campaign-composer/internal/llm"
	"github.com/emiralpayar/campaign-composer/internal/verr"
)

// maxBody bounds request bodies.
const maxBody = 1 << 20

// apiError is the error envelope body: {"error":{"code","message","details"}}.
type apiError struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

func (e *apiError) Error() string { return e.Code + ": " + e.Message }

func badRequest(msg string) *apiError {
	return &apiError{Status: http.StatusBadRequest, Code: "bad_request", Message: msg}
}

var (
	errNotFound = &apiError{Status: http.StatusNotFound, Code: "not_found", Message: "Kampanya bulunamadı"}
	errNoLLM    = &apiError{Status: http.StatusServiceUnavailable, Code: "llm_not_configured", Message: "ANTHROPIC_API_KEY tanımlı değil"}
)

// toAPIError maps domain errors to HTTP errors; anything unknown is a 500.
func toAPIError(err error) *apiError {
	var (
		ae  *apiError
		vl  verr.List
		ioe *llm.InvalidOutputError
	)
	switch {
	case errors.As(err, &ae):
		return ae
	case errors.As(err, &ioe):
		return &apiError{Status: http.StatusUnprocessableEntity, Code: "llm_invalid_output",
			Message: "Model çıktısı onarım denemelerinden sonra da geçersiz", Details: ioe.Errors}
	case errors.As(err, &vl):
		return &apiError{Status: http.StatusUnprocessableEntity, Code: "validation_failed",
			Message: "Doğrulama başarısız", Details: vl}
	case errors.Is(err, llm.ErrRefusal):
		return &apiError{Status: http.StatusUnprocessableEntity, Code: "llm_refused", Message: "Model isteği reddetti"}
	case errors.Is(err, llm.ErrTruncated):
		return &apiError{Status: http.StatusUnprocessableEntity, Code: "llm_truncated",
			Message: "Model yanıtı uzunluk sınırında kesildi"}
	case errors.Is(err, campaign.ErrNotFound):
		return errNotFound
	case errors.Is(err, campaign.ErrLocked):
		return &apiError{Status: http.StatusConflict, Code: "campaign_locked", Message: "Kampanya onaylandı; artık düzenlenemez"}
	case errors.Is(err, campaign.ErrAlreadyApproved):
		return &apiError{Status: http.StatusConflict, Code: "already_approved", Message: "Kampanya zaten onaylandı"}
	case errors.Is(err, campaign.ErrNotReady):
		return &apiError{Status: http.StatusConflict, Code: "not_ready", Message: "Kampanya bu işlem için hazır değil"}
	case errors.Is(err, campaign.ErrLargeAudienceNotConfirmed):
		return &apiError{Status: http.StatusUnprocessableEntity, Code: "large_audience_not_confirmed",
			Message: "Kitle tüm kullanıcıların %50'sinden fazla; geniş kitle onayı gerekli"}
	case errors.Is(err, campaign.ErrDuplicateNotConfirmed):
		return &apiError{Status: http.StatusUnprocessableEntity, Code: "duplicate_not_confirmed",
			Message: "Bu metin son 30 günde başka bir kampanyayla aynı kişilere gönderildi; tekrar gönderim onayı gerekli"}
	case errors.Is(err, campaign.ErrEmptyAudience):
		return &apiError{Status: http.StatusUnprocessableEntity, Code: "empty_audience", Message: "Kitle boş"}
	}
	return &apiError{Status: http.StatusInternalServerError, Code: "internal", Message: "Beklenmeyen bir hata oluştu"}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	ae := toAPIError(err)
	if ae.Status >= 500 && ae.Code == "internal" {
		slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	}
	writeJSON(w, ae.Status, map[string]*apiError{"error": ae})
}

// decode reads a JSON body into v. An empty body leaves v unchanged when
// allowEmpty is set; malformed JSON is a 400.
func decode(r *http.Request, v any, allowEmpty bool) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBody))
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) && allowEmpty {
			return nil
		}
		return badRequest("Geçersiz JSON gövdesi: " + err.Error())
	}
	return nil
}
