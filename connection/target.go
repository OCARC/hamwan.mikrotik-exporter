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
}

func createTargetConnections(targetName string) *targetConnections {
	tc := targetConnections{
		targetName:  targetName,
		connections: make(map[*Connection]struct{}),
	}
	return &tc
}

func (tc *targetConnections) get(log zerolog.Logger, target *config.Target) (*Connection, error) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	log.Trace().Msg("try to find existing connection")
	for c := range tc.connections {
		if c.Use(log) {
			return c, nil
		}
	}

	log.Info().Msg("connect to target")
	client, err := target.Dial()
	if err != nil {
		return nil, err
	}
	connection := Connection{
		Client: client,
	}
	tc.connections[&connection] = struct{}{}

	return &connection, nil
}

func (tc *targetConnections) cleanup(useTimeout time.Duration) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	log.Logger.Trace().Str("target", tc.targetName).Msg("run cleanup")

	for c := range tc.connections {
		if c.IsInUse() {
			continue
		}

		lastUse := c.GetLastUse()
		healthy := c.IsHealthy()
		expired := time.Since(lastUse) > useTimeout

		if !healthy || expired {
			log.Logger.Info().Str("target", tc.targetName).Bool("healthy", healthy).Bool("expired", expired).Time("lastUse", lastUse).Msg("close and cleanup connection")
			c.Client.Close()
			delete(tc.connections, c)
		}
	}
}
