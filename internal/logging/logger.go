package logging

import (
	"os"
	"sync"

	"github.com/rs/zerolog"
)

// Interface describes the minimal logging interface the worker relies on.
type Interface interface {
	Infof(format string, args ...interface{})
	Errorf(format string, args ...interface{})
	Debugf(format string, args ...interface{})
	Warnf(format string, args ...interface{})
}

var (
	globalLogger Interface
	once         sync.Once
)

// Logger returns a lazily initialized zerolog-backed logger implementing Interface.
func Logger() Interface {
	once.Do(func() {
		base := zerolog.New(os.Stdout).With().Timestamp().Logger()
		globalLogger = &zerologAdapter{log: base}
	})
	return globalLogger
}

type zerologAdapter struct {
	log zerolog.Logger
}

func (l *zerologAdapter) Infof(format string, args ...interface{}) {
	l.log.Info().Msgf(format, args...)
}

func (l *zerologAdapter) Errorf(format string, args ...interface{}) {
	l.log.Error().Msgf(format, args...)
}

func (l *zerologAdapter) Debugf(format string, args ...interface{}) {
	l.log.Debug().Msgf(format, args...)
}

func (l *zerologAdapter) Warnf(format string, args ...interface{}) {
	l.log.Warn().Msgf(format, args...)
}
