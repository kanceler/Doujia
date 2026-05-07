package artifact

import "context"

type Store interface {
	Read(ctx context.Context, uri string) ([]byte, error)
	Write(ctx context.Context, uri string, content []byte) error
}
