package ipc

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/egoist/mygo"
)

type callerScope struct {
	native   *mygo.Page
	browser  bool
	language atomic.Pointer[string]
}
type callerScopeKey struct{}

var nativeCallerScopes sync.Map

func scopeForPage(page *mygo.Page) *callerScope {
	if page == nil {
		return nil
	}
	value, _ := nativeCallerScopes.LoadOrStore(page, &callerScope{native: page})
	return value.(*callerScope)
}
func releasePageScope(page *mygo.Page) {
	if page != nil {
		nativeCallerScopes.Delete(page)
	}
}
func callerPage(ctx context.Context) *callerScope {
	if page := mygo.CallerPage(ctx); page != nil {
		return scopeForPage(page)
	}
	scope, _ := ctx.Value(callerScopeKey{}).(*callerScope)
	return scope
}

type eventSink[T any] interface{ Send(T) error }

func NewBrowserOwner(ctx context.Context) context.Context {
	return context.WithValue(ctx, callerScopeKey{}, &callerScope{browser: true})
}
