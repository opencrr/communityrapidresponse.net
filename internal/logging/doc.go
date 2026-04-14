// Package logging initializes the structured logger (slog) and provides
// request-scoped context helpers.
//
// Call [Init] at startup to configure the global slog logger with the
// desired format (text or JSON) and level. [WithRequestID] attaches a
// request ID to a context, and [RequestID] retrieves it, enabling
// correlated log output across a single HTTP request.
package logging
