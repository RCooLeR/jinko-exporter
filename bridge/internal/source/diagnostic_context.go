package source

import "context"

type diagnosticFetchContextKey struct{}

// IsDiagnosticFetch reports whether ctx belongs to an evidence-only source
// read. Source clients can use this narrow marker to suppress ordinary
// operator notifications that would otherwise duplicate the correlation
// alert. It must not change authentication, transport, or parsing behavior.
func IsDiagnosticFetch(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	diagnostic, _ := ctx.Value(diagnosticFetchContextKey{}).(bool)
	return diagnostic
}

func withDiagnosticFetch(ctx context.Context) context.Context {
	return context.WithValue(ctx, diagnosticFetchContextKey{}, true)
}
