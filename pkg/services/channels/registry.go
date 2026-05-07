package channels

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/liut/morign/pkg/models/channel"
)

// HTTPRouter is an optional interface for channels that support HTTP webhook callbacks.
// Implementations should register their GET (verification) and POST (message) handlers on the router.
type HTTPRouter interface {
	RegisterHTTPRoutes(r chi.Router, callbackPath string, handler channel.MessageHandler)
}

// Factory creates a channel instance from configuration options.
type Factory func(opts map[string]any) (channel.Channel, error)

// Registry manages channel adapters.
type Registry struct {
	channels map[string]Factory
	started  map[string]channel.Channel
	mu       sync.Mutex
}

var registry = &Registry{
	channels: make(map[string]Factory),
	started:  make(map[string]channel.Channel),
}

// RegisterChannel registers a channel factory under the given name.
func RegisterChannel(name string, factory Factory) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.channels[name]; exists {
		slog.Warn("channel: overwriting existing channel registration",
			"channel", name)
	}
	registry.channels[name] = factory
	slog.Info("channel: registered", "name", name)
}

// NewChannel creates a new channel instance by name with the given options.
func NewChannel(name string, opts map[string]any) (channel.Channel, error) {
	factory, exists := registry.channels[name]
	if !exists {
		return nil, fmt.Errorf("channel %q not registered, available: %v",
			name, availableChannels())
	}
	return factory(opts)
}

// TrackChannel adds a started channel to the registry with a unique key.
func TrackChannel(key string, p channel.Channel) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.started[key] = p
}

// StopAll stops all tracked channels.
func StopAll() {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	for name, p := range registry.started {
		if err := p.Stop(); err != nil {
			slog.Warn("channel: stop failed", "name", name, "error", err)
		}
	}
	registry.started = make(map[string]channel.Channel)
}

// availableChannels returns a list of registered channel names.
func availableChannels() []string {
	var names []string
	for name := range registry.channels {
		names = append(names, name)
	}
	return names
}
