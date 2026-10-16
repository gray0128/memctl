package git

import (
	"context"
	"os"
)

// inheritedFileKey carries a file descriptor that every Git child started with
// the context must inherit. The repository lock uses it so a killed parent
// cannot leave Git writing after the lock is released: the child holds the
// same open file description, keeping the lock alive until Git exits.
type inheritedFileKey struct{}

// WithInheritedFile returns a context whose Git children inherit file.
func WithInheritedFile(ctx context.Context, file *os.File) context.Context {
	if file == nil {
		return ctx
	}
	return context.WithValue(ctx, inheritedFileKey{}, file)
}

func inheritedFile(ctx context.Context) *os.File {
	file, _ := ctx.Value(inheritedFileKey{}).(*os.File)
	return file
}
