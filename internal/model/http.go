package model

import "net/http"

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type HTTPMiddleware func(HTTPDoer) HTTPDoer
