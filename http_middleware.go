package easyllm

import (
	"errors"
	"net/http"
	"time"

	"github.com/kiry163/easyllm/internal/model"
)

type HTTPDoer = model.HTTPDoer
type HTTPMiddleware = model.HTTPMiddleware

const defaultHTTPTimeout = 30 * time.Second

func newHTTPDoer(config Config) (HTTPDoer, error) {
	timeout := defaultHTTPTimeout
	if config.Timeout > 0 {
		timeout = config.Timeout
	}
	base := &http.Client{Timeout: timeout}
	if config.HTTPMiddleware == nil {
		return base, nil
	}
	doer := config.HTTPMiddleware(base)
	if doer == nil {
		return nil, errors.New("http middleware returned nil doer")
	}
	return doer, nil
}
