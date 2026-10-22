package session

import (
	"errors"
	"time"
)

var ErrInvalidConfiguration = errors.New("invalid session configuration")

type Config struct {
	IdleTimeout        time.Duration
	AbsoluteTimeout    time.Duration
	RotationInterval   time.Duration
	PreviousTokenGrace time.Duration
}

func (c Config) Validate() error {
	if c.IdleTimeout <= 0 || c.AbsoluteTimeout <= 0 || c.RotationInterval <= 0 || c.PreviousTokenGrace <= 0 {
		return ErrInvalidConfiguration
	}
	if c.IdleTimeout >= c.AbsoluteTimeout || c.RotationInterval >= c.AbsoluteTimeout {
		return ErrInvalidConfiguration
	}
	if c.PreviousTokenGrace >= c.RotationInterval {
		return ErrInvalidConfiguration
	}
	return nil
}
