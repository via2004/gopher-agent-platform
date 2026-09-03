package weather

import "errors"

var (
	ErrInvalidBaseURL   = errors.New("weather base URL is invalid")
	ErrInvalidCity      = errors.New("weather city is invalid")
	ErrProviderFailed   = errors.New("weather provider request failed")
	ErrResponseTooLarge = errors.New("weather provider response is too large")
	ErrResponseInvalid  = errors.New("weather provider response is invalid")
)
