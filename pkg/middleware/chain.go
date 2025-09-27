package middleware

import "net/http"

// Chain is a middleware chain that allows composing multiple middlewares
type Chain struct {
	middlewares []func(http.Handler) http.Handler
}

// NewChain creates a new middleware chain
func NewChain(middlewares ...func(http.Handler) http.Handler) *Chain {
	return &Chain{
		middlewares: middlewares,
	}
}

// Append adds middlewares to the chain
func (c *Chain) Append(middlewares ...func(http.Handler) http.Handler) *Chain {
	newMiddlewares := make([]func(http.Handler) http.Handler, len(c.middlewares)+len(middlewares))
	copy(newMiddlewares, c.middlewares)
	copy(newMiddlewares[len(c.middlewares):], middlewares)

	return &Chain{
		middlewares: newMiddlewares,
	}
}

// Then executes the chain with the final handler
func (c *Chain) Then(h http.Handler) http.Handler {
	for i := len(c.middlewares) - 1; i >= 0; i-- {
		h = c.middlewares[i](h)
	}
	return h
}

// ThenFunc executes the chain with the final handler function
func (c *Chain) ThenFunc(fn http.HandlerFunc) http.Handler {
	return c.Then(fn)
}
