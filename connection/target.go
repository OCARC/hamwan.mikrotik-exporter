package connection

import (
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/swoga/mikrotik-exporter/config"
)

type targetConnections struct {
	targetName  string
	connections map[*Connection]struct{}
	mu          sync.Mutex
	stopCleanup chan (bool)
	nextId      int
}

func createTargetConnections(log zerolog.Logger, targetName string, cleanupInterval time.Duration, useTimeout time.Duration) *targetConnections {
	tc := targetConnections{
		targetName:  targetName,
		connections: make(map[*Connection]struct{}),
	}
	tc.StartCleanup(log, cleanupInterval, useTimeout)
	return &tc
}

// Get existing unused connection or create new connection (blocks during healthcheck or if there is an ongoing connection attempt)
func (tc *targetConnections) get(log zerolog.Logger, target *config.Target) (*Connection, error) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	log.Trace().Msg("try to find existing connection")
	for c := range tc.connections {
		if c.Use(log, target.TimeoutDuration) {
			return c, nil
		}
	}

	id := tc.nextId
	tc.nextId++
	log.Info().Msg("connect to target")
	client, err := target.Dial()
	if err != nil {
		return nil, err
	}
	errC := client.Async()
	go handleAsyncError(log, errC)

	connection := Connection{
		Client: client,
		id:     id,
	}
	tc.connections[&connection] = struct{}{}

	return &connection, nil
}

func handleAsyncError(log zerolog.Logger, errC <-chan error) {
	for err := range errC {
		log.Err(err).Msg("error during async operation")
	}
}

func (tc *targetConnections) cleanup(useTimeout time.Duration) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	cleanupLog := log.With().Str("target", tc.targetName).Logger()
	cleanupLog.Trace().Msg("run cleanup")

	for c := range tc.connections {
		if c.IsInUse() {
			continue
		}

		lastUse := c.GetLastUse()
		healthy := c.IsHealthy()
		expired := time.Since(lastUse) > useTimeout

		if !healthy || expired {
			cleanupLog.Info().Bool("healthy", healthy).Bool("expired", expired).Time("lastUse", lastUse).Msg("close and cleanup connection")
			c.Client.Close()
			delete(tc.connections, c)
		}
	}
}

func (tc *targetConnections) StartCleanup(log zerolog.Logger, cleanupInterval time.Duration, useTimeout time.Duration) {
	log.Debug().Msg("start cleanup job")
	ticker := time.NewTicker(cleanupInterval)

	go func() {
		for {
			select {
			case <-tc.stopCleanup:
				ticker.Stop()
				return
			case <-ticker.C:
				tc.cleanup(useTimeout)
				continue
			}
		}
	}()
}

func (tc *targetConnections) StopCleanup() {
	log.Logger.Debug().Msg("stop cleanup job")

	select {
	case tc.stopCleanup <- true:
		break
	default:
		break
	}
}
