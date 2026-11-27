package ipc

import "context"

func testOwner(ctx context.Context) context.Context {
	return context.WithValue(ctx, callerScopeKey{}, &callerScope{})
}
