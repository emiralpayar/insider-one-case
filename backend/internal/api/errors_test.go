package api

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/emiralpayar/campaign-composer/internal/llm"
)

func TestLLMFailureMapping(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{fmt.Errorf("llm: segment attempt 1: %w", fmt.Errorf("llm: anthropic: %w", llm.ErrTruncated)), http.StatusUnprocessableEntity, "llm_truncated"},
		{fmt.Errorf("llm: segment attempt 1: %w", llm.ErrRefusal), http.StatusUnprocessableEntity, "llm_refused"},
		{errors.New("dial tcp: timeout"), http.StatusBadGateway, "llm_error"},
	}
	for _, c := range cases {
		ae := toAPIError(llmFailure(c.err))
		if ae.Status != c.status || ae.Code != c.code {
			t.Errorf("%v: got %d %s, want %d %s", c.err, ae.Status, ae.Code, c.status, c.code)
		}
	}
}
