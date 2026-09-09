package template

import "github.com/alecthw/sub-server/internal/subscription"

// Codec owns format parsing and serialization. Decode reports whether the
// document supports injection; unsupported roots are returned unchanged.
type Codec[T any] interface {
	Decode([]byte) (document T, supported bool, err error)
	Encode(T) ([]byte, error)
}

type transform[T any] func(Context, T) (T, error)

// pipeline shares the decode/transform/encode lifecycle across YAML and text.
// Its document type keeps format-specific operations checked at compile time.
type pipeline[T any] struct {
	codec Codec[T]
	steps []transform[T]
}

func (p pipeline[T]) Inject(ctx Context, content []byte) ([]byte, error) {
	document, supported, err := p.codec.Decode(content)
	if err != nil {
		return nil, err
	}
	if !supported {
		return content, nil
	}
	entries := make([]subscription.Entry, 0, len(ctx.Entries))
	for _, entry := range ctx.Entries {
		if entry.Name != "" && entry.URL != "" {
			entries = append(entries, entry)
		}
	}
	ctx.Entries = entries
	for _, step := range p.steps {
		document, err = step(ctx, document)
		if err != nil {
			return nil, err
		}
	}
	return p.codec.Encode(document)
}
