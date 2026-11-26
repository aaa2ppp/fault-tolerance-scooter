package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/valkey-io/valkey-go"
)

const startFrom = 1

type Client[TValue Marshaller, TValuePtr UnMarshaller] struct {
	client valkey.Client
}

func NewClient[TValue Marshaller, TValuePtr UnMarshaller](client valkey.Client) *Client[TValue, TValuePtr] {
	return &Client[TValue, TValuePtr]{
		client: client,
	}
}

func (c *Client[TValue, TValuePtr]) Get(ctx context.Context, key string) (Entry[TValue, TValuePtr], error) {
	value, err := c.client.Do(ctx, c.client.B().Get().Key(key).Build()).AsBytes()
	if err != nil {
		return Entry[TValue, TValuePtr]{}, fmt.Errorf("get entry for key %s: %w", key, err)
	}
	return From[TValue, TValuePtr](key, value)
}

func (c *Client[TValue, TValuePtr]) Set(ctx context.Context, entry Entry[TValue, TValuePtr]) error {
	packedValue := versionPack(entry.Version, entry.marshall())
	return c.client.Do(ctx, setCommand(c.client.B(), entry.Key, packedValue, entry.TTL())).Error()
}

func setCommand(builder valkey.Builder, key string, value []byte, ttl time.Duration) valkey.Completed {
	command := builder.Set().Key(key).Value(string(value))
	if ttl > 0 {
		return command.PxMilliseconds(max(1, ttl.Milliseconds())).Build()
	}
	if ttl == -1 {
		return command.Keepttl().Build()
	}
	return command.Build()
}
